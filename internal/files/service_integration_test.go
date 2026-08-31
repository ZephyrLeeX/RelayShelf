//go:build integration

package files_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
)

type selectiveDeleteFailureAdapter struct {
	storage.Adapter
	mu   sync.Mutex
	fail map[storage.Key]bool
}

type downloadFailureAdapter struct {
	storage.Adapter
	openFailure bool
	readFailure bool
}

func (a *downloadFailureAdapter) Open(ctx context.Context, key storage.Key) (storage.File, error) {
	if a.openFailure {
		return nil, errors.New("injected storage open failure")
	}
	f, err := a.Adapter.Open(ctx, key)
	if err != nil || !a.readFailure {
		return f, err
	}
	return &downloadFailureFile{File: f}, nil
}

type downloadFailureFile struct{ storage.File }

func (f *downloadFailureFile) Read([]byte) (int, error) {
	return 0, errors.New("injected storage read failure")
}

func (a *selectiveDeleteFailureAdapter) Delete(ctx context.Context, key storage.Key) error {
	a.mu.Lock()
	fail := a.fail[key]
	a.mu.Unlock()
	if fail {
		return errors.New("injected physical delete failure")
	}
	return a.Adapter.Delete(ctx, key)
}

func TestDownloadOwnershipSharedGCAndReconciliation(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	adapter, err := storage.NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	service := files.NewService(db, adapter)
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	alice, bob := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, u := range []struct {
		id   uuid.UUID
		name string
	}{{alice, "alice"}, {bob, "bob"}} {
		if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status)VALUES($1,$2,$2,'x','ACTIVE')`, u.id, u.name); err != nil {
			t.Fatal(err)
		}
	}
	fileID := writeObject(t, ctx, adapter, []byte("shared"))
	sum := sha256.Sum256([]byte("shared"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at)VALUES($1,$2,6,'application/octet-stream','filesystem',$3,'READY',$4,$4,$4)`, fileID, sum[:], storage.ObjectKey(fileID).String(), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	messageIDs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	attachmentIDs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	for i, owner := range []uuid.UUID{alice, bob} {
		if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at)VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageIDs[i], owner, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order)VALUES($1,$2,$3,$4,0)`, attachmentIDs[i], messageIDs[i], fileID, "shared.bin"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = service.AuthorizedDownload(ctx, bob, attachmentIDs[0]); !errors.Is(err, files.ErrAttachmentNotFound) {
		t.Fatalf("cross owner=%v", err)
	}
	if _, err = service.AuthorizedDownload(ctx, alice, attachmentIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM messages WHERE id=$1`, messageIDs[0]); err != nil {
		t.Fatal(err)
	}
	if err = service.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatal("shared object deleted")
	}
	if _, err = db.Exec(ctx, `DELETE FROM messages WHERE id=$1`, messageIDs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET ready_at=$2 WHERE id=$1`, fileID, now); err != nil {
		t.Fatal(err)
	}
	if err = service.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatal("grace ignored")
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET ready_at=$2 WHERE id=$1`, fileID, now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = service.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object remains %v", err)
	}
	pendingID := writeObject(t, ctx, adapter, []byte("recover"))
	pendingHash := sha256.Sum256([]byte("recover"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at)VALUES($1,$2,7,'application/octet-stream','filesystem',$3,'PENDING',$4,$4)`, pendingID, pendingHash[:], storage.ObjectKey(pendingID).String(), now); err != nil {
		t.Fatal(err)
	}
	if err = service.Reconcile(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1`, pendingID).Scan(&status); err != nil || status != "READY" {
		t.Fatalf("pending recovery=%s %v", status, err)
	}
	missingID := uuid.Must(uuid.NewV7())
	missingHash := sha256.Sum256([]byte("missing"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at)VALUES($1,$2,7,'application/octet-stream','filesystem',$3,'PENDING',$4,$4)`, missingID, missingHash[:], storage.ObjectKey(missingID).String(), now); err != nil {
		t.Fatal(err)
	}
	if err = service.Reconcile(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_objects WHERE id=$1)`, missingID).Scan(&exists); err != nil || exists {
		t.Fatalf("missing pending remains %v %v", exists, err)
	}
}

