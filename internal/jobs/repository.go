package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
	ids  id.Generator
}

func NewRepository(pool *pgxpool.Pool, ids id.Generator) *Repository {
	return &Repository{pool: pool, ids: ids}
}

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Type, &j.SubjectType, &j.SubjectID, &j.Status, &j.Attempts, &j.NextRunAt, &j.StartedAt, &j.LastErrorCode, &j.LastErrorSummary, &j.CreatedAt, &j.UpdatedAt)
	return j, err
}

func (r *Repository) Claim(ctx context.Context, now time.Time) (Job, bool, error) {
	job, err := scanJob(r.pool.QueryRow(ctx, `WITH candidate AS (
SELECT id FROM background_jobs WHERE status='PENDING' AND next_run_at<=$1
ORDER BY next_run_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1
) UPDATE background_jobs job SET status='RUNNING',attempts=attempts+1,started_at=$1,updated_at=$1
FROM candidate WHERE job.id=candidate.id
RETURNING job.id,job.job_type,job.subject_type,job.subject_id,job.status,job.attempts,job.next_run_at,job.started_at,job.last_error_code,job.last_error_summary,job.created_at,job.updated_at`, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	return job, err == nil, err
}

func (r *Repository) Complete(ctx context.Context, id uuid.UUID, now time.Time) error {
	ct, err := r.pool.Exec(ctx, `UPDATE background_jobs SET status='COMPLETED',started_at=NULL,last_error_code=NULL,last_error_summary=NULL,updated_at=$2 WHERE id=$1 AND status='RUNNING'`, id, now)
	if err != nil || ct.RowsAffected() == 1 {
		return err
	}
	return r.reconcileComplete(ctx, id)
}

func (r *Repository) Fail(ctx context.Context, job Job, code, summary string, permanent bool, now time.Time, maxAttempts int) error {
	if permanent || job.Attempts >= maxAttempts {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		ct, execErr := tx.Exec(ctx, `UPDATE background_jobs SET status='FAILED',started_at=NULL,last_error_code=$2,last_error_summary=$3,updated_at=$4 WHERE id=$1 AND status='RUNNING'`, job.ID, code, summary, now)
		if execErr != nil {
			return execErr
		}
		if ct.RowsAffected() != 1 {
			return r.reconcileFail(ctx, tx, job, code, summary, StatusFailed, now)
		}
		if job.Type == TypeGenerateThumbnail && job.SubjectID != nil {
			if _, err = tx.Exec(ctx, `UPDATE file_derivatives SET status='FAILED',updated_at=$2 WHERE source_file_id=$1 AND kind='THUMBNAIL_SMALL' AND status='PENDING'`, *job.SubjectID, now); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	nextRunAt := now.Add(Backoff(job.Attempts))
	ct, err := r.pool.Exec(ctx, `UPDATE background_jobs SET status='PENDING',started_at=NULL,next_run_at=$2,last_error_code=$3,last_error_summary=$4,updated_at=$5 WHERE id=$1 AND status='RUNNING'`, job.ID, nextRunAt, code, summary, now)
	if err != nil || ct.RowsAffected() == 1 {
		return err
	}
	return r.reconcileFail(ctx, r.pool, job, code, summary, StatusPending, now)
}

type jobStateReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *Repository) reconcileComplete(ctx context.Context, jobID uuid.UUID) error {
	var status string
	if err := r.pool.QueryRow(ctx, `SELECT status FROM background_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return claimLost(jobID, StatusCompleted, "MISSING")
		}
		return err
	}
	if status == StatusCompleted {
		return nil
	}
	if status == StatusRunning {
		return ErrStateTransition
	}
	return claimLost(jobID, StatusCompleted, status)
}

func (r *Repository) reconcileFail(ctx context.Context, reader jobStateReader, job Job, code, summary, desiredState string, now time.Time) error {
	var status string
	var intended bool
	nextRunAt := now.Add(Backoff(job.Attempts))
	err := reader.QueryRow(ctx, `SELECT status,
attempts=$2 AND last_error_code IS NOT DISTINCT FROM $3 AND last_error_summary IS NOT DISTINCT FROM $4
AND ($5 <> 'PENDING' OR next_run_at=$6)
FROM background_jobs WHERE id=$1`, job.ID, job.Attempts, code, summary, desiredState, nextRunAt).Scan(&status, &intended)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return claimLost(job.ID, desiredState, "MISSING")
		}
		return err
	}
	if status == desiredState && intended {
		return nil
	}
	if status == StatusRunning {
		return ErrStateTransition
	}
	return claimLost(job.ID, desiredState, status)
}

func claimLost(jobID uuid.UUID, desiredState, currentState string) error {
	return &JobClaimLostError{JobID: jobID, DesiredState: desiredState, CurrentState: currentState}
}

func (r *Repository) RecoverStuck(ctx context.Context, now time.Time, timeout time.Duration, maxAttempts, batch int) (int64, error) {
	if batch <= 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `WITH stuck AS (
SELECT id FROM background_jobs WHERE status='RUNNING' AND started_at<$1
ORDER BY started_at,id FOR UPDATE SKIP LOCKED LIMIT $2
) UPDATE background_jobs j SET status=CASE WHEN attempts >= $3 THEN 'FAILED' ELSE 'PENDING' END,
started_at=NULL,next_run_at=CASE WHEN attempts >= $3 THEN next_run_at ELSE $4 END,
last_error_code=CASE WHEN attempts >= $3 THEN 'JOB_STUCK_MAX_ATTEMPTS' ELSE 'JOB_STUCK_RECOVERED' END,
last_error_summary=CASE WHEN attempts >= $3 THEN 'stuck job reached maximum attempts' ELSE 'stuck job recovered' END,updated_at=$4
FROM stuck WHERE j.id=stuck.id RETURNING j.job_type,j.subject_id,j.status`, now.Add(-timeout), batch, maxAttempts, now)
	if err != nil {
		return 0, err
	}
	var affected int64
	var failedThumbnailIDs []uuid.UUID
	for rows.Next() {
		var jobType, status string
		var subjectID *uuid.UUID
		if err = rows.Scan(&jobType, &subjectID, &status); err != nil {
			rows.Close()
			return 0, err
		}
		affected++
		if jobType == TypeGenerateThumbnail && subjectID != nil && status == StatusFailed {
			failedThumbnailIDs = append(failedThumbnailIDs, *subjectID)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(failedThumbnailIDs) > 0 {
		if _, err = tx.Exec(ctx, `UPDATE file_derivatives SET status='FAILED',updated_at=$2 WHERE source_file_id=ANY($1::uuid[]) AND kind='THUMBNAIL_SMALL' AND status='PENDING'`, failedThumbnailIDs, now); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return affected, nil
}

func IsThumbnailMIME(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func (r *Repository) EnsureThumbnailJobTx(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, mime string, now time.Time) (bool, error) {
	if !IsThumbnailMIME(mime) {
		return false, nil
	}
	var ready bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_derivatives WHERE source_file_id=$1 AND kind='THUMBNAIL_SMALL' AND status='READY')`, fileID).Scan(&ready); err != nil || ready {
		return false, err
	}
	jobID, err := r.ids.New()
	if err != nil {
		return false, err
	}
	ct, err := tx.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,attempts,next_run_at,created_at,updated_at)
VALUES($1,'GENERATE_THUMBNAIL','FILE_OBJECT',$2,'PENDING',0,$3,$3,$3) ON CONFLICT DO NOTHING`, jobID, fileID, now)
	return ct.RowsAffected() == 1, err
}

func (r *Repository) EnqueueMissingThumbnailJobs(ctx context.Context, now time.Time, batch int) (int, error) {
	if batch <= 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT fo.id,fo.detected_mime FROM file_objects fo
WHERE fo.status='READY' AND fo.detected_mime=ANY($1::text[])
AND EXISTS(SELECT 1 FROM message_attachments ma WHERE ma.file_object_id=fo.id)
AND NOT EXISTS(SELECT 1 FROM file_derivatives fd WHERE fd.source_file_id=fo.id AND fd.kind='THUMBNAIL_SMALL' AND fd.status='READY')
AND NOT EXISTS(SELECT 1 FROM background_jobs bj WHERE bj.job_type='GENERATE_THUMBNAIL' AND bj.subject_type='FILE_OBJECT' AND bj.subject_id=fo.id)
ORDER BY fo.created_at,fo.id FOR UPDATE OF fo SKIP LOCKED LIMIT $2`, []string{"image/jpeg", "image/png", "image/gif", "image/webp"}, batch)
	if err != nil {
		return 0, err
	}
	type source struct {
		id   uuid.UUID
		mime string
	}
	var sources []source
	for rows.Next() {
		var value source
		if err = rows.Scan(&value.id, &value.mime); err != nil {
			rows.Close()
			return 0, err
		}
		sources = append(sources, value)
	}
	rows.Close()
	count := 0
	for _, source := range sources {
		inserted, ensureErr := r.EnsureThumbnailJobTx(ctx, tx, source.id, source.mime, now)
		if ensureErr != nil {
			return 0, ensureErr
		}
		if inserted {
			count++
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}
