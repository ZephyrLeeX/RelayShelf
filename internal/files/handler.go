package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request, attachmentID httpapi.AttachmentId) {
	a, _ := auth.FromContext(r.Context())
	d, err := h.service.AuthorizedDownload(r.Context(), a.User.ID, uuid.UUID(attachmentID))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	etag := ETag(d)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", d.MIME)
	w.Header().Set("Content-Disposition", contentDisposition(d.Filename))
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", d.Modified.UTC().Format(http.TimeFormat))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	start, length, status, rangeErr := parseRange(r.Header.Get("Range"), d.Size)
	if rangeErr != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", d.Size))
		auth.WriteError(w, r, http.StatusRequestedRangeNotSatisfiable, "RANGE_NOT_SATISFIABLE", "requested range is not satisfiable")
		return
	}
	if status == http.StatusPartialContent && !ifRangeMatches(r.Header.Get("If-Range"), etag, d.Modified) {
		start, length, status = 0, d.Size, http.StatusOK
	}
	f, err := h.service.Open(r.Context(), d)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		h.writeError(w, r, ErrStorageUnavailable)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if status == http.StatusPartialContent {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, d.Size))
	}
	w.WriteHeader(status)
	_, _ = copyDownload(r.Context(), w, io.LimitReader(f, length), make([]byte, 128<<10))
}
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrAttachmentNotFound) {
		auth.WriteError(w, r, http.StatusNotFound, "ATTACHMENT_NOT_FOUND", "attachment not found")
		return
	}
	auth.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "storage is unavailable")
}
func matchesETag(value, etag string) bool {
	for _, v := range strings.Split(value, ",") {
		if strings.TrimSpace(v) == etag || strings.TrimSpace(v) == "*" {
			return true
		}
	}
	return false
}
func ifRangeMatches(value, etag string, modified time.Time) bool {
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "\"") {
		return value == etag
	}
	t, err := http.ParseTime(value)
	return err == nil && !modified.After(t.Add(time.Second))
}
func parseRange(value string, size int64) (int64, int64, int, error) {
	if value == "" {
		return 0, size, http.StatusOK, nil
	}
	if size == 0 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, 0, ErrRange
	}
	spec := strings.TrimPrefix(value, "bytes=")
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, 0, ErrRange
	}
	if left == "" {
		suffix, err := strconv.ParseInt(right, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, 0, ErrRange
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, suffix, http.StatusPartialContent, nil
	}
	start, err := strconv.ParseInt(left, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, 0, ErrRange
	}
	end := size - 1
	if right != "" {
		end, err = strconv.ParseInt(right, 10, 64)
		if err != nil || end < start {
			return 0, 0, 0, ErrRange
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, http.StatusPartialContent, nil
}
func contentDisposition(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 0x20 && r <= 0x7e && r != '"' && r != '\\' && r != ';' {
			b.WriteRune(r)
		} else if r > 127 {
			b.WriteByte('_')
		}
	}
	fallback := strings.TrimSpace(b.String())
	if fallback == "" {
		fallback = "download"
	}
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + strings.ReplaceAll(url.PathEscape(name), "+", "%20")
}
func copyDownload(ctx context.Context, dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, er := src.Read(buf)
		if n > 0 {
			wn, ew := dst.Write(buf[:n])
			total += int64(wn)
			if ew != nil {
				return total, ew
			}
			if wn != n {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(er, io.EOF) {
			return total, nil
		}
		if er != nil {
			return total, er
		}
	}
}
