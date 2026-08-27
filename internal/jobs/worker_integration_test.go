//go:build integration

package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type workerClock struct{ now time.Time }

func (c workerClock) Now() time.Time { return c.now }

type handlerFunc func(context.Context, Job) error

func (f handlerFunc) Handle(ctx context.Context, j Job) error { return f(ctx, j) }

func TestWorkerFailurePanicAndConcurrentClaim(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	repo := NewRepository(db, id.UUIDv7{})
	now := time.Now()
	clock := workerClock{now}
	wake := NewWake()
	cases := []struct {
		name, kind, wantStatus, wantCode string
		handler                          Handler
	}{{"retry", "RETRY", "PENDING", "STORAGE_UNAVAILABLE", handlerFunc(func(context.Context, Job) error { return Retryable("STORAGE_UNAVAILABLE", "storage is unavailable") })}, {"permanent", "PERMANENT", "FAILED", "THUMBNAIL_DECODE_FAILED", handlerFunc(func(context.Context, Job) error {
		return Permanent("THUMBNAIL_DECODE_FAILED", "source image cannot be decoded")
	})}, {"panic", "PANIC", "PENDING", "JOB_HANDLER_PANIC", handlerFunc(func(context.Context, Job) error { panic("private panic payload") })}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobID := uuid.Must(uuid.NewV7())
			if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,next_run_at,created_at,updated_at) VALUES($1,$2,'TEST','PENDING',$3,$3,$3)`, jobID, tc.kind, now); err != nil {
				t.Fatal(err)
			}
			worker := NewWorker(repo, map[string]Handler{tc.kind: tc.handler}, wake, clock)
			if _, err := worker.drainDue(ctx); err != nil {
				t.Fatal(err)
			}
			var status, code, summary string
			var attempts int
			if err := db.QueryRow(ctx, `SELECT status,attempts,last_error_code,last_error_summary FROM background_jobs WHERE id=$1`, jobID).Scan(&status, &attempts, &code, &summary); err != nil {
				t.Fatal(err)
			}
			if status != tc.wantStatus || attempts != 1 || code != tc.wantCode {
				t.Fatalf("status=%s attempts=%d code=%s summary=%q", status, attempts, code, summary)
			}
			if summary == "private panic payload" {
				t.Fatal("panic payload leaked")
			}
		})
	}
	jobID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,next_run_at,created_at,updated_at) VALUES($1,'ONCE','TEST','PENDING',$2,$2,$2)`, jobID, now); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	once := sync.Once{}
	handler := handlerFunc(func(context.Context, Job) error {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return nil
	})
	first := NewWorker(repo, map[string]Handler{"ONCE": handler}, wake, clock)
	second := NewWorker(repo, map[string]Handler{"ONCE": handler}, wake, clock)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() { defer wg.Done(); _, err := first.drainDue(ctx); errs <- err }()
	<-started
	go func() { defer wg.Done(); _, err := second.drainDue(ctx); errs <- err }()
	time.Sleep(10 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls=%d", calls.Load())
	}
	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM background_jobs WHERE id=$1`, jobID).Scan(&status); err != nil || status != "COMPLETED" {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func installOneShotTransitionFault(t *testing.T, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, transition string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE SEQUENCE worker_fault_seq`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION worker_fault_once() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF nextval('worker_fault_seq')=1 THEN RAISE EXCEPTION 'injected transient fault'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE TRIGGER worker_fault BEFORE UPDATE ON background_jobs FOR EACH ROW WHEN (NEW.status=`+transition+`) EXECUTE FUNCTION worker_fault_once()`); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerContinuesAfterTransientClaimFault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := postgresutil.NewDatabase(t)
	installOneShotTransitionFault(t, db, `'RUNNING'`)
	now := time.Now()
	jobID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,next_run_at,created_at,updated_at) VALUES($1,'CLAIM_RECOVERS','TEST','PENDING',$2,$2,$2)`, jobID, now); err != nil {
		t.Fatal(err)
	}
	processed := make(chan struct{}, 1)
	worker := NewWorker(NewRepository(db, id.UUIDv7{}), map[string]Handler{"CLAIM_RECOVERS": handlerFunc(func(context.Context, Job) error {
		processed <- struct{}{}
		return nil
	})}, NewWake(), workerClock{now})
	worker.poll = 10 * time.Millisecond
	worker.SetErrorReporter(func(error) {})
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-processed:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not recover from claim fault")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkerRetriesResultPersistenceWithoutReexecutingHandler(t *testing.T) {
	for _, tc := range []struct {
		name, transition, wantStatus string
		handlerErr                   error
	}{{"complete", `'COMPLETED'`, "COMPLETED", nil}, {"retryable-fail", `'PENDING'`, "PENDING", Retryable("STORAGE_UNAVAILABLE", "storage unavailable")}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := postgresutil.NewDatabase(t)
			installOneShotTransitionFault(t, db, tc.transition)
			now := time.Now()
			jobID := uuid.Must(uuid.NewV7())
			if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,next_run_at,created_at,updated_at) VALUES($1,'PERSIST_RETRY','TEST','PENDING',$2,$2,$2)`, jobID, now); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			worker := NewWorker(NewRepository(db, id.UUIDv7{}), map[string]Handler{"PERSIST_RETRY": handlerFunc(func(context.Context, Job) error {
				calls.Add(1)
				return tc.handlerErr
			})}, NewWake(), workerClock{now})
			worker.retry = 10 * time.Millisecond
			worker.SetErrorReporter(func(error) {})
			if _, err := worker.drainDue(ctx); err != nil {
				t.Fatal(err)
			}
			var status string
			if err := db.QueryRow(ctx, `SELECT status FROM background_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || status != tc.wantStatus {
				t.Fatalf("handler calls=%d status=%s", calls.Load(), status)
			}
		})
	}
}
