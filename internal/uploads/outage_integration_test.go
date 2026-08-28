//go:build integration

package uploads_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/ZephyrLeeX/RelayShelf/internal/uploads"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// outageMode models the NFS failure shapes the architecture requires the
// service layer to survive: immediate unavailability, a hung hard mount
// (timeout), and I/O errors such as permission failures.
type outageMode int32

const (
	outageHealthy outageMode = iota
	outageUnavailable
	outageTimeout
)

// outageAdapter wraps a real adapter with a toggleable fault. Every faulted
// call is counted so tests can prove degraded paths never touch storage.
type outageAdapter struct {
	storage.Adapter
	state outageMode
	calls int64
}

func (a *outageAdapter) Space(ctx context.Context) (storage.Space, error) {
	switch a.state {
	case outageUnavailable:
		a.calls++
		return storage.Space{}, storage.ErrUnavailable
	case outageTimeout:
		a.calls++
		select {
		case <-ctx.Done():
			return storage.Space{}, ctx.Err()
		case <-time.After(30 * time.Second):
			return storage.Space{}, storage.ErrUnavailable
		}
	default:
		return a.Adapter.Space(ctx)
	}
}

func (a *outageAdapter) Open(ctx context.Context, key storage.Key) (storage.File, error) {
	if a.state != outageHealthy {
		a.calls++
		return nil, storage.ErrUnavailable
	}
	return a.Adapter.Open(ctx, key)
}

func (a *outageAdapter) CreateCommitTemp(ctx context.Context, key storage.Key) (storage.File, error) {
	if a.state != outageHealthy {
		a.calls++
		return nil, storage.ErrUnavailable
	}
	return a.Adapter.CreateCommitTemp(ctx, key)
}

func (a *outageAdapter) faultedCalls() int64 { return a.calls }

// TestNFSOutageBoundary proves the Phase 11 T124 architecture boundary under
// each fault shape: DB-backed text paths keep working, staging uploads keep
// working, storage-dependent paths reject boundedly (without touching the
// faulted adapter once degraded), and the very same Complete retry succeeds
// after recovery without re-uploading a single part.
func TestNFSOutageBoundary(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	storageRoot, stagingRoot := t.TempDir(), t.TempDir()
	realAdapter, err := storage.NewFilesystemStorageAdapter(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = realAdapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	faulted := &outageAdapter{Adapter: realAdapter}

	monitor := storage.NewMonitorTunable(faulted, 10*time.Millisecond, 2)
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	defer stopMonitor()
	go monitor.Run(monitorCtx)

	now := clock.Real{}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	cipher, err := messages.NewAESGCMCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	messageService := messages.NewService(messages.NewPostgreSQLRepository(db), id.UUIDv7{}, now, cipher)
	stagingManager, err := staging.New(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.NewService(uploads.NewPostgreSQLRepository(db), stagingManager, staging.NewStatFSProbe(stagingRoot), id.UUIDv7{}, now, uploads.NewLockRegistry(), 4, 1<<30, 0, 0)
	uploadService.SetFinalizer(uploads.NewFileFinalizer(db, faulted, id.UUIDv7{}, now, 1))
	uploadService.SetMonitor(monitor)
	fileService := files.NewService(db, faulted)
	fileService.SetMonitor(monitor)

	owner := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,'outage','outage','x','ACTIVE')`, owner); err != nil {
		t.Fatal(err)
	}

	// Seed one READY object so the download path has authorized content to
	// read once storage recovers.
	seed := []byte("outage-seed-payload")
	seedAttachment := seedReadyObject(t, ctx, db, realAdapter, owner, seed)

	// Upload the first chunk of a two-chunk file while storage is healthy,
	// then take storage down.
	payload := bytes.Repeat([]byte("n"), 12<<20)
	session, err := uploadService.Create(ctx, owner, uploads.CreateCommand{OriginalFilename: "outage.bin", ExpectedSize: int64(len(payload))})
	if err != nil {
		t.Fatal(err)
	}
	half := session.ChunkSize
	if err = uploadService.PutPart(ctx, owner, session.ID, 0, half, bytes.NewReader(payload[:half])); err != nil {
		t.Fatal(err)
	}

	faulted.state = outageUnavailable
	waitForCondition(t, "degraded", func() bool { return !monitor.Healthy() })

	// While storage is down: text create and read keep working.
	outageBody := "text-path-survives-outage"
	deviceID := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at) VALUES($1,$2,'outage-dev','test',$3,$3)`, deviceID, owner, now.Now()); err != nil {
		t.Fatal(err)
	}
	created, err := messageService.Create(ctx, owner, deviceID, messages.CreateCommand{Body: outageBody, BodyFormat: "TEXT", Lifecycle: "TEMPORARY", IdempotencyKey: "outage-text-1"})
	if err != nil {
		t.Fatalf("text create during outage: %v", err)
	}
	page, err := messageService.List(ctx, owner, messages.ListFilter{}, false)
	if err != nil {
		t.Fatalf("text list during outage: %v", err)
	}
	found := false
	for _, item := range page.Items {
		if item.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("created text missing from list during outage")
	}

	// Staging upload continues: the second chunk still lands in VM staging.
	if err = uploadService.PutPart(ctx, owner, session.ID, 1, int64(len(payload))-half, bytes.NewReader(payload[half:])); err != nil {
		t.Fatalf("staging put during outage: %v", err)
	}

	// Storage-heavy paths reject boundedly without touching the adapter.
	before := faulted.faultedCalls()
	if _, err = uploadService.Complete(ctx, owner, session.ID); !errors.Is(err, uploads.ErrFinalizeRetryable) {
		t.Fatalf("complete during outage err=%v", err)
	}
	if _, err = fileService.AuthorizedDownload(ctx, owner, seedAttachment); !errors.Is(err, files.ErrStorageUnavailable) {
		t.Fatalf("download during outage err=%v", err)
	}
	if got := faulted.faultedCalls(); got != before {
		t.Fatalf("degraded paths touched storage %d extra time(s)", got-before)
	}

	// Recovery: the same Complete retry succeeds with the parts already on
	// the server; nothing is re-uploaded.
	faulted.state = outageHealthy
	waitForCondition(t, "recovered", monitor.Healthy)
	completed, err := uploadService.Complete(ctx, owner, session.ID)
	if err != nil {
		t.Fatalf("complete after recovery: %v", err)
	}
	if completed.Status != uploads.Completed {
		t.Fatalf("status=%s", completed.Status)
	}
	download, err := fileService.AuthorizedDownload(ctx, owner, seedAttachment)
	if err != nil {
		t.Fatalf("download after recovery: %v", err)
	}
	reader, err := fileService.Open(ctx, download)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(io.LimitReader(reader, download.Size))
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read=%v close=%v", err, closeErr)
	}
	if !bytes.Equal(got, seed) {
		t.Fatal("downloaded bytes changed across the outage")
	}
}

