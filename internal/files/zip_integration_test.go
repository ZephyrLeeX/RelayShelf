//go:build integration

package files_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
)

type archiveFaultAdapter struct {
	storage.Adapter
	failKey storage.Key
	mode    string
}

func (a *archiveFaultAdapter) Open(ctx context.Context, key storage.Key) (storage.File, error) {
	if key == a.failKey && a.mode == "open" {
		return nil, storage.ErrUnavailable
	}
	f, e := a.Adapter.Open(ctx, key)
	if e == nil && key == a.failKey && a.mode == "read" {
		return &downloadFailureFile{File: f}, nil
	}
	return f, e
}

func TestMessageArchiveAuthorizationStorageAndHTTP(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	adapter, e := storage.NewFilesystemStorageAdapter(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = adapter.EnsureLayout(ctx); e != nil {
		t.Fatal(e)
	}
	owner, other, message := uuid.New(), uuid.New(), uuid.New()
	for i, u := range []uuid.UUID{owner, other} {
		if _, e = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status,is_admin) VALUES($1,$2,'test','x','ACTIVE',true)`, u, []string{"owner", "other"}[i]); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_ciphertext,body_nonce,body_encryption_version,body_format,sensitive,lifecycle) VALUES($1,$2,NULL,'\x01','\x000000000000000000000000',1,'TEXT',true,'PERMANENT')`, message, owner); e != nil {
		t.Fatal(e)
	}
	data := []byte(strings.Repeat("archive", 20000))
	sum := sha256.Sum256(data)
	object := writeObject(t, ctx, adapter, data)
	if _, e = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,ready_at) VALUES($1,$2,$3,'application/octet-stream','filesystem',$4,'READY',now())`, object, sum[:], len(data), storage.ObjectKey(object).String()); e != nil {
		t.Fatal(e)
	}
	for i, name := range []string{"../../报告.txt", `C:\secret\报告.txt`, "报告 (2).txt"} {
		if _, e = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,$4,$5)`, uuid.New(), message, object, name, i); e != nil {
			t.Fatal(e)
		}
	}
	service := files.NewService(db, adapter)
	call := func(h *files.Handler, user, id uuid.UUID) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/", nil)
		r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: user, IsAdmin: true}}))
		w := httptest.NewRecorder()
		h.DownloadMessageAttachments(w, r, httpapi.MessageId(id))
		return w
	}
	handler := files.NewHandler(service)
	for _, id := range []uuid.UUID{message, uuid.New()} {
		response := call(handler, other, id)
		if response.Code != 404 || strings.Contains(response.Body.String(), "objects/") {
			t.Fatalf("cross owner: %d %s", response.Code, response.Body)
		}
	}
	response := call(handler, owner, message)
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/zip" || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("Accept-Ranges") != "none" {
		t.Fatalf("headers=%v status=%d", response.Header(), response.Code)
	}
	archive, e := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	if len(archive.File) != 3 {
		t.Fatal("missing attachments")
	}
	for i, f := range archive.File {
		if f.Name != []string{"报告.txt", "报告 (2).txt", "报告 (2) (2).txt"}[i] {
			t.Fatal(f.Name)
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		got, e := io.ReadAll(r)
		r.Close()
		if e != nil || !bytes.Equal(got, data) {
			t.Fatal("content mismatch", e)
		}
	}
	// A slow stream holds the ZIP slot; cancellation releases it so a later
	// request can succeed, without queuing more storage-heavy requests.
	cancelCtx, cancel := context.WithCancel(ctx)
	started := make(chan struct{})
	finished := make(chan any, 1)
	slow := &archiveCancelWriter{ResponseRecorder: httptest.NewRecorder(), ctx: cancelCtx, started: started}
	request := httptest.NewRequest("GET", "/", nil).WithContext(auth.ContextWithAuthentication(cancelCtx, auth.Authentication{User: auth.User{ID: owner}}))
	go func() {
		defer func() { finished <- recover() }()
		handler.DownloadMessageAttachments(slow, request, httpapi.MessageId(message))
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("stream did not start")
	}
	busy := call(handler, owner, message)
	if busy.Code != 503 || !strings.Contains(busy.Body.String(), "DOWNLOAD_BUSY") {
		cancel()
		t.Fatalf("busy=%d", busy.Code)
	}
	cancel()
	select {
	case aborted := <-finished:
		if aborted != http.ErrAbortHandler {
			t.Fatalf("cancel abort=%v", aborted)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation retained ZIP slot")
	}
	if response = call(handler, owner, message); response.Code != 200 {
		t.Fatal("ZIP slot not released")
	}
	// Trash keeps references and remains downloadable, matching single files.
	if _, e = db.Exec(ctx, `UPDATE messages SET trashed_at=now(),purge_at=now()+interval '7 days' WHERE id=$1`, message); e != nil {
		t.Fatal(e)
	}
	if response = call(handler, owner, message); response.Code != 200 {
		t.Fatal("trash download denied")
	}
	monitor := storage.NewMonitorWithProbe(func(context.Context) error { return storage.ErrUnavailable }, time.Millisecond, 1)
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	defer stopMonitor()
	go monitor.Run(monitorCtx)
	deadline := time.Now().Add(time.Second)
	for monitor.Healthy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	service.SetMonitor(monitor)
	if response = call(handler, owner, message); response.Code != 503 {
		t.Fatal("health ignored")
	}
	service.SetMonitor(nil)
	for _, mode := range []string{"open", "read"} {
		h := files.NewHandler(files.NewService(db, &archiveFaultAdapter{Adapter: adapter, failKey: storage.ObjectKey(object), mode: mode}))
		response = call(h, owner, message)
		if response.Code != 503 || !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("prestream %s: %d", mode, response.Code)
		}
	}
	// A later file failure after actual HTTP bytes must produce a broken response,
	// never an HTTP success containing an archive silently missing entries.
	secondData := []byte("second")
	secondHash := sha256.Sum256(secondData)
	second := writeObject(t, ctx, adapter, secondData)
	if _, e = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,ready_at) VALUES($1,$2,$3,'application/octet-stream','filesystem',$4,'READY',now())`, second, secondHash[:], len(secondData), storage.ObjectKey(second).String()); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE message_attachments SET file_object_id=$2 WHERE message_id=$1 AND display_order=2`, message, second); e != nil {
		t.Fatal(e)
	}
	h := files.NewHandler(files.NewService(db, &archiveFaultAdapter{Adapter: adapter, failKey: storage.ObjectKey(second), mode: "read"}))
	server := httptest.NewServer(httpx.Recovery(log.New(io.Discard, "", 0))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: owner}}))
		h.DownloadMessageAttachments(w, r, httpapi.MessageId(message))
	})))
	defer server.Close()
	res, e := server.Client().Get(server.URL)
	if e != nil {
		t.Fatalf("stream should have started before failure: %v", e)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/zip" {
		t.Fatal("expected ZIP response before interruption")
	}
	partial, readErr := io.ReadAll(res.Body)
	if readErr == nil {
		t.Fatal("interrupted response ended successfully")
	}
	if bytes.Contains(partial, []byte("INTERNAL_ERROR")) {
		t.Fatal("Recovery appended JSON")
	}
	if _, e = zip.NewReader(bytes.NewReader(partial), int64(len(partial))); e == nil {
		t.Fatal("partial ZIP completed")
	}

	// Missing or non-READY object cannot be silently excluded.
	if _, e = db.Exec(ctx, `UPDATE file_objects SET status='DELETING' WHERE id=$1`, second); e != nil {
		t.Fatal(e)
	}
	if _, e = service.AuthorizedArchive(ctx, owner, message); !errors.Is(e, files.ErrStorageIntegrity) {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE file_objects SET status='READY' WHERE id=$1`, second); e != nil {
		t.Fatal(e)
	}
	if e = adapter.Delete(ctx, storage.ObjectKey(second)); e != nil {
		t.Fatal(e)
	}
	if response = call(handler, owner, message); response.Code != 503 {
		t.Fatal("missing file succeeded")
	}
}

// Simulates a browser cancelling while an HTTP write is blocked.
type archiveCancelWriter struct {
	*httptest.ResponseRecorder
	ctx     context.Context
	started chan struct{}
}

func (w *archiveCancelWriter) Write([]byte) (int, error) {
	close(w.started)
	<-w.ctx.Done()
	return 0, w.ctx.Err()
}
