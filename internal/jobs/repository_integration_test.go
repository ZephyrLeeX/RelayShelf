//go:build integration

package jobs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/jobs"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
)

func TestJobClaimRetryAndRecovery(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewEmptyDatabase(t)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := jobs.NewRepository(db, id.UUIDv7{})
	now := time.Now().UTC().Truncate(time.Microsecond)
	first, second := uuid.New(), uuid.New()
	source := uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,attempts,next_run_at,created_at,updated_at) VALUES($1,'GENERATE_THUMBNAIL','FILE_OBJECT',$3,'PENDING',0,$4,$4,$4),($2,'OTHER','TEST',NULL,'PENDING',0,$4+interval '1 second',$4,$4)`, first, second, source, now); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := repo.Claim(ctx, now)
	if err != nil || !ok || claimed.ID != first || claimed.Attempts != 1 {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err = repo.Fail(ctx, claimed, "STORAGE_UNAVAILABLE", "storage is unavailable", false, now, jobs.DefaultMaxAttempts); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	var next time.Time
	if err = db.QueryRow(ctx, `SELECT status,attempts,next_run_at FROM background_jobs WHERE id=$1`, first).Scan(&status, &attempts, &next); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" || attempts != 1 || !next.Equal(now.Add(30*time.Second)) {
		t.Fatalf("status=%s attempts=%d next=%s", status, attempts, next)
	}
	if _, err = db.Exec(ctx, `UPDATE background_jobs SET status='RUNNING',attempts=8,started_at=$2 WHERE id=$1`, first, now.Add(-16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RecoverStuck(ctx, now, jobs.DefaultStuckTimeout, jobs.DefaultMaxAttempts, 200)
	if err != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, err)
	}
	if err = db.QueryRow(ctx, `SELECT status FROM background_jobs WHERE id=$1`, first).Scan(&status); err != nil || status != "FAILED" {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func TestClaimSkipLocked(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewEmptyDatabase(t)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := jobs.NewRepository(db, id.UUIDv7{})
	now := time.Now()
	first, second := uuid.New(), uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,next_run_at,created_at,updated_at) VALUES($1,'A','X','PENDING',$3,$3,$3),($2,'B','X','PENDING',$3,$3+interval '1 second',$3)`, first, second, now); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM background_jobs WHERE id=$1 FOR UPDATE`, first); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := repo.Claim(ctx, now)
	if err != nil || !ok || claimed.ID != second {
		t.Fatalf("claim=%s want=%s ok=%v err=%v", claimed.ID, second, ok, err)
	}
}

func TestEnsureThumbnailJobAtomicAndDeduplicated(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewEmptyDatabase(t)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := jobs.NewRepository(db, id.UUIDv7{})
	source := uuid.New()
	now := time.Now()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.EnsureThumbnailJobTx(ctx, tx, source, "image/png", now)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM background_jobs WHERE subject_id=$1`, source).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, e := db.Begin(ctx)
			if e == nil {
				_, e = repo.EnsureThumbnailJobTx(ctx, tx, source, "image/png", now)
			}
			if e == nil {
				e = tx.Commit(ctx)
			} else if tx != nil {
				_ = tx.Rollback(ctx)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM background_jobs WHERE subject_id=$1 AND job_type='GENERATE_THUMBNAIL'`, source).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedup count=%d err=%v", count, err)
	}
}