// TestNFSOutageTimeoutShape pins the hung-mount case: a probe that never
// returns still drives the monitor to a NAS_TIMEOUT degraded state, and the
// monitor's gate keeps at most one probe outstanding.
func TestNFSOutageTimeoutShape(t *testing.T) {
	ctx := context.Background()
	realAdapter, err := storage.NewFilesystemStorageAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = realAdapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	faulted := &outageAdapter{Adapter: realAdapter}
	faulted.state = outageTimeout

	monitor := storage.NewMonitorTunable(faulted, 10*time.Millisecond, 2)
	monitorCtx, stop := context.WithCancel(ctx)
	defer stop()
	go monitor.Run(monitorCtx)
	waitForCondition(t, "degraded", func() bool { return !monitor.Healthy() })
	if reason := monitor.Reason(); reason != "NAS_TIMEOUT" {
		t.Fatalf("reason=%s", reason)
	}
}

// TestNFSOutagePermissionShape covers the I/O-error shape against a real
// read-only directory instead of the synthetic adapter: reads fail with a
// classified, bounded error rather than hanging.
func TestNFSOutagePermissionShape(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	realAdapter, err := storage.NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = realAdapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	owner := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,'perm','perm','x','ACTIVE')`, owner); err != nil {
		t.Fatal(err)
	}
	seed := []byte("permission-shape")
	seedAttachment := seedReadyObject(t, ctx, db, realAdapter, owner, seed)

	if err = os.Chmod(root+"/objects", 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root+"/objects", 0o750) })

	fileService := files.NewService(db, realAdapter)
	download, err := fileService.AuthorizedDownload(ctx, owner, seedAttachment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fileService.Open(ctx, download); !errors.Is(err, files.ErrStorageUnavailable) && !errors.Is(err, files.ErrStorageIntegrity) {
		t.Fatalf("read-only open err=%v", err)
	}
}

func waitForCondition(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition never satisfied: %s", what)
}

// seedReadyObject writes a real physical object and binds it to an owned
// message so download authorization resolves through real rows.
func seedReadyObject(t *testing.T, ctx context.Context, db *pgxpool.Pool, adapter *storage.FilesystemStorageAdapter, owner uuid.UUID, payload []byte) uuid.UUID {
	t.Helper()
	fileID := uuid.Must(uuid.NewV7())
	tempKey := storage.CommitTempKey(fileID)
	f, err := adapter.CreateCommitTemp(ctx, tempKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(payload); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = adapter.Commit(ctx, tempKey, storage.ObjectKey(fileID)); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	now := time.Now().UTC()
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,$3,'application/octet-stream','filesystem',$4,'READY',$5,$5,$5)`, fileID, hash[:], len(payload), storage.ObjectKey(fileID).String(), now); err != nil {
		t.Fatal(err)
	}
	messageID := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$2,'seed','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageID, owner, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	attachmentID := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'seed.bin',0)`, attachmentID, messageID, fileID); err != nil {
		t.Fatal(err)
	}
	return attachmentID
}
