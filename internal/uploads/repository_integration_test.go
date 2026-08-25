//go:build integration

package uploads_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/uploads"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationClock struct{ now time.Time }

func (c *integrationClock) Now() time.Time { return c.now }

type healthySpace struct{}

func (healthySpace) Probe() (staging.Space, error) {
	return staging.Space{AvailableBytes: 1 << 40, TotalBytes: 2 << 40}, nil
}

type uploadFixture struct {
	db         *pgxpool.Pool
	root       string
	stage      *staging.Manager
	clock      *integrationClock
	alice, bob uuid.UUID
}

func newUploadFixture(t *testing.T) *uploadFixture {
	t.Helper()
	db := testutil.NewDatabase(t)
	f := &uploadFixture{db: db, root: t.TempDir(), clock: &integrationClock{time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)}, alice: uuid.Must(uuid.NewV7()), bob: uuid.Must(uuid.NewV7())}
	for _, u := range []struct {
		id   uuid.UUID
		name string
	}{{f.alice, "alice"}, {f.bob, "bob"}} {
		if _, err := db.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,status)VALUES($1,$2,$2,'unused','ACTIVE')`, u.id, u.name); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.stage, err = staging.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *uploadFixture) service(repo uploads.Repository, max int64) *uploads.Service {
	return uploads.NewService(repo, f.stage, healthySpace{}, id.UUIDv7{}, f.clock, uploads.NewLockRegistry(), 8, max, 0, 0)
}

func TestUploadLifecycleOwnerTTLAndFailureWindows(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	repo := uploads.NewPostgreSQLRepository(f.db)
	service := f.service(repo, 1<<30)
	a, err := service.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "archive.zip", ExpectedSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID.Version() != 7 || a.ChunkSize != uploads.ChunkSize || !a.ExpiresAt.Equal(f.clock.now.Add(24*time.Hour)) {
		t.Fatalf("created=%+v", a)
	}
	if _, err = service.Get(ctx, f.bob, a.ID); !errors.Is(err, uploads.ErrNotFound) {
		t.Fatalf("foreign status=%v", err)
	}
	if _, err = f.db.Exec(ctx, `UPDATE system_settings SET upload_retention_hours=12,max_file_size_bytes=10 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	b, err := service.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "b", ExpectedSize: 0})
	if err != nil || !b.ExpiresAt.Equal(f.clock.now.Add(12*time.Hour)) {
		t.Fatalf("ttl snapshot=%+v %v", b, err)
	}
	if !a.ExpiresAt.Equal(f.clock.now.Add(24 * time.Hour)) {
		t.Fatal("old TTL changed")
	}
	if _, err = service.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "large", ExpectedSize: 11}); !errors.Is(err, uploads.ErrFileTooLarge) {
		t.Fatalf("max file=%v", err)
	}
	if err = service.PutPart(ctx, f.alice, a.ID, 0, 4, bytes.NewReader([]byte("data"))); err != nil {
		t.Fatal(err)
	}
	status, err := service.Get(ctx, f.alice, a.ID)
	if err != nil || len(status.CompletedParts) != 1 || status.CompletedParts[0] != 0 {
		t.Fatalf("resume=%+v %v", status, err)
	}
	if _, err = service.Complete(ctx, f.bob, a.ID); !errors.Is(err, uploads.ErrNotFound) {
		t.Fatalf("foreign complete=%v", err)
	}
	done, err := service.Complete(ctx, f.alice, a.ID)
	if err != nil || done.Status != uploads.Completing {
		t.Fatalf("complete=%+v %v", done, err)
	}
	retry, err := service.Complete(ctx, f.alice, a.ID)
	if err != nil || retry.Status != uploads.Completing {
		t.Fatalf("complete retry=%+v %v", retry, err)
	}

	boom := errors.New("injected db marker failure")
	createFailure := uploads.NewPostgreSQLRepositoryWithFailureHooks(f.db, uploads.FailureHooks{BeforeCreateCommit: func() error { return boom }})
	if _, err = f.service(createFailure, 1<<30).Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "orphan-window", ExpectedSize: 1}); !errors.Is(err, boom) {
		t.Fatalf("create injection=%v", err)
	}
	owned, scanErr := f.stage.OwnedFiles()
	if scanErr != nil || len(owned) != 2 { // a and zero-byte b remain active; failed create was removed.
		t.Fatalf("failed create staging leaked: owned=%v err=%v", owned, scanErr)
	}
	failRepo := uploads.NewPostgreSQLRepositoryWithFailureHooks(f.db, uploads.FailureHooks{BeforePartMarker: func() error { return boom }})
	failedService := f.service(failRepo, 1<<30)
	c, err := failedService.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "c", ExpectedSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err = failedService.PutPart(ctx, f.alice, c.ID, 0, 4, bytes.NewReader([]byte("sync"))); !errors.Is(err, boom) {
		t.Fatalf("marker failure=%v", err)
	}
	var markers int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM upload_parts WHERE upload_session_id=$1`, c.ID).Scan(&markers); err != nil || markers != 0 {
		t.Fatalf("marker persisted count=%d err=%v", markers, err)
	}
	if err = service.PutPart(ctx, f.alice, c.ID, 0, 4, bytes.NewReader([]byte("safe"))); err != nil {
		t.Fatal(err)
	}

	failComplete := uploads.NewPostgreSQLRepositoryWithFailureHooks(f.db, uploads.FailureHooks{BeforeCompleteTransition: func() error { return boom }})
	if _, err = f.service(failComplete, 1<<30).Complete(ctx, f.alice, c.ID); !errors.Is(err, boom) {
		t.Fatalf("complete injection=%v", err)
	}
	var state string
	if err = f.db.QueryRow(ctx, `SELECT status FROM upload_sessions WHERE id=$1`, c.ID).Scan(&state); err != nil || state != uploads.Uploading {
		t.Fatalf("rollback state=%s err=%v", state, err)
	}
}

func TestConcurrentReservationCannotOversubscribe(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	service := f.service(uploads.NewPostgreSQLRepository(f.db), 100)
	if _, err := service.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "existing", ExpectedSize: 40}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "candidate", ExpectedSize: 50})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, full := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, uploads.ErrStagingFull) {
			full++
		} else {
			t.Fatalf("unexpected create error %v", err)
		}
	}
	if success != 1 || full != 1 {
		t.Fatalf("success=%d full=%d", success, full)
	}
	var total int64
	if err := f.db.QueryRow(ctx, `SELECT COALESCE(sum(expected_size),0) FROM upload_sessions WHERE status IN ('CREATED','UPLOADING','COMPLETING','FAILED')`).Scan(&total); err != nil || total > 100 {
		t.Fatalf("reservation=%d err=%v", total, err)
	}
}

func TestExpirationAndOrphanReconciliation(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	service := f.service(uploads.NewPostgreSQLRepository(f.db), 1<<30)
	row, err := service.Create(ctx, f.alice, uploads.CreateCommand{OriginalFilename: "expire", ExpectedSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.PutPart(ctx, f.alice, row.ID, 0, 1, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	f.clock.now = row.ExpiresAt
	if err = service.ExpireDueUploads(ctx, 10); err != nil {
		t.Fatal(err)
	}
	exists, err := f.stage.Exists(row.ID)
	if err != nil || exists {
		t.Fatalf("expired file exists=%v err=%v", exists, err)
	}
	var count int
	if err = f.db.QueryRow(ctx, `SELECT count(*) FROM upload_parts WHERE upload_session_id=$1`, row.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("parts=%d %v", count, err)
	}
	orphan := uuid.Must(uuid.NewV7())
	if err = f.stage.Create(orphan, 0); err != nil {
		t.Fatal(err)
	}
	if err = service.ReconcileStaging(ctx, 100); err != nil {
		t.Fatal(err)
	}
	exists, err = f.stage.Exists(orphan)
	if err != nil || exists {
		t.Fatalf("orphan exists=%v err=%v", exists, err)
	}
}