func TestDownloadHTTPRegressionMatrixAndIntegrityContract(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	adapter, err := storage.NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	alice, bob := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, user := range []struct {
		id, name string
	}{{alice.String(), "alice"}, {bob.String(), "bob"}} {
		if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,$2,$2,'x','ACTIVE')`, user.id, user.name); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("0123456789")
	fileID := writeObject(t, ctx, adapter, data)
	hash := sha256.Sum256(data)
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,$3,'application/zip','filesystem',$4,'READY',$5,$5,$5)`, fileID, hash[:], len(data), storage.ObjectKey(fileID).String(), now); err != nil {
		t.Fatal(err)
	}
	attachments := make([]uuid.UUID, 2)
	messages := make([]uuid.UUID, 2)
	for i, owner := range []uuid.UUID{alice, bob} {
		messageID := uuid.Must(uuid.NewV7())
		messages[i] = messageID
		attachments[i] = uuid.Must(uuid.NewV7())
		if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageID, owner, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,$4,0)`, attachments[i], messageID, fileID, "MicYou-Android-2.0.1.apk"); err != nil {
			t.Fatal(err)
		}
	}
	handler := files.NewHandler(files.NewService(db, adapter))
	request := func(owner uuid.UUID, admin bool, attachment uuid.UUID, rangeHeader, ifNoneMatch, ifRange string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+attachment.String()+"/download", nil)
		r.Header.Set("Range", rangeHeader)
		r.Header.Set("If-None-Match", ifNoneMatch)
		r.Header.Set("If-Range", ifRange)
		r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: owner, IsAdmin: admin}}))
		w := httptest.NewRecorder()
		handler.DownloadAttachment(w, r, httpapi.AttachmentId(attachment))
		return w
	}
	full := request(alice, false, attachments[0], "", "", "")
	if full.Code != http.StatusOK || full.Body.String() != string(data) || full.Header().Get("Content-Length") != "10" || full.Header().Get("Content-Type") != "application/octet-stream" || !strings.Contains(full.Header().Get("Content-Disposition"), "MicYou-Android-2.0.1.apk") || full.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("full status=%d body=%q headers=%v", full.Code, full.Body.String(), full.Header())
	}
	zipAttachment := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'backup.zip',1)`, zipAttachment, messages[0], fileID); err != nil {
		t.Fatal(err)
	}
	zipDownload := request(alice, false, zipAttachment, "", "", "")
	if zipDownload.Code != http.StatusOK || zipDownload.Body.String() != string(data) || zipDownload.Header().Get("Content-Type") != "application/octet-stream" || !strings.Contains(zipDownload.Header().Get("Content-Disposition"), "backup.zip") {
		t.Fatalf("zip status=%d body=%q headers=%v", zipDownload.Code, zipDownload.Body.String(), zipDownload.Header())
	}
	etag := full.Header().Get("ETag")
	cases := []struct {
		name, rangeValue, ifRange, body, contentRange string
		status                                        int
	}{
		{"first byte", "bytes=0-0", "", "0", "bytes 0-0/10", 206},
		{"start end", "bytes=2-5", "", "2345", "bytes 2-5/10", 206},
		{"open ended", "bytes=7-", "", "789", "bytes 7-9/10", 206},
		{"suffix", "bytes=-3", "", "789", "bytes 7-9/10", 206},
		{"if range match", "bytes=2-5", etag, "2345", "bytes 2-5/10", 206},
		{"if range mismatch", "bytes=2-5", `"different"`, string(data), "", 200},
	}
	for _, tt := range cases {
		w := request(alice, false, attachments[0], tt.rangeValue, "", tt.ifRange)
		if w.Code != tt.status || w.Body.String() != tt.body || w.Header().Get("Content-Range") != tt.contentRange || w.Header().Get("Content-Length") != fmt.Sprint(len(tt.body)) || w.Header().Get("Content-Type") != "application/octet-stream" || !strings.Contains(w.Header().Get("Content-Disposition"), "MicYou-Android-2.0.1.apk") {
			t.Errorf("%s status=%d body=%q range=%q length=%q", tt.name, w.Code, w.Body.String(), w.Header().Get("Content-Range"), w.Header().Get("Content-Length"))
		}
	}
	for _, invalid := range []string{"bytes=10-", "bytes=0-1,3-4", "items=0-1"} {
		w := request(alice, false, attachments[0], invalid, "", "")
		if w.Code != http.StatusRequestedRangeNotSatisfiable || w.Header().Get("Content-Range") != "bytes */10" {
			t.Errorf("invalid %q status=%d range=%q", invalid, w.Code, w.Header().Get("Content-Range"))
		}
	}
	if w := request(alice, false, attachments[0], "", etag, ""); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("not modified status=%d body=%q", w.Code, w.Body.String())
	}
	for _, admin := range []bool{false, true} {
		if w := request(bob, admin, attachments[0], "", "", ""); w.Code != http.StatusNotFound {
			t.Fatalf("cross-owner admin=%v status=%d", admin, w.Code)
		}
	}
	if w := request(bob, false, attachments[1], "bytes=0-0", "", ""); w.Code != http.StatusPartialContent || w.Body.String() != "0" {
		t.Fatalf("owner-specific shared attachment status=%d body=%q", w.Code, w.Body.String())
	}
	preview := func(owner uuid.UUID, admin bool, attachment uuid.UUID, rangeHeader string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+attachment.String()+"/preview", nil)
		r.Header.Set("Range", rangeHeader)
		r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: owner, IsAdmin: admin}}))
		w := httptest.NewRecorder()
		handler.PreviewAttachment(w, r, httpapi.AttachmentId(attachment))
		return w
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET detected_mime='application/pdf' WHERE id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	if w := preview(alice, false, attachments[0], "bytes=2-5"); w.Code != http.StatusPartialContent || w.Body.String() != "2345" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline;") || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("preview range status=%d body=%q headers=%v", w.Code, w.Body.String(), w.Header())
	}
	for _, admin := range []bool{false, true} {
		if w := preview(bob, admin, attachments[0], ""); w.Code != http.StatusNotFound {
			t.Fatalf("cross-owner preview admin=%v status=%d", admin, w.Code)
		}
	}
	if _, err = db.Exec(ctx, `UPDATE messages SET trashed_at=$2::timestamptz,purge_at=($2::timestamptz)+interval '1 day' WHERE id=$1`, messages[0], now); err != nil {
		t.Fatal(err)
	}
	if w := preview(alice, false, attachments[0], ""); w.Code != http.StatusOK {
		t.Fatalf("trash owner preview status=%d", w.Code)
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET detected_mime='text/html' WHERE id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	if w := preview(alice, false, attachments[0], ""); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":"ATTACHMENT_PREVIEW_NOT_FOUND"`) || strings.Contains(w.Body.String(), fileID.String()) {
		t.Fatalf("unsafe preview status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET detected_mime='application/pdf' WHERE id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	if err = adapter.Delete(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatal(err)
	}
	w := request(alice, false, attachments[0], "", "", "")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"STORAGE_INTEGRITY_ERROR"`) || strings.Contains(w.Body.String(), fileID.String()) || strings.Contains(w.Body.String(), "objects/") {
		t.Fatalf("missing-final response status=%d body=%s", w.Code, w.Body.String())
	}
	writeAtKey := func(content []byte) {
		t.Helper()
		temp, createErr := adapter.CreateCommitTemp(ctx, storage.CommitTempKey(fileID))
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, createErr = temp.Write(content); createErr != nil {
			t.Fatal(createErr)
		}
		if createErr = temp.Sync(); createErr != nil {
			t.Fatal(createErr)
		}
		if createErr = temp.Close(); createErr != nil {
			t.Fatal(createErr)
		}
		if createErr = adapter.Commit(ctx, storage.CommitTempKey(fileID), storage.ObjectKey(fileID)); createErr != nil {
			t.Fatal(createErr)
		}
	}
	writeAtKey([]byte("short"))
	if w = request(alice, false, attachments[0], "", "", ""); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"STORAGE_INTEGRITY_ERROR"`) {
		t.Fatalf("wrong-size response status=%d body=%s", w.Code, w.Body.String())
	}
	if err = adapter.Delete(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "objects", fileID.String()), 0o750); err != nil {
		t.Fatal(err)
	}
	if w = request(alice, false, attachments[0], "", "", ""); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"STORAGE_INTEGRITY_ERROR"`) {
		t.Fatalf("non-regular response status=%d body=%s", w.Code, w.Body.String())
	}
	if err = os.Remove(filepath.Join(root, "objects", fileID.String())); err != nil {
		t.Fatal(err)
	}
	writeAtKey(data)
	for _, failure := range []struct {
		name string
		open bool
		read bool
	}{{name: "open", open: true}, {name: "read", read: true}} {
		handler = files.NewHandler(files.NewService(db, &downloadFailureAdapter{Adapter: adapter, openFailure: failure.open, readFailure: failure.read}))
		w = request(alice, false, attachments[0], "", "", "")
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"STORAGE_UNAVAILABLE"`) || strings.Contains(w.Body.String(), fileID.String()) {
			t.Fatalf("%s failure status=%d body=%s", failure.name, w.Code, w.Body.String())
		}
	}
	handler = files.NewHandler(files.NewService(db, adapter))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+attachments[0].String()+"/download", nil).WithContext(cancelled)
	r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: alice}}))
	w = httptest.NewRecorder()
	handler.DownloadAttachment(w, r, httpapi.AttachmentId(attachments[0]))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"STORAGE_UNAVAILABLE"`) {
		t.Fatalf("cancelled download status=%d body=%s", w.Code, w.Body.String())
	}
}

func writeObject(t *testing.T, ctx context.Context, a *storage.FilesystemStorageAdapter, data []byte) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	f, err := a.CreateCommitTemp(ctx, storage.CommitTempKey(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = a.Commit(ctx, storage.CommitTempKey(id), storage.ObjectKey(id)); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDerivativeDeleteBeforeSource(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	adapter, _ := storage.NewFilesystemStorageAdapter(root)
	if err := adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	service := files.NewService(db, adapter)
	now := time.Now().UTC()
	source := writeObject(t, ctx, adapter, []byte("source"))
	hash := sha256.Sum256([]byte("source"))
	if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at)VALUES($1,$2,6,'x','filesystem',$3,'DELETING',$4,$4,$4)`, source, hash[:], storage.ObjectKey(source).String(), now); err != nil {
		t.Fatal(err)
	}
	derivative := uuid.Must(uuid.NewV7())
	if err := os.WriteFile(filepath.Join(root, "derivatives", derivative.String()), []byte("d"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status)VALUES($1,$2,'TEST',$3,'x',1,'READY')`, derivative, source, storage.DerivativeKey(derivative).String()); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "derivatives", derivative.String())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("derivative remains %v", err)
	}
	if _, err := adapter.Stat(ctx, storage.ObjectKey(source)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("source remains %v", err)
	}
}

func TestReconcileMakesProgressPastPermanentItemFailures(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	base, err := storage.NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = base.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	adapter := &selectiveDeleteFailureAdapter{Adapter: base, fail: make(map[storage.Key]bool)}
	service := files.NewService(db, adapter)
	now := time.Now().UTC()

	malformed := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	valid := writeObject(t, ctx, base, []byte("valid"))
	missing := uuid.MustParse("00000000-0000-4000-8000-000000000003")
	for i, item := range []struct {
		id   uuid.UUID
		data string
		key  string
	}{{malformed, "bad", "not-an-object-key"}, {valid, "valid", storage.ObjectKey(valid).String()}, {missing, "missing", storage.ObjectKey(missing).String()}} {
		hash := sha256.Sum256([]byte(item.data))
		created := now.Add(time.Duration(i) * time.Second)
		if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at) VALUES($1,$2,$3,'x','filesystem',$4,'PENDING',$5,$5)`, item.id, hash[:], len(item.data), item.key, created); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Reconcile(ctx, 2); err == nil || !errors.Is(err, files.ErrStorageIntegrity) {
		t.Fatalf("malformed item error=%v", err)
	}
	var status string
	if err = db.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1`, valid).Scan(&status); err != nil || status != "READY" {
		t.Fatalf("later valid pending status=%q err=%v", status, err)
	}
	var exists bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_objects WHERE id=$1)`, missing).Scan(&exists); err != nil || exists {
		t.Fatalf("later missing pending exists=%v err=%v", exists, err)
	}

	failingDelete := writeObject(t, ctx, base, []byte("delete-a"))
	laterDelete := writeObject(t, ctx, base, []byte("delete-b"))
	adapter.fail[storage.ObjectKey(failingDelete)] = true
	for i, item := range []struct {
		id   uuid.UUID
		data string
	}{{failingDelete, "delete-a"}, {laterDelete, "delete-b"}} {
		hash := sha256.Sum256([]byte(item.data))
		created := now.Add(time.Duration(10+i) * time.Second)
		if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,$3,'x','filesystem',$4,'DELETING',$5,$5,$5)`, item.id, hash[:], len(item.data), storage.ObjectKey(item.id).String(), created); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Reconcile(ctx, 1); err == nil || !errors.Is(err, files.ErrStorageUnavailable) {
		t.Fatalf("delete failure error=%v", err)
	}
	if _, err = base.Stat(ctx, storage.ObjectKey(laterDelete)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("later deleting object remains: %v", err)
	}

	derivativeSource := writeObject(t, ctx, base, []byte("derivative-source"))
	derivativeID := uuid.Must(uuid.NewV7())
	derivativePath := filepath.Join(root, "derivatives", derivativeID.String())
	if err = os.WriteFile(derivativePath, []byte("derivative"), 0o640); err != nil {
		t.Fatal(err)
	}
	derivativeHash := sha256.Sum256([]byte("derivative-source"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,$3,'x','filesystem',$4,'DELETING',$5,$5,$5)`, derivativeSource, derivativeHash[:], len("derivative-source"), storage.ObjectKey(derivativeSource).String(), now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status) VALUES($1,$2,'TEST',$3,'x',$4,'READY')`, derivativeID, derivativeSource, storage.DerivativeKey(derivativeID).String(), len("derivative")); err != nil {
		t.Fatal(err)
	}
	adapter.fail[storage.DerivativeKey(derivativeID)] = true
	if err = service.Reconcile(ctx, 1); err == nil || !errors.Is(err, files.ErrStorageUnavailable) {
		t.Fatalf("derivative delete failure=%v", err)
	}
	if _, err = os.Stat(derivativePath); err != nil {
		t.Fatalf("failed derivative delete removed file: %v", err)
	}
	delete(adapter.fail, storage.DerivativeKey(derivativeID))
	if err = service.Reconcile(ctx, 1); err != nil && !errors.Is(err, files.ErrStorageIntegrity) && !errors.Is(err, files.ErrStorageUnavailable) {
		t.Fatalf("derivative retry=%v", err)
	}
	if _, err = os.Stat(derivativePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("derivative retry left file: %v", err)
	}

	firstTemp := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	laterTemp := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	for _, id := range []uuid.UUID{firstTemp, laterTemp} {
		f, createErr := base.CreateCommitTemp(ctx, storage.CommitTempKey(id))
		if createErr != nil {
			t.Fatal(createErr)
		}
		_ = f.Close()
	}
	adapter.fail[storage.CommitTempKey(firstTemp)] = true
	if err = service.Reconcile(ctx, 1); err == nil || !errors.Is(err, files.ErrStorageUnavailable) {
		t.Fatalf("temp cleanup failure=%v", err)
	}
	if _, err = base.Stat(ctx, storage.CommitTempKey(laterTemp)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("later orphan temp remains: %v", err)
	}
}
