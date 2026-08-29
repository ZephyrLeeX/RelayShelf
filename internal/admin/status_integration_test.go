//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
)

type fakeStorageSpace struct {
	space storage.Space
	err   error
}

func (f fakeStorageSpace) Space(context.Context) (storage.Space, error) { return f.space, f.err }

type fakeStagingSpace struct {
	space staging.Space
	err   error
}

func (f fakeStagingSpace) Probe() (staging.Space, error) { return f.space, f.err }

func TestStorageStagingUsageMatchesUploadReservationSemantics(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	now := time.Now().UTC()
	userID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status)VALUES($1,'uploader','Uploader','hash',false,'ACTIVE')`, userID); err != nil {
		t.Fatal(err)
	}
	// One session per lifecycle status with a distinct reservation size. The
	// first four statuses still reserve staging capacity per the upload
	// reservation authority; COMPLETED and EXPIRED do not.
	sessions := []struct {
		status string
		size   int64
	}{
		{"CREATED", 100},
		{"UPLOADING", 200},
		{"COMPLETING", 300},
		{"FAILED", 400},
		{"COMPLETED", 5000},
		{"EXPIRED", 6000},
	}
	var wantActive int64
	for _, session := range sessions {
		if session.status != "COMPLETED" && session.status != "EXPIRED" {
			wantActive += session.size
		}
		id := uuid.Must(uuid.NewV7())
		var completedAt any
		if session.status == "COMPLETED" {
			completedAt = now.Add(-time.Hour)
		}
		if _, err := db.Exec(ctx, `INSERT INTO upload_sessions(id,user_id,original_filename,expected_size,chunk_size,status,expires_at,created_at,updated_at,completed_at)VALUES($1,$2,'report.bin',$3,8388608,$4,$5,$6,$6,$7)`, id, userID, session.size, session.status, now.Add(24*time.Hour), now, completedAt); err != nil {
			t.Fatalf("seed %s session: %v", session.status, err)
		}
	}
	service := NewStatusService(db, fakeStorageSpace{space: storage.Space{AvailableBytes: 1 << 30, TotalBytes: 2 << 30}}, fakeStagingSpace{space: staging.Space{AvailableBytes: 1 << 30, TotalBytes: 2 << 30}})
	value := service.Storage(ctx)
	if value.State != Healthy {
		t.Fatalf("state=%+v", value)
	}
	if value.StagingUsageBytes != wantActive {
		t.Fatalf("staging usage=%d want=%d (FAILED must count, COMPLETED/EXPIRED must not)", value.StagingUsageBytes, wantActive)
	}
}

func TestStorageHealthyDegradedAndAdminStatusRedaction(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	now := time.Now().UTC()
	fileID := uuid.Must(uuid.NewV7())
	sum := make([]byte, 32)
	sum[0] = 1
	if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at)VALUES($1,$2,80,'application/octet-stream','filesystem',$3,'READY',$4,$4,$4)`, fileID, sum, "objects/"+fileID.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE system_settings SET max_storage_bytes=100 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	service := NewStatusService(db, fakeStorageSpace{space: storage.Space{AvailableBytes: 500, TotalBytes: 1000}}, fakeStagingSpace{space: staging.Space{AvailableBytes: 700, TotalBytes: 1000}})
	value := service.Storage(ctx)
	if value.State != Degraded || value.ThresholdState != ThresholdWarning || value.NASAvailableBytes == nil || *value.NASAvailableBytes != 500 {
		t.Fatalf("status=%+v", value)
	}
	service = NewStatusService(db, fakeStorageSpace{err: errors.New("nas unavailable")}, fakeStagingSpace{err: errors.New("staging unavailable")})
	value = service.Storage(ctx)
	if value.State != Degraded || len(value.DegradedReasons) != 3 {
		t.Fatalf("degraded=%+v", value)
	}
	jobID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO background_jobs(id,job_type,subject_type,subject_id,status,attempts,next_run_at,last_error_code,last_error_summary,created_at,updated_at)VALUES($1,'GENERATE_THUMBNAIL','FILE_OBJECT',$3,'FAILED',8,$2,'IMAGE_DECODE_FAILED','password=top-secret token=abc',$2,$2)`, jobID, now, fileID); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(statusDTO(service.Status(ctx)))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(encoded))
	if strings.Contains(text, "top-secret") || strings.Contains(text, "password=") || strings.Contains(text, "token=") {
		t.Fatalf("status leaked job summary: %s", text)
	}
}

// TestSecurityProjectionTracksActiveAdminTOTP pins the public-exposure gate
// semantics in the admin status surface: only ACTIVE admins count, pending
// enrollments do not satisfy the gate, and confirming the last admin's
// enrollment flips the projection to satisfied.
func TestSecurityProjectionTracksActiveAdminTOTP(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	service := NewStatusService(db, fakeStorageSpace{space: storage.Space{AvailableBytes: 1, TotalBytes: 1}}, fakeStagingSpace{space: staging.Space{AvailableBytes: 1, TotalBytes: 1}})
	seedAdmin := func(name string) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status)VALUES($1,$2,$2,'hash',true,'ACTIVE')`, id, name); err != nil {
			t.Fatal(err)
		}
		return id
	}
	disabledAdmin := seedAdmin("disabled-admin")
	if _, err := db.Exec(ctx, `UPDATE users SET status='DISABLED' WHERE id=$1`, disabledAdmin); err != nil {
		t.Fatal(err)
	}
	loneAdmin := seedAdmin("lone-admin")

	status := service.Status(ctx)
	if status.Security.ActiveAdmins != 1 || status.Security.ActiveAdminsWithoutTOTP != 1 || status.Security.AdminTotpSatisfied {
		t.Fatalf("before enrollment: %+v", status.Security)
	}

	// A pending enrollment must not satisfy the gate.
	if _, err := db.Exec(ctx, `INSERT INTO user_totp(id,user_id,secret_ciphertext,secret_nonce,secret_encryption_version,digits,period_seconds,algorithm,created_at,updated_at)VALUES($1,$2,decode('00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff','hex'),decode('0011223344556677889900aa','hex'),1,6,30,'SHA1',now(),now())`, uuid.Must(uuid.NewV7()), loneAdmin); err != nil {
		t.Fatal(err)
	}
	status = service.Status(ctx)
	if status.Security.AdminTotpSatisfied || status.Security.ActiveAdminsWithoutTOTP != 1 {
		t.Fatalf("pending enrollment satisfied gate: %+v", status.Security)
	}

	// Confirming it does.
	if _, err := db.Exec(ctx, `UPDATE user_totp SET enabled_at=now() WHERE user_id=$1`, loneAdmin); err != nil {
		t.Fatal(err)
	}
	status = service.Status(ctx)
	if !status.Security.AdminTotpSatisfied || status.Security.ActiveAdminsWithoutTOTP != 0 {
		t.Fatalf("confirmed enrollment did not satisfy gate: %+v", status.Security)
	}
}
