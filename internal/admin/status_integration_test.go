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
