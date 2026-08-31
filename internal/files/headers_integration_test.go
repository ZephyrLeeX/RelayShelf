//go:build integration

package files_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
)

// TestAttachmentResponseSecurityHeaders pins the Phase 11 baseline for every
// attachment byte response: downloads, inline previews, and thumbnails must
// always send nosniff and private, no-store, and must never advertise a
// document Content-Type that the browser could treat as active content.
func TestAttachmentResponseSecurityHeaders(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	adapter, err := storage.NewFilesystemStorageAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	owner := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,'alice','alice','x','ACTIVE')`, owner); err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.4 minimal")
	fileID := writeObject(t, ctx, adapter, pdf)
	hash := sha256.Sum256(pdf)
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,$3,'application/pdf','filesystem',$4,'READY',$5,$5,$5)`, fileID, hash[:], len(pdf), storage.ObjectKey(fileID).String(), now); err != nil {
		t.Fatal(err)
	}
	messageID := uuid.Must(uuid.NewV7())
	attachment := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageID, owner, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'report.pdf',0)`, attachment, messageID, fileID); err != nil {
		t.Fatal(err)
	}
	derivative := uuid.Must(uuid.NewV7())
	thumb := []byte("stub-jpeg")
	if err = adapterWrite(t, ctx, adapter, storage.DerivativeKey(derivative), thumb); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status,created_at,updated_at) VALUES($1,$2,'THUMBNAIL_SMALL',$3,'image/jpeg',$4,'READY',$5,$5)`, derivative, fileID, storage.DerivativeKey(derivative).String(), len(thumb), now); err != nil {
		t.Fatal(err)
	}

	handler := files.NewHandler(files.NewService(db, adapter))
	call := func(name string, fn func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+attachment.String()+name, nil)
		r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: owner}}))
		w := httptest.NewRecorder()
		fn(w, r)
		return w
	}

	download := call("/download", func(w http.ResponseWriter, r *http.Request) {
		handler.DownloadAttachment(w, r, httpapi.AttachmentId(attachment))
	})
	preview := call("/preview", func(w http.ResponseWriter, r *http.Request) {
		handler.PreviewAttachment(w, r, httpapi.AttachmentId(attachment))
	})
	thumbnail := call("/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		handler.GetAttachmentThumbnail(w, r, httpapi.AttachmentId(attachment))
	})

	for name, response := range map[string]*httptest.ResponseRecorder{"download": download, "preview": preview, "thumbnail": thumbnail} {
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%q", name, response.Code, response.Body.String())
		}
		if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s nosniff=%q", name, got)
		}
		if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s cache=%q", name, got)
		}
	}
	if got := download.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("download type=%q", got)
	}
	if got := preview.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("preview type=%q", got)
	}
	if disposition := download.Header().Get("Content-Disposition"); disposition == "" || !containsAttachment(disposition) {
		t.Fatalf("download disposition=%q", disposition)
	}
	if disposition := preview.Header().Get("Content-Disposition"); disposition == "" || containsAttachment(disposition) {
		t.Fatalf("preview disposition=%q", disposition)
	}
}

func containsAttachment(disposition string) bool {
	return len(disposition) >= 10 && disposition[:10] == "attachment"
}

func adapterWrite(t *testing.T, ctx context.Context, adapter *storage.FilesystemStorageAdapter, key storage.Key, data []byte) error {
	t.Helper()
	tempKey := storage.CommitTempKey(uuid.Must(uuid.NewV7()))
	f, err := adapter.CreateCommitTemp(ctx, tempKey)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return adapter.Commit(ctx, tempKey, key)
}
