package settings

import (
	"context"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrValidation = errors.New("invalid runtime settings")

type Settings struct {
	TemporaryTTLHours, TrashTTLHours         int
	MaxFileSizeBytes                         int64
	MaxStorageBytes                          *int64
	AuditRetentionDays, UploadRetentionHours int
	UpdatedAt                                time.Time
	UpdatedByUserID                          *uuid.UUID
}

func (s Settings) Validate() error {
	if s.TemporaryTTLHours < 1 || s.TemporaryTTLHours > 8760 ||
		s.TrashTTLHours < 1 || s.TrashTTLHours > 8760 ||
		s.MaxFileSizeBytes < 1 || s.MaxFileSizeBytes > 2<<40 ||
		s.AuditRetentionDays < 1 || s.AuditRetentionDays > 3650 ||
		s.UploadRetentionHours < 1 || s.UploadRetentionHours > 168 ||
		(s.MaxStorageBytes != nil && *s.MaxStorageBytes < 1) {
		return ErrValidation
	}
	return nil
}

type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	clock    audit.Clock
}

func NewService(pool *pgxpool.Pool, recorder *audit.Recorder, clock audit.Clock) *Service {
	return &Service{pool: pool, recorder: recorder, clock: clock}
}

func scan(row pgx.Row) (Settings, error) {
	var value Settings
	err := row.Scan(&value.TemporaryTTLHours, &value.TrashTTLHours, &value.MaxFileSizeBytes, &value.MaxStorageBytes, &value.AuditRetentionDays, &value.UploadRetentionHours, &value.UpdatedAt, &value.UpdatedByUserID)
	return value, err
}

const selectSettings = `SELECT temporary_ttl_hours,trash_ttl_hours,max_file_size_bytes,max_storage_bytes,audit_retention_days,upload_retention_hours,updated_at,updated_by_user_id FROM system_settings WHERE id=1`

func (s *Service) Get(ctx context.Context) (Settings, error) {
	return scan(s.pool.QueryRow(ctx, selectSettings))
}

func (s *Service) Update(ctx context.Context, actor audit.Actor, input Settings) (Settings, error) {
	if err := input.Validate(); err != nil {
		return Settings{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := scan(tx.QueryRow(ctx, selectSettings+` FOR UPDATE`))
	if err != nil {
		return Settings{}, err
	}
	changed := changedFields(before, input)
	now := s.clock.Now()
	updated, err := scan(tx.QueryRow(ctx, `UPDATE system_settings SET temporary_ttl_hours=$1,trash_ttl_hours=$2,max_file_size_bytes=$3,max_storage_bytes=$4,audit_retention_days=$5,upload_retention_hours=$6,updated_at=$7,updated_by_user_id=$8 WHERE id=1 RETURNING temporary_ttl_hours,trash_ttl_hours,max_file_size_bytes,max_storage_bytes,audit_retention_days,upload_retention_hours,updated_at,updated_by_user_id`, input.TemporaryTTLHours, input.TrashTTLHours, input.MaxFileSizeBytes, input.MaxStorageBytes, input.AuditRetentionDays, input.UploadRetentionHours, now, actor.UserID))
	if err != nil {
		return Settings{}, err
	}
	if len(changed) > 0 {
		if err = s.recorder.Record(ctx, tx, audit.RuntimeSettingsUpdated(actor, changed)); err != nil {
			return Settings{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Settings{}, err
	}
	return updated, nil
}

func changedFields(a, b Settings) []string {
	fields := make([]string, 0, 6)
	if a.TemporaryTTLHours != b.TemporaryTTLHours {
		fields = append(fields, "temporaryTtlHours")
	}
	if a.TrashTTLHours != b.TrashTTLHours {
		fields = append(fields, "trashTtlHours")
	}
	if a.MaxFileSizeBytes != b.MaxFileSizeBytes {
		fields = append(fields, "maxFileSizeBytes")
	}
	if !equalInt64(a.MaxStorageBytes, b.MaxStorageBytes) {
		fields = append(fields, "maxStorageBytes")
	}
	if a.AuditRetentionDays != b.AuditRetentionDays {
		fields = append(fields, "auditRetentionDays")
	}
	if a.UploadRetentionHours != b.UploadRetentionHours {
		fields = append(fields, "uploadRetentionHours")
	}
	return fields
}

func equalInt64(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
