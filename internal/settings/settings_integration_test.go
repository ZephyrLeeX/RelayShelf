//go:build integration

package settings

import (
	"context"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
)

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

func TestTypedSingletonUpdateIsAtomicAndAudited(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	clock := testClock{time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)}
	recorder := audit.NewRecorder(id.UUIDv7{}, clock)
	service := NewService(db, recorder, clock)
	adminID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status)VALUES($1,'admin','Admin','hash',true,'ACTIVE')`, adminID); err != nil {
		t.Fatal(err)
	}
	max := int64(1000)
	updated, err := service.Update(ctx, audit.Actor{UserID: adminID}, Settings{TemporaryTTLHours: 48, TrashTTLHours: 96, MaxFileSizeBytes: 500, MaxStorageBytes: &max, AuditRetentionDays: 30, UploadRetentionHours: 12})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TemporaryTTLHours != 48 || updated.UpdatedByUserID == nil || *updated.UpdatedByUserID != adminID {
		t.Fatalf("updated=%+v", updated)
	}
	var rows, events int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM system_settings`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("singleton rows=%d err=%v", rows, err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE event_type='RUNTIME_SETTINGS_UPDATED' AND metadata = '{"changedFields":["temporaryTtlHours","trashTtlHours","maxFileSizeBytes","maxStorageBytes","auditRetentionDays","uploadRetentionHours"]}'::jsonb`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	invalid := updated
	invalid.TemporaryTTLHours = 0
	if _, err = service.Update(ctx, audit.Actor{UserID: adminID}, invalid); err != ErrValidation {
		t.Fatalf("invalid err=%v", err)
	}
	current, err := service.Get(ctx)
	if err != nil || current.TemporaryTTLHours != 48 {
		t.Fatalf("atomic current=%+v err=%v", current, err)
	}
}
