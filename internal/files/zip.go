package files

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
)

// AuthorizedArchive reads only attachment metadata, never message bodies. Do
// not filter out non-READY objects: an incomplete archive must fail explicitly.
func (s *Service) AuthorizedArchive(ctx context.Context, owner, message uuid.UUID) ([]Download, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE id=$1 AND owner_id=$2)`, message, owner).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrAttachmentNotFound
	}
	if s.degraded() {
		return nil, ErrStorageUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT ma.id,ma.original_filename,fo.storage_key,fo.sha256,fo.size_bytes,fo.status FROM message_attachments ma JOIN messages m ON m.id=ma.message_id JOIN file_objects fo ON fo.id=ma.file_object_id WHERE m.id=$1 AND m.owner_id=$2 ORDER BY ma.display_order,ma.id`, message, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Download
	for rows.Next() {
		var d Download
		var key, status string
		if err := rows.Scan(&d.AttachmentID, &d.Filename, &key, &d.SHA, &d.Size, &status); err != nil {
			return nil, err
		}
		d.Key = storage.Key(key)
		if status != "READY" || d.Key.Validate() != nil || !strings.HasPrefix(key, "objects/") || d.Size < 0 {
			return nil, ErrStorageIntegrity
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrAttachmentNotFound
	}
	return items, nil
}

func archiveName(name string, used map[string]bool) string {
	// Both Windows and Unix separators are untrusted, as are control characters
	// and Windows drive/stream syntax. ZIP entries always live in its root.
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	// Leave room for duplicate suffixes on common extraction filesystems.
	for len(name) > 180 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	name = strings.Trim(name, " .")
	if name == "" {
		name = "attachment"
	}
	stem, ext := strings.TrimSuffix(name, path.Ext(name)), path.Ext(name)
	upperStem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if upperStem == "CON" || upperStem == "PRN" || upperStem == "AUX" || upperStem == "NUL" ||
		(len(upperStem) == 4 && (strings.HasPrefix(upperStem, "COM") || strings.HasPrefix(upperStem, "LPT")) && upperStem[3] >= '1' && upperStem[3] <= '9') {
		name = "_" + name
		stem = "_" + stem
	}
	candidate := name
	for n := 2; used[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

// streamArchive keeps one file and a fixed copy buffer open. It never closes
// the ZIP on failure, so no central directory can validate a partial download.
func streamArchive(ctx context.Context, dst io.Writer, items []Download, open func(context.Context, Download) (storage.File, error)) error {
	zw := zip.NewWriter(dst)
	used := make(map[string]bool)
	buf := make([]byte, 128<<10)
	for _, d := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := open(ctx, d)
		if err != nil {
			return err
		}
		err = func() error {
			stop := context.AfterFunc(ctx, func() { _ = f.Close() })
			defer stop()
			defer f.Close()
			// Read before writing this entry's header, catching first-read outages.
			reader := io.LimitReader(f, d.Size)
			n, firstErr := reader.Read(buf)
			if firstErr != nil && !errors.Is(firstErr, io.EOF) {
				return firstErr
			}
			header := &zip.FileHeader{Name: archiveName(d.Filename, used), Method: zip.Store}
			header.SetMode(0600)
			entry, err := zw.CreateHeader(header)
			if err != nil {
				return err
			}
			hash := sha256.New()
			writer := io.MultiWriter(entry, hash)
			if _, err = writer.Write(buf[:n]); err != nil {
				return err
			}
			copied, err := copyDownload(ctx, writer, reader, buf)
			if err != nil {
				return err
			}
			if int64(n)+copied != d.Size || !bytes.Equal(hash.Sum(nil), d.SHA) {
				return ErrStorageIntegrity
			}
			return ctx.Err()
		}()
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return zw.Close()
}

// archiveResponse delays HTTP headers until ZIP bytes are actually emitted.
// A failure before that point can still return the standard JSON error.
type archiveResponse struct {
	http.ResponseWriter
	started  bool
	filename string
}

func (w *archiveResponse) Write(p []byte) (int, error) {
	if !w.started {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", contentDisposition("attachment", w.filename))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Accept-Ranges", "none")
		w.started = true
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (h *Handler) DownloadMessageAttachments(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	a, _ := auth.FromContext(r.Context())
	items, err := h.service.AuthorizedArchive(r.Context(), a.User.ID, uuid.UUID(messageID))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// Existing limits protect upload writes/finalization, not downloads. ZIP
	// packing gets a separate, process-wide service gate; overload never queues.
	select {
	case h.service.archiveSlots <- struct{}{}:
		defer func() { <-h.service.archiveSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		auth.WriteError(w, r, http.StatusServiceUnavailable, "DOWNLOAD_BUSY", "another archive download is active")
		return
	}
	// Preflight every object without retaining file handles or file bytes.
	for _, d := range items {
		f, err := h.service.Open(r.Context(), d)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := f.Close(); err != nil {
			h.writeError(w, r, ErrStorageUnavailable)
			return
		}
	}
	response := &archiveResponse{ResponseWriter: w, filename: "attachments-" + uuid.UUID(messageID).String() + ".zip"}
	err = streamArchive(r.Context(), response, items, func(ctx context.Context, d Download) (storage.File, error) {
		if h.service.degraded() {
			return nil, ErrStorageUnavailable
		}
		return h.service.Open(ctx, d)
	})
	if err == nil {
		return
	}
	if !response.started {
		h.writeError(w, r, err)
		return
	}
	// Do not log filenames, storage keys or sensitive content. ErrAbortHandler
	// makes net/http reset HTTP/2 or close HTTP/1, never ending a broken ZIP as OK.
	reason := "STREAM_IO_ERROR"
	if r.Context().Err() != nil {
		reason = "CLIENT_CANCELLED"
	} else if errors.Is(err, ErrStorageIntegrity) {
		reason = "STORAGE_INTEGRITY_ERROR"
	} else if errors.Is(err, ErrStorageUnavailable) {
		reason = "STORAGE_UNAVAILABLE"
	}
	slog.Warn("attachment archive interrupted", "message_id", uuid.UUID(messageID).String(), "reason", reason)
	panic(http.ErrAbortHandler)
}
