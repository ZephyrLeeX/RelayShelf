//go:build integration

package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
)

func TestPostgreSQLLoginSessionOwnershipAndRevocation(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	password := "integration-password"
	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	one, two, admin := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, row := range []struct {
		id      uuid.UUID
		name    string
		isAdmin bool
	}{{one, "alice", false}, {two, "bob", false}, {admin, "admin", true}} {
		if _, err = db.Exec(ctx, "INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,$3,$4,'ACTIVE')", row.id, row.name, encoded, row.isAdmin); err != nil {
			t.Fatal(err)
		}
	}
	repo := auth.NewPostgreSQLRepository(db)
	now := clock.Real{}
	service := auth.NewService(repo, hasher, id.UUIDv7{}, now, auth.NewRateLimiter(now, 100))
	ip := netip.MustParseAddr("192.0.2.10")
	login := func(name string, device *uuid.UUID) auth.LoginResult {
		result, err := service.Login(ctx, auth.LoginInput{Username: name, Password: password, DeviceID: device, ClientIP: ip, UserAgent: "integration"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := login("alice", nil)
	raw, err := base64.RawURLEncoding.DecodeString(first.RawToken)
	if err != nil || len(raw) != 32 {
		t.Fatalf("raw=%d err=%v", len(raw), err)
	}
	expected := sha256.Sum256(raw)
	var stored []byte
	if err = db.QueryRow(ctx, "SELECT token_hash FROM sessions WHERE id=$1", first.Session.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(expected[:]) {
		t.Fatal("database did not store SHA-256(raw)")
	}
	var rawMatches int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE token_hash=$1", raw).Scan(&rawMatches); err != nil || rawMatches != 0 {
		t.Fatalf("raw token found in database: %d %v", rawMatches, err)
	}
	if _, err = service.Authenticate(ctx, first.RawToken, false, ip); err != nil {
		t.Fatal(err)
	}
	var lastSeen, expiresAt time.Time
	if err = db.QueryRow(ctx, `SELECT last_seen_at,expires_at FROM sessions WHERE id=$1`, first.Session.ID).Scan(&lastSeen, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ValidateSession(ctx, first.Session.ID); err != nil {
		t.Fatal(err)
	}
	var validatedLastSeen, validatedExpiresAt time.Time
	if err = db.QueryRow(ctx, `SELECT last_seen_at,expires_at FROM sessions WHERE id=$1`, first.Session.ID).Scan(&validatedLastSeen, &validatedExpiresAt); err != nil {
		t.Fatal(err)
	}
	if !validatedLastSeen.Equal(lastSeen) || !validatedExpiresAt.Equal(expiresAt) {
		t.Fatalf("read-only validation touched session: last_seen %s -> %s expires %s -> %s", lastSeen, validatedLastSeen, expiresAt, validatedExpiresAt)
	}
	oldLastSeen, probeExpiry := time.Now().Add(-2*auth.TouchInterval), time.Now().Add(time.Hour)
	if _, err = db.Exec(ctx, `UPDATE sessions SET last_seen_at=$2,expires_at=$3 WHERE id=$1`, first.Session.ID, oldLastSeen, probeExpiry); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT last_seen_at,expires_at FROM sessions WHERE id=$1`, first.Session.ID).Scan(&oldLastSeen, &probeExpiry); err != nil {
		t.Fatal(err)
	}
	origin, _ := url.Parse("http://example.test")
	cookies := auth.NewCookiePolicy(origin)
	csrf := auth.NewCSRF([]byte("01234567890123456789012345678901"))
	router := auth.Router(auth.NewHandler(service, csrf, cookies), auth.NewMiddleware(service, cookies, csrf, origin, httpx.NewResolver(nil)))
	for range 2 {
		request := httptest.NewRequest(http.MethodGet, origin.String()+"/api/v1/events", nil)
		request.RemoteAddr = "192.0.2.10:1234"
		request.AddCookie(&http.Cookie{Name: cookies.Name, Value: first.RawToken})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotImplemented {
			t.Fatalf("events auth probe status=%d", response.Code)
		}
	}
	var probedLastSeen, probedExpiresAt time.Time
	if err = db.QueryRow(ctx, `SELECT last_seen_at,expires_at FROM sessions WHERE id=$1`, first.Session.ID).Scan(&probedLastSeen, &probedExpiresAt); err != nil {
		t.Fatal(err)
	}
	if !probedLastSeen.Equal(oldLastSeen) || !probedExpiresAt.Equal(probeExpiry) {
		t.Fatalf("events auth probe touched session: last_seen %s -> %s expires %s -> %s", oldLastSeen, probedLastSeen, probeExpiry, probedExpiresAt)
	}
	cross := login("bob", &first.Device.ID)
	if cross.Device.ID == first.Device.ID || cross.Device.UserID != two {
		t.Fatal("cross-user device was reused")
	}
	second := login("alice", &first.Device.ID)
	if second.Device.ID != first.Device.ID {
		t.Fatal("owned device was not reused")
	}
	if err = service.ChangePassword(ctx, second.Authentication, password, "changed-password", auth.LoginInput{ClientIP: ip}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(ctx, first.RawToken, false, ip); err == nil {
		t.Fatal("other session survived password change")
	}
	if _, err = service.Authenticate(ctx, second.RawToken, false, ip); err != nil {
		t.Fatalf("current session revoked: %v", err)
	}
	if _, err = db.Exec(ctx, "UPDATE users SET status='DISABLED' WHERE id=$1", one); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(ctx, second.RawToken, false, ip); err == nil {
		t.Fatal("disabled user session remained valid")
	}
}

func TestAdminResetRevokesAllTargetSessions(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	encoded, _ := hasher.Hash("initial-password")
	target, adminID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, row := range []struct {
		id    uuid.UUID
		name  string
		admin bool
	}{{target, "target", false}, {adminID, "admin", true}} {
		if _, err := db.Exec(ctx, "INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,$3,$4,'ACTIVE')", row.id, row.name, encoded, row.admin); err != nil {
			t.Fatal(err)
		}
	}
	now := clock.Real{}
	service := auth.NewService(auth.NewPostgreSQLRepository(db), hasher, id.UUIDv7{}, now, auth.NewRateLimiter(now, 100))
	ip := netip.MustParseAddr("192.0.2.1")
	login := func(name string) auth.LoginResult {
		r, e := service.Login(ctx, auth.LoginInput{Username: name, Password: "initial-password", ClientIP: ip})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	one := login("target")
	two := login("target")
	administrator := login("admin")
	if err := service.ResetPasswordByAdmin(ctx, administrator.Authentication, target, "reset-password", auth.LoginInput{ClientIP: ip}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{one.RawToken, two.RawToken} {
		if _, err := service.Authenticate(ctx, token, false, ip); err == nil {
			t.Fatal("target session survived admin reset")
		}
	}
	var changed string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", target).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	ok, _, err := hasher.Verify(changed, "reset-password")
	if err != nil || !ok {
		t.Fatal("reset hash not persisted")
	}
	var auditCount int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE event_type='PASSWORD_RESET_BY_ADMIN'").Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit=%d err=%v", auditCount, err)
	}
}
