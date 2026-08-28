//go:build integration

package auth_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
)

// recordingServer proves whether a request survived the middleware chain
// without coupling this test to any business module.
type recordingServer struct {
	httpapi.Unimplemented
	reached int
}

func (s *recordingServer) GetAdminStatus(w http.ResponseWriter, _ *http.Request) {
	s.reached++
	w.WriteHeader(http.StatusOK)
}
func (s *recordingServer) CreateAdminUser(w http.ResponseWriter, _ *http.Request) {
	s.reached++
	w.WriteHeader(http.StatusCreated)
}

// TestAdminRoutesAreEnforcedServerSide covers the full authorization matrix
// for the admin surface: unauthenticated, normal user, administrator, and an
// administrator disabled after their session was issued. The disabled case
// must fail even though the session row itself is still valid, because
// authentication re-reads live user state on every request.
func TestAdminRoutesAreEnforcedServerSide(t *testing.T) {
	ctx := t.Context()
	db := postgresutil.NewDatabase(t)
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	password := "integration-password"
	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	adminID, userID, secondAdminID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, row := range []struct {
		id      uuid.UUID
		name    string
		isAdmin bool
	}{{adminID, "admin", true}, {userID, "alice", false}, {secondAdminID, "later-admin", true}} {
		if _, err = db.Exec(ctx, "INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,$2,$2,$3,$4,'ACTIVE')", row.id, row.name, encoded, row.isAdmin); err != nil {
			t.Fatal(err)
		}
	}
	repo := auth.NewPostgreSQLRepository(db)
	now := clock.Real{}
	service := auth.NewService(repo, hasher, id.UUIDv7{}, now, auth.NewRateLimiter(now, 100))
	ip := netip.MustParseAddr("192.0.2.10")
	login := func(name string) auth.LoginResult {
		result, err := service.Login(ctx, auth.LoginInput{Username: name, Password: password, ClientIP: ip, UserAgent: "integration"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	origin, _ := url.Parse("https://example.test")
	csrf := auth.NewCSRF([]byte("01234567890123456789012345678901"))
	cookies := auth.NewCookiePolicy(origin)
	server := &recordingServer{}
	router := auth.Router(server, auth.NewMiddleware(service, cookies, csrf, origin, httpx.NewResolver(nil)))

	adminSession := login("admin")
	userSession := login("alice")
	secondAdminSession := login("later-admin")
	request := func(method, path, rawToken, csrfToken string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.test"+path, nil)
		if rawToken != "" {
			r.AddCookie(&http.Cookie{Name: cookies.Name, Value: rawToken})
		}
		if csrfToken != "" {
			r.Header.Set("X-CSRF-Token", csrfToken)
		}
		r.Header.Set("Origin", "https://example.test")
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	if w := request(http.MethodGet, "/api/v1/admin/status", "", ""); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "AUTH_REQUIRED") {
		t.Fatalf("unauthenticated status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(http.MethodGet, "/api/v1/admin/status", userSession.RawToken, ""); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("normal user status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(http.MethodGet, "/api/v1/admin/status", adminSession.RawToken, ""); w.Code != http.StatusOK || server.reached != 1 {
		t.Fatalf("administrator status=%d reached=%d body=%s", w.Code, server.reached, w.Body.String())
	}
	// Unsafe admin methods stay authorization-gated behind CSRF.
	if w := request(http.MethodPost, "/api/v1/admin/users", userSession.RawToken, csrf.Token(userSession.Session.ID)); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("normal user unsafe status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/v1/admin/users", adminSession.RawToken, csrf.Token(adminSession.Session.ID)); w.Code != http.StatusCreated || server.reached != 2 {
		t.Fatalf("administrator unsafe status=%d reached=%d body=%s", w.Code, server.reached, w.Body.String())
	}

	// Disabling the administrator must deny their still-valid session on the
	// next request; enforcement lives in authentication, not the UI.
	if _, err = db.Exec(ctx, "UPDATE users SET status='DISABLED' WHERE id=$1", secondAdminSession.User.ID); err != nil {
		t.Fatal(err)
	}
	if w := request(http.MethodGet, "/api/v1/admin/status", secondAdminSession.RawToken, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled administrator status=%d body=%s", w.Code, w.Body.String())
	}
	if server.reached != 2 {
		t.Fatalf("disabled administrator reached handler reached=%d", server.reached)
	}
	// The untouched administrator keeps access after the other was disabled.
	if w := request(http.MethodGet, "/api/v1/admin/status", adminSession.RawToken, ""); w.Code != http.StatusOK || server.reached != 3 {
		t.Fatalf("administrator after disable status=%d reached=%d", w.Code, server.reached)
	}
}
