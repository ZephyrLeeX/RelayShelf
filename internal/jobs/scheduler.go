package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/realtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultSchedulerInterval       = time.Hour
	DefaultMaintenanceBatch        = 200
	SchedulerAdvisoryLockID  int64 = 823174592117
)

type UploadMaintenance interface {
	ExpireDueUploads(context.Context, int32) error
}
type FileMaintenance interface {
	GC(context.Context, int, time.Time) error
	Reconcile(context.Context, int) error
}

type Scheduler struct {
	pool              *pgxpool.Pool
	repo              *Repository
	uploads           UploadMaintenance
	files             FileMaintenance
	publisher         realtime.Publisher
	ids               id.Generator
	clock             Clock
	wake              *Wake
	interval          time.Duration
	batch, maxBatches int
	report            func(error)
}

func NewScheduler(pool *pgxpool.Pool, repo *Repository, uploads UploadMaintenance, files FileMaintenance, publisher realtime.Publisher, ids id.Generator, clock Clock, wake *Wake) *Scheduler {
	return &Scheduler{pool: pool, repo: repo, uploads: uploads, files: files, publisher: publisher, ids: ids, clock: clock, wake: wake, interval: DefaultSchedulerInterval, batch: DefaultMaintenanceBatch, maxBatches: 5, report: func(err error) { log.Printf("maintenance scheduler: %v", err) }}
}

func (s *Scheduler) SetErrorReporter(report func(error)) {
	if report != nil {
		s.report = report
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
		safeReport(s.report, err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
				safeReport(s.report, err)
			}
		}
	}
}

func (s *Scheduler) RunOnce(ctx context.Context) (bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var acquired bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, SchedulerAdvisoryLockID).Scan(&acquired); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, SchedulerAdvisoryLockID)
	}()
	var errs []error
	run := func(name string, fn func() error) {
		if taskErr := fn(); taskErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, taskErr))
		}
	}
	now := s.clock.Now()
	run("recover stuck jobs", func() error {
		_, e := s.repo.RecoverStuck(ctx, now, DefaultStuckTimeout, DefaultMaxAttempts, s.batch)
		return e
	})
	run("temporary expiry", func() error { return s.expireTemporary(ctx, conn, now) })
	run("trash purge", func() error { return s.purgeTrash(ctx, conn, now) })
	run("audit retention", func() error { return s.cleanupAudit(ctx, conn, now) })
	run("thumbnail backfill", func() error {
		for i := 0; i < s.maxBatches; i++ {
			n, e := s.repo.EnqueueMissingThumbnailJobs(ctx, now, s.batch)
			if e != nil {
				return e
			}
			if n > 0 {
				s.wake.Signal()
			}
			if n < s.batch {
				return nil
			}
		}
		return nil
	})
	if s.uploads != nil {
		run("upload expiry", func() error { return s.uploads.ExpireDueUploads(ctx, int32(s.batch)) })
	}
	if s.files != nil {
		run("file object gc", func() error { return s.files.GC(ctx, s.batch, now) })
		run("file object reconcile", func() error { return s.files.Reconcile(ctx, s.batch) })
	}
	return true, errors.Join(errs...)
}

type changedMessage struct {
	userID, messageID uuid.UUID
	version           int64
}

func (s *Scheduler) expireTemporary(ctx context.Context, conn *pgxpool.Conn, now time.Time) error {
	for cycle := 0; cycle < s.maxBatches; cycle++ {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		var trashHours int
		if err = tx.QueryRow(ctx, `SELECT trash_ttl_hours FROM system_settings WHERE id=1`).Scan(&trashHours); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		rows, err := tx.Query(ctx, `WITH due AS (SELECT id FROM messages WHERE lifecycle='TEMPORARY' AND trashed_at IS NULL AND expires_at<=$1 ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE messages m SET trashed_at=$1,purge_at=$1+($3::int*interval '1 hour'),version=version+1,updated_at=$1 FROM due WHERE m.id=due.id RETURNING m.owner_id,m.id,m.version`, now, s.batch, trashHours)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		var changed []changedMessage
		for rows.Next() {
			var m changedMessage
			if err = rows.Scan(&m.userID, &m.messageID, &m.version); err != nil {
				rows.Close()
				_ = tx.Rollback(ctx)
				return err
			}
			changed = append(changed, m)
		}
		rows.Close()
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		for _, m := range changed {
			s.publish(m.userID, realtime.MessageUpdated, m.messageID, &m.version)
		}
		if len(changed) < s.batch {
			return nil
		}
	}
	return nil
}

func (s *Scheduler) purgeTrash(ctx context.Context, conn *pgxpool.Conn, now time.Time) error {
	for cycle := 0; cycle < s.maxBatches; cycle++ {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `WITH due AS (SELECT id,owner_id FROM messages WHERE purge_at<=$1 ORDER BY purge_at,id FOR UPDATE SKIP LOCKED LIMIT $2),deleted AS (DELETE FROM messages m USING due WHERE m.id=due.id RETURNING m.id,m.owner_id) SELECT owner_id,id FROM deleted`, now, s.batch)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		var changed []changedMessage
		for rows.Next() {
			var m changedMessage
			if err = rows.Scan(&m.userID, &m.messageID); err != nil {
				rows.Close()
				_ = tx.Rollback(ctx)
				return err
			}
			changed = append(changed, m)
		}
		rows.Close()
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		for _, m := range changed {
			s.publish(m.userID, realtime.MessageDeleted, m.messageID, nil)
		}
		if len(changed) < s.batch {
			return nil
		}
	}
	return nil
}

func (s *Scheduler) cleanupAudit(ctx context.Context, conn *pgxpool.Conn, now time.Time) error {
	for cycle := 0; cycle < s.maxBatches; cycle++ {
		deleted, err := audit.Cleanup(ctx, conn, now, s.batch)
		if err != nil {
			return err
		}
		if deleted < int64(s.batch) {
			return nil
		}
	}
	return nil
}

func (s *Scheduler) publish(userID uuid.UUID, eventType string, resourceID uuid.UUID, version *int64) {
	if s.publisher == nil || s.ids == nil {
		return
	}
	eventID, err := s.ids.New()
	if err != nil {
		return
	}
	s.publisher.Publish(userID, realtime.Event{ID: eventID, Type: eventType, ResourceID: resourceID, Version: version, OccurredAt: s.clock.Now()})
}
