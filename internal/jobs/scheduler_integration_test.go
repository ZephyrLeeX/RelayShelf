//go:build integration

package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/realtime"
	"github.com/google/uuid"
)

type schedulerClock struct{ now time.Time }

func (c schedulerClock) Now() time.Time { return c.now }

type failingUploads struct{ called int }

func (u *failingUploads) ExpireDueUploads(context.Context, int32) error {
	u.called++
	return errors.New("injected upload failure")
}

type recordingFiles struct{ gc, reconcile int }

func (f *recordingFiles) GC(context.Context, int, time.Time) error { f.gc++; return nil }
func (f *recordingFiles) Reconcile(context.Context, int) error     { f.reconcile++; return nil }

type recordingPublisher struct {
	mu     sync.Mutex
	events map[uuid.UUID][]realtime.Event
}

func (p *recordingPublisher) Publish(user uuid.UUID, event realtime.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.events == nil {
		p.events = map[uuid.UUID][]realtime.Event{}
	}
	p.events[user] = append(p.events[user], event)
}

func TestSchedulerMaintenanceOrderingIsolationAndLock(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	now := time.Date(2026, 8, 26, 2, 0, 0, 0, time.UTC)
	clock := schedulerClock{now}
	repo := NewRepository(db, id.UUIDv7{})
	wake := NewWake()
	uploads := &failingUploads{}
	fileTasks := &recordingFiles{}
	publisher := &recordingPublisher{}
	scheduler := NewScheduler(db, repo, uploads, fileTasks, publisher, id.UUIDv7{}, clock, wake)
	user := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status) VALUES($1,'scheduler-user','scheduler','x','ACTIVE')`, user); err != nil {
		t.Fatal(err)
	}
	expired, purged, active := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at) VALUES($1,$4,'expired','TEXT',false,'TEMPORARY',$5,$6,$6),($2,$4,'purged','TEXT',false,'TEMPORARY',$7,$6,$6),($3,$4,'active','TEXT',false,'TEMPORARY',$8,$6,$6)`, expired, purged, active, user, now.Add(-time.Hour), now.Add(-48*time.Hour), now.Add(-2*time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE messages SET trashed_at=$2,purge_at=$3 WHERE id=$1`, purged, now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	fileID, attachmentID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	hash := make([]byte, 32)
	hash[0] = 1
	if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at) VALUES($1,$2,1,'image/png','filesystem',$3,'READY',$4,$4,$4)`, fileID, hash, "objects/"+fileID.String(), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order) VALUES($1,$2,$3,'old.png',0)`, attachmentID, active, fileID); err != nil {
		t.Fatal(err)
	}
	auditID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO audit_logs(id,event_type,created_at) VALUES($1,'OLD',$2)`, auditID, now.Add(-100*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	stuckID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,status,attempts,next_run_at,started_at,created_at,updated_at) VALUES($1,'OTHER','TEST','RUNNING',1,$2,$3,$3,$3)`, stuckID, now, now.Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	entered, err := scheduler.RunOnce(ctx)
	if !entered {
		t.Fatal("scheduler did not acquire lock")
	}
	if err == nil {
		t.Fatal("injected task error was not returned")
	}
	if uploads.called != 1 || fileTasks.gc != 1 || fileTasks.reconcile != 1 {
		t.Fatalf("error isolation uploads=%d gc=%d reconcile=%d", uploads.called, fileTasks.gc, fileTasks.reconcile)
	}
	var trashedAt *time.Time
	if err = db.QueryRow(ctx, `SELECT trashed_at FROM messages WHERE id=$1`, expired).Scan(&trashedAt); err != nil || trashedAt == nil {
		t.Fatalf("temporary not trashed err=%v", err)
	}
	var exists bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE id=$1)`, purged).Scan(&exists); err != nil || exists {
		t.Fatalf("purged message exists=%v err=%v", exists, err)
	}
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_logs WHERE id=$1)`, auditID).Scan(&exists); err != nil || exists {
		t.Fatalf("old audit exists=%v err=%v", exists, err)
	}
	var jobStatus string
	if err = db.QueryRow(ctx, `SELECT status FROM background_jobs WHERE id=$1`, stuckID).Scan(&jobStatus); err != nil || jobStatus != "PENDING" {
		t.Fatalf("stuck status=%s err=%v", jobStatus, err)
	}
	var thumbnailJobs int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM background_jobs WHERE job_type='GENERATE_THUMBNAIL' AND subject_id=$1`, fileID).Scan(&thumbnailJobs); err != nil || thumbnailJobs != 1 {
		t.Fatalf("backfill jobs=%d err=%v", thumbnailJobs, err)
	}
	publisher.mu.Lock()
	events := append([]realtime.Event(nil), publisher.events[user]...)
	publisher.mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events=%v", events)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, SchedulerAdvisoryLockID); err != nil {
		t.Fatal(err)
	}
	entered, err = scheduler.RunOnce(ctx)
	if err != nil || entered {
		t.Fatalf("second scheduler entered=%v err=%v", entered, err)
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, SchedulerAdvisoryLockID); err != nil {
		t.Fatal(err)
	}
}
