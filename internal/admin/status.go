package admin

import (
	"context"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/buildinfo"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthState string

const (
	Healthy     HealthState = "HEALTHY"
	Degraded    HealthState = "DEGRADED"
	Unavailable HealthState = "UNAVAILABLE"
)

type ThresholdState string

const (
	ThresholdUnconfigured  ThresholdState = "UNCONFIGURED"
	ThresholdNormal        ThresholdState = "NORMAL"
	ThresholdWarning       ThresholdState = "WARNING"
	ThresholdStrongWarning ThresholdState = "STRONG_WARNING"
	ThresholdLimitReached  ThresholdState = "LIMIT_REACHED"
)

type StorageStatus struct {
	State                                    HealthState
	LogicalUsageBytes                        int64
	MaxStorageBytes                          *int64
	ThresholdState                           ThresholdState
	NASAvailableBytes, NASTotalBytes         *int64
	StagingUsageBytes                        int64
	StagingAvailableBytes, StagingTotalBytes *int64
	DegradedReasons                          []string
}

type spaceAdapter interface {
	Space(context.Context) (storage.Space, error)
}

type StatusService struct {
	pool                 *pgxpool.Pool
	storage              spaceAdapter
	staging              staging.SpaceProbe
	probeTimeout         time.Duration
	nasGate, stagingGate chan struct{}
}

func NewStatusService(pool *pgxpool.Pool, adapter spaceAdapter, stagingProbe staging.SpaceProbe) *StatusService {
	return &StatusService{pool: pool, storage: adapter, staging: stagingProbe, probeTimeout: 1500 * time.Millisecond, nasGate: make(chan struct{}, 1), stagingGate: make(chan struct{}, 1)}
}

func (s *StatusService) Storage(ctx context.Context) StorageStatus {
	result := StorageStatus{State: Healthy, ThresholdState: ThresholdUnconfigured, DegradedReasons: []string{}}
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.pool.QueryRow(dbCtx, `SELECT COALESCE((SELECT sum(size_bytes) FROM file_objects WHERE status='READY'),0),max_storage_bytes,COALESCE((SELECT sum(expected_size) FROM upload_sessions WHERE status IN ('CREATED','UPLOADING','COMPLETING')),0) FROM system_settings WHERE id=1`).Scan(&result.LogicalUsageBytes, &result.MaxStorageBytes, &result.StagingUsageBytes); err != nil {
		result.State = Unavailable
		result.DegradedReasons = append(result.DegradedReasons, "DATABASE_UNAVAILABLE")
	}
	result.ThresholdState = threshold(result.LogicalUsageBytes, result.MaxStorageBytes)
	if result.ThresholdState == ThresholdWarning || result.ThresholdState == ThresholdStrongWarning {
		result.DegradedReasons = append(result.DegradedReasons, "LOGICAL_THRESHOLD_WARNING")
	}
	if result.ThresholdState == ThresholdLimitReached {
		result.DegradedReasons = append(result.DegradedReasons, "LOGICAL_THRESHOLD_EXCEEDED")
	}
	type nasResult struct {
		value storage.Space
		err   error
	}
	type stagingResult struct {
		value staging.Space
		err   error
	}
	nasDone, stagingDone := make(chan nasResult, 1), make(chan stagingResult, 1)
	go func() {
		value, err := boundedProbe(ctx, s.probeTimeout, s.nasGate, func() (storage.Space, error) { return s.storage.Space(context.Background()) })
		nasDone <- nasResult{value, err}
	}()
	go func() {
		value, err := boundedProbe(ctx, s.probeTimeout, s.stagingGate, func() (staging.Space, error) { return s.staging.Probe() })
		stagingDone <- stagingResult{value, err}
	}()
	nasOutcome, stagingOutcome := <-nasDone, <-stagingDone
	nas, nasErr := nasOutcome.value, nasOutcome.err
	if nasErr == nil {
		available, total := int64(nas.AvailableBytes), int64(nas.TotalBytes)
		result.NASAvailableBytes, result.NASTotalBytes = &available, &total
	} else if errors.Is(nasErr, context.DeadlineExceeded) {
		result.DegradedReasons = append(result.DegradedReasons, "NAS_TIMEOUT")
	} else {
		result.DegradedReasons = append(result.DegradedReasons, "NAS_UNAVAILABLE")
	}
	stage, stageErr := stagingOutcome.value, stagingOutcome.err
	if stageErr == nil {
		available, total := stage.AvailableBytes, stage.TotalBytes
		result.StagingAvailableBytes, result.StagingTotalBytes = &available, &total
	} else {
		result.DegradedReasons = append(result.DegradedReasons, "STAGING_UNAVAILABLE")
	}
	if result.State != Unavailable && len(result.DegradedReasons) > 0 {
		result.State = Degraded
	}
	return result
}

func threshold(used int64, maximum *int64) ThresholdState {
	if maximum == nil {
		return ThresholdUnconfigured
	}
	if used >= *maximum {
		return ThresholdLimitReached
	}
	if used >= *maximum-*maximum/10 {
		return ThresholdStrongWarning
	}
	if used >= *maximum-*maximum/5 {
		return ThresholdWarning
	}
	return ThresholdNormal
}

func boundedProbe[T any](ctx context.Context, timeout time.Duration, gate chan struct{}, probe func() (T, error)) (T, error) {
	var zero T
	select {
	case gate <- struct{}{}:
	default:
		return zero, context.DeadlineExceeded
	}
	type outcome struct {
		value T
		err   error
	}
	done := make(chan outcome, 1)
	go func() { value, err := probe(); done <- outcome{value, err}; <-gate }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-timer.C:
		return zero, context.DeadlineExceeded
	case result := <-done:
		return result.value, result.err
	}
}

