//go:build integration

package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/jobs"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const tinyWebP = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func TestThumbnailerFormatsAndSizing(t *testing.T) {
	webpBytes, err := base64.StdEncoding.DecodeString(tinyWebP)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, mime, wantMime string
		data                 func() []byte
		wantW, wantH         int
	}{
		{name: "jpeg", mime: "image/jpeg", wantMime: "image/jpeg", data: func() []byte { return encodeJPEG(image.NewNRGBA(image.Rect(0, 0, 800, 600))) }, wantW: 512, wantH: 384},
		{name: "png-alpha", mime: "image/png", wantMime: "image/png", data: func() []byte {
			im := image.NewNRGBA(image.Rect(0, 0, 100, 50))
			im.Set(1, 1, color.NRGBA{R: 255, A: 80})
			return encodePNG(im)
		}, wantW: 100, wantH: 50},
		{name: "gif-first-frame", mime: "image/gif", wantMime: "image/png", data: func() []byte {
			im := image.NewPaletted(image.Rect(0, 0, 40, 20), color.Palette{color.Black, color.White})
			var b bytes.Buffer
			_ = gif.Encode(&b, im, nil)
			return b.Bytes()
		}, wantW: 40, wantH: 20},
		{name: "webp", mime: "image/webp", wantMime: "image/png", data: func() []byte { return webpBytes }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := postgresutil.NewDatabase(t)
			adapter, err := storage.NewFilesystemStorageAdapter(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = adapter.EnsureLayout(ctx); err != nil {
				t.Fatal(err)
			}
			sourceID := writeSource(t, ctx, db, adapter, tc.mime, tc.data())
			thumbnailer := files.NewThumbnailer(db, adapter, id.UUIDv7{}, time.Now)
			err = thumbnailer.Handle(ctx, jobs.Job{Type: jobs.TypeGenerateThumbnail, SubjectType: jobs.SubjectFileObject, SubjectID: &sourceID})
			if err != nil {
				t.Fatal(err)
			}
			var key, mime, status string
			var size int64
			if err = db.QueryRow(ctx, `SELECT storage_key,mime,size_bytes,status FROM file_derivatives WHERE source_file_id=$1 AND kind='THUMBNAIL_SMALL'`, sourceID).Scan(&key, &mime, &size, &status); err != nil {
				t.Fatal(err)
			}
			if status != "READY" || mime != tc.wantMime || size <= 0 {
				t.Fatalf("status=%s mime=%s size=%d", status, mime, size)
			}
			f, err := adapter.Open(ctx, storage.Key(key))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			cfg, _, err := image.DecodeConfig(f)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantW > 0 && (cfg.Width != tc.wantW || cfg.Height != tc.wantH) {
				t.Fatalf("size=%dx%d want=%dx%d", cfg.Width, cfg.Height, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestThumbnailFailureDoesNotAffectOriginal(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	adapter, err := storage.NewFilesystemStorageAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	sourceID := writeSource(t, ctx, db, adapter, "image/jpeg", []byte("not an image"))
	thumbnailer := files.NewThumbnailer(db, adapter, id.UUIDv7{}, time.Now)
	err = thumbnailer.Handle(ctx, jobs.Job{Type: jobs.TypeGenerateThumbnail, SubjectType: jobs.SubjectFileObject, SubjectID: &sourceID})
	var handlerErr *jobs.HandlerError
	if !errors.As(err, &handlerErr) || !handlerErr.Permanent || handlerErr.Code != "THUMBNAIL_DECODE_FAILED" {
		t.Fatalf("error=%v", err)
	}
	var sourceStatus, derivativeStatus string
	if err = db.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1`, sourceID).Scan(&sourceStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT status FROM file_derivatives WHERE source_file_id=$1`, sourceID).Scan(&derivativeStatus); err != nil {
		t.Fatal(err)
	}
	if sourceStatus != "READY" || derivativeStatus != "FAILED" {
		t.Fatalf("source=%s derivative=%s", sourceStatus, derivativeStatus)
	}
}

func TestThumbnailAttachmentAuthorizationAndIntegrity(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	adapter, err := storage.NewFilesystemStorageAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	alice, bob := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, u := range []struct {
		id   uuid.UUID
		name string
	}{{alice, "alice-thumb"}, {bob, "bob-thumb"}} {
		if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,$2,$2,'x','ACTIVE')`, u.id, u.name); err != nil {
			t.Fatal(err)
		}
	}
	sourceID := writeSource(t, ctx, db, adapter, "image/png", encodePNG(image.NewNRGBA(image.Rect(0, 0, 20, 10))))
	messageID, attachmentID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageID, alice, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'private.png',0)`, attachmentID, messageID, sourceID); err != nil {
		t.Fatal(err)
	}
	thumbnailer := files.NewThumbnailer(db, adapter, id.UUIDv7{}, time.Now)
	if err = thumbnailer.Handle(ctx, jobs.Job{Type: jobs.TypeGenerateThumbnail, SubjectType: jobs.SubjectFileObject, SubjectID: &sourceID}); err != nil {
		t.Fatal(err)
	}
	service := files.NewService(db, adapter)
	if _, err = service.AuthorizedThumbnail(ctx, alice, attachmentID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.AuthorizedThumbnail(ctx, bob, attachmentID); !errors.Is(err, files.ErrThumbnailNotFound) {
		t.Fatalf("foreign access=%v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE messages SET trashed_at=$2::timestamptz,purge_at=$2::timestamptz+interval '1 day' WHERE id=$1`, messageID, now); err != nil {
		t.Fatal(err)
	}
	handler := files.NewHandler(service)
	request := func(user uuid.UUID) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/attachments/"+attachmentID.String()+"/thumbnail", nil)
		r = r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: user, IsAdmin: true}}))
		w := httptest.NewRecorder()
		handler.GetAttachmentThumbnail(w, r, httpapi.AttachmentId(attachmentID))
		return w
	}
	w := request(alice)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("owner status=%d headers=%v", w.Code, w.Header())
	}
	if w = request(bob); w.Code != http.StatusNotFound {
		t.Fatalf("foreign admin status=%d", w.Code)
	}
	var derivativeKey string
	if err = db.QueryRow(ctx, `SELECT storage_key FROM file_derivatives WHERE source_file_id=$1`, sourceID).Scan(&derivativeKey); err != nil {
		t.Fatal(err)
	}
	if err = adapter.Delete(ctx, storage.Key(derivativeKey)); err != nil {
		t.Fatal(err)
	}
	w = request(alice)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"STORAGE_INTEGRITY_ERROR"`) || strings.Contains(w.Body.String(), derivativeKey) {
		t.Fatalf("integrity status=%d body=%s", w.Code, w.Body.String())
	}
}

type blockingCreateAdapter struct {
	storage.Adapter
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (a *blockingCreateAdapter) CreateCommitTemp(ctx context.Context, key storage.Key) (storage.File, error) {
	a.once.Do(func() {
		close(a.started)
		select {
		case <-a.release:
		case <-ctx.Done():
		}
	})
	return a.Adapter.CreateCommitTemp(ctx, key)
}

func TestThumbnailGenerationRacesSourceGC(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	base, err := storage.NewFilesystemStorageAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = base.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	user := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,'gc-race-user','gc','x','ACTIVE')`, user); err != nil {
		t.Fatal(err)
	}
	sourceID := writeSource(t, ctx, db, base, "image/png", encodePNG(image.NewNRGBA(image.Rect(0, 0, 600, 400))))
	if _, err = db.Exec(ctx, `UPDATE file_objects SET ready_at=$2 WHERE id=$1`, sourceID, now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	messageID, attachmentID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageID, user, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'race.png',0)`, attachmentID, messageID, sourceID); err != nil {
		t.Fatal(err)
	}
	blocking := &blockingCreateAdapter{Adapter: base, started: make(chan struct{}), release: make(chan struct{})}
	thumbnailer := files.NewThumbnailer(db, blocking, id.UUIDv7{}, time.Now)
	done := make(chan error, 1)
	go func() {
		done <- thumbnailer.Handle(ctx, jobs.Job{Type: jobs.TypeGenerateThumbnail, SubjectType: jobs.SubjectFileObject, SubjectID: &sourceID})
	}()
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("thumbnail did not reach storage commit")
	}
	var derivativeKey string
	if err = db.QueryRow(ctx, `SELECT storage_key FROM file_derivatives WHERE source_file_id=$1`, sourceID).Scan(&derivativeKey); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM messages WHERE id=$1`, messageID); err != nil {
		t.Fatal(err)
	}
	if err = files.NewService(db, base).GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	close(blocking.release)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("thumbnail did not finish")
	}
	var exists bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_objects WHERE id=$1) OR EXISTS(SELECT 1 FROM file_derivatives WHERE source_file_id=$1)`, sourceID).Scan(&exists); err != nil || exists {
		t.Fatalf("database orphan exists=%v err=%v", exists, err)
	}
	if _, err = base.Stat(ctx, storage.Key(derivativeKey)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("physical derivative remains: %v", err)
	}
}

func writeSource(t *testing.T, ctx context.Context, db *pgxpool.Pool, adapter storage.Adapter, mime string, data []byte) uuid.UUID {
	t.Helper()
	sourceID := uuid.Must(uuid.NewV7())
	temp := storage.CommitTempKey(sourceID)
	f, err := adapter.CreateCommitTemp(ctx, temp)
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
	if err = adapter.Commit(ctx, temp, storage.ObjectKey(sourceID)); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	now := time.Now()
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,$3,$4,'filesystem',$5,'READY',$6,$6,$6)`, sourceID, digest[:], len(data), mime, storage.ObjectKey(sourceID).String(), now); err != nil {
		t.Fatal(err)
	}
	return sourceID
}

func encodeJPEG(im image.Image) []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, im, nil)
	return b.Bytes()
}
func encodePNG(im image.Image) []byte { var b bytes.Buffer; _ = png.Encode(&b, im); return b.Bytes() }
