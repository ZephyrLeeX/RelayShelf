//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/settings"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
	"github.com/google/uuid"
)

// The administrator is an operational role, never a content super-user. Every
// response the admin surface can emit is checked here against sentinels that
// exist in the database: another user's private message body, their password
// hash, and secrets typed into admin mutations.
func TestAdminSurfaceNeverExposesPrivateContentOrSecrets(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	now := clock.Real{}
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	recorder := audit.NewRecorder(id.UUIDv7{}, now)
	userAdmin := users.NewAdminService(db, hasher, id.UUIDv7{}, now, recorder)
	authService := auth.NewService(auth.NewPostgreSQLRepository(db, recorder), hasher, id.UUIDv7{}, now, auth.NewRateLimiter(now, 100))
	statusService := NewStatusService(db, fakeStorageSpace{}, fakeStagingSpace{})
	handler := NewHandler(userAdmin, authService, statusService)
	settingsHandler := settings.NewHandler(settings.NewService(db, recorder, now))

	adminID, bobID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	const bodySentinel = "bob-private-body-sentinel-9f2c"
	bobHash, err := hasher.Hash("bob-original-password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bobHash, bodySentinel) {
		t.Fatal("sentinel collision with password hash")
	}
	for _, row := range []struct {
		id    uuid.UUID
		name  string
		admin bool
		hash  string
	}{{adminID, "admin", true, "admin-hash"}, {bobID, "bob", false, bobHash}} {
		if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status)VALUES($1,$2,$2,$3,$4,'ACTIVE')`, row.id, row.name, row.hash, row.admin); err != nil {
			t.Fatal(err)
		}
	}
	messageID := uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at)VALUES($1,$2,$3,'TEXT',false,'PERMANENT',NULL,$4,$4)`, messageID, bobID, bodySentinel, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Real device and session rows so admin mutations audit a valid actor.
	deviceID, sessionID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	tokenHash := make([]byte, 32)
	tokenHash[0] = 7
	if _, err = db.Exec(ctx, `INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at)VALUES($1,$2,'admin-device','integration',now(),now())`, deviceID, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at,created_at)VALUES($1,$2,$3,$4,now()+interval '1 day',now()+interval '2 day',now(),now())`, sessionID, adminID, deviceID, tokenHash); err != nil {
		t.Fatal(err)
	}

	bodies := make([]string, 0, 8)
	call := func(name string, fn func(w *httptest.ResponseRecorder)) {
		w := httptest.NewRecorder()
		fn(w)
		bodies = append(bodies, w.Body.String())
		if w.Code >= http.StatusInternalServerError {
			t.Fatalf("%s failed status=%d body=%s", name, w.Code, w.Body.String())
		}
	}
	seed := func(method, path string, body []byte) *http.Request {
		r := httptest.NewRequest(method, "https://example.test"+path, bytes.NewReader(body))
		authentication := auth.Authentication{User: auth.User{ID: adminID, IsAdmin: true, Status: "ACTIVE"}}
		authentication.Device.ID = deviceID
		authentication.Session.ID = sessionID
		return r.WithContext(auth.ContextWithAuthentication(r.Context(), authentication))
	}
	call("status", func(w *httptest.ResponseRecorder) {
		handler.GetAdminStatus(w, seed(http.MethodGet, "/api/v1/admin/status", nil))
	})
	call("storage", func(w *httptest.ResponseRecorder) {
		handler.GetStorageStatus(w, seed(http.MethodGet, "/api/v1/admin/storage", nil))
	})
	call("users", func(w *httptest.ResponseRecorder) {
		handler.ListAdminUsers(w, seed(http.MethodGet, "/api/v1/admin/users", nil), httpapi.ListAdminUsersParams{})
	})

	call("create", func(w *httptest.ResponseRecorder) {
		payload, _ := json.Marshal(httpapi.CreateAdminUserRequest{Username: "carol", DisplayName: "Carol", Password: "carol-super-secret-password", IsAdmin: false})
		handler.CreateAdminUser(w, seed(http.MethodPost, "/api/v1/admin/users", payload))
	})
	var carolID uuid.UUID
	if err = db.QueryRow(ctx, `SELECT id FROM users WHERE username='carol'`).Scan(&carolID); err != nil {
		t.Fatal(err)
	}
	call("reset", func(w *httptest.ResponseRecorder) {
		payload, _ := json.Marshal(httpapi.ResetAdminUserPasswordRequest{NewPassword: "carol-rotated-secret-password"})
		r := seed(http.MethodPost, "/api/v1/admin/users/"+carolID.String()+"/password/reset", payload)
		handler.ResetAdminUserPassword(w, r, httpapi.UserId(carolID))
	})
	call("disable", func(w *httptest.ResponseRecorder) {
		handler.DisableAdminUser(w, seed(http.MethodPost, "/api/v1/admin/users/"+carolID.String()+"/disable", nil), httpapi.UserId(carolID))
	})
	call("settings-get", func(w *httptest.ResponseRecorder) {
		settingsHandler.GetRuntimeSettings(w, seed(http.MethodGet, "/api/v1/admin/settings", nil))
	})
	call("settings-put", func(w *httptest.ResponseRecorder) {
		payload, _ := json.Marshal(httpapi.UpdateRuntimeSettingsRequest{TemporaryTtlHours: 96, TrashTtlHours: 336, MaxFileSizeBytes: 1073741824, AuditRetentionDays: 180, UploadRetentionHours: 48})
		settingsHandler.UpdateRuntimeSettings(w, seed(http.MethodPut, "/api/v1/admin/settings", payload))
	})

	var auditMetadata string
	if err = db.QueryRow(ctx, `SELECT COALESCE(string_agg(metadata::text,' '||event_type||' '),'') FROM audit_logs`).Scan(&auditMetadata); err != nil {
		t.Fatal(err)
	}
	var bobBody string
	if err = db.QueryRow(ctx, `SELECT body_plaintext FROM messages WHERE id=$1`, messageID).Scan(&bobBody); err != nil || bobBody != bodySentinel {
		t.Fatalf("negative control body=%q err=%v", bobBody, err)
	}

	for _, leaked := range []string{bodySentinel, bobHash, "carol-super-secret-password", "carol-rotated-secret-password", "storage_key", "password_hash"} {
		for index, body := range bodies {
			if strings.Contains(body, leaked) {
				t.Fatalf("admin response %d leaked %q: %s", index, leaked, body)
			}
		}
		if strings.Contains(auditMetadata, leaked) {
			t.Fatalf("audit metadata leaked %q: %s", leaked, auditMetadata)
		}
	}
}