type FailedJob struct {
	ID                           uuid.UUID
	Type, SubjectType, ErrorCode string
	Attempts                     int
	UpdatedAt                    time.Time
}
type MigrationStatus struct {
	Current, Latest int64
	Compatible      bool
}
type AdminStatus struct {
	State, DatabaseState HealthState
	Build                buildinfo.Info
	Migration            MigrationStatus
	FailedJobs           []FailedJob
	Storage              StorageStatus
}

func (s *StatusService) Status(ctx context.Context) AdminStatus {
	result := AdminStatus{State: Healthy, DatabaseState: Healthy, Build: buildinfo.Current(), FailedJobs: []FailedJob{}}
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	current, currentErr := database.CurrentVersion(dbCtx, s.pool)
	latest, latestErr := database.LatestVersion()
	compatible := currentErr == nil && latestErr == nil && current == latest
	result.Migration = MigrationStatus{Current: current, Latest: latest, Compatible: compatible}
	if err := s.pool.Ping(dbCtx); err != nil || currentErr != nil {
		result.DatabaseState, result.State = Unavailable, Unavailable
	} else if !compatible {
		result.DatabaseState, result.State = Degraded, Degraded
	}
	if result.DatabaseState != Unavailable {
		rows, err := s.pool.Query(dbCtx, `SELECT id,job_type,subject_type,attempts,COALESCE(last_error_code,'JOB_FAILED'),updated_at FROM background_jobs WHERE status='FAILED' ORDER BY updated_at DESC,id DESC LIMIT 50`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var job FailedJob
				if rows.Scan(&job.ID, &job.Type, &job.SubjectType, &job.Attempts, &job.ErrorCode, &job.UpdatedAt) == nil {
					result.FailedJobs = append(result.FailedJobs, job)
				}
			}
		} else if result.State == Healthy {
			result.State = Degraded
		}
		if len(result.FailedJobs) > 0 && result.State == Healthy {
			result.State = Degraded
		}
	}
	result.Storage = s.Storage(ctx)
	if result.State == Healthy && result.Storage.State != Healthy {
		result.State = Degraded
	}
	return result
}
