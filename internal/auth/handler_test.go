package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/google/uuid"
)

func loginRequest(t *testing.T, handler http.Handler, username string) (int, httpapi.Error) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": "wrong-password"})
	r := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/auth/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.test")
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var apiErr httpapi.Error
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	return w.Code, apiErr
}

func TestLoginRequiresSameOrigin(t *testing.T) {
	origin, _ := url.Parse("http://example.test")
	clock := &fakeClock{now: time.Now()}
	service := NewService(&memoryRepo{findUserErr: ErrNotFound}, &countingHasher{}, &sequenceIDs{}, clock, NewRateLimiter(clock, 32), nil)
	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	cookies := NewCookiePolicy(origin)
	router := Router(NewHandler(service, csrf, cookies), NewMiddleware(service, cookies, csrf, origin, httpx.NewResolver(nil)))

	request := func(originHeader, referer string) int {
		body, _ := json.Marshal(map[string]string{"username": "missing", "password": "wrong-password"})
		r := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/auth/login", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", originHeader)
		r.Header.Set("Referer", referer)
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}
	if status := request("http://example.test", ""); status != http.StatusUnauthorized {
		t.Fatalf("valid Origin status=%d", status)
	}
	if status := request("", "http://example.test/login"); status != http.StatusUnauthorized {
		t.Fatalf("valid Referer status=%d", status)
	}
	if status := request("http://evil.test", ""); status != http.StatusForbidden {
		t.Fatalf("evil Origin status=%d", status)
	}
	if status := request("", ""); status != http.StatusForbidden {
		t.Fatalf("missing Origin and Referer status=%d", status)
	}
}

func TestLoginCookieExpiresAtAbsoluteExpiry(t *testing.T) {
	origin, _ := url.Parse("http://example.test")
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	user := User{ID: uuid.New(), Username: "alice", Status: "ACTIVE", PasswordHash: "hash"}
	repo := &memoryRepo{user: user}
	service := NewService(repo, &countingHasher{ok: true}, &sequenceIDs{}, clock, NewRateLimiter(clock, 32), nil)
	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	cookies := NewCookiePolicy(origin)
	router := Router(NewHandler(service, csrf, cookies), NewMiddleware(service, cookies, csrf, origin, httpx.NewResolver(nil)))
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "correct-password"})
	r := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/auth/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.test")
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	response := w.Result()
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	})
	set := response.Cookies()
	if len(set) != 1 {
		t.Fatalf("cookies=%d", len(set))
	}
	if want := now.Add(AbsoluteLifetime); !set[0].Expires.Equal(want) {
		t.Fatalf("cookie expiry=%s want=%s", set[0].Expires, want)
	}
	if set[0].Expires.Equal(now.Add(IdleLifetime)) {
		t.Fatal("cookie expiry still uses idle expiry")
	}
}

func TestLoginHTTPEnumerationAndRateLimitContract(t *testing.T) {
	origin, _ := url.Parse("http://example.test")
	clock := &fakeClock{now: time.Now()}
	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	build := func(repo *memoryRepo, hasher PasswordHasher) http.Handler {
		service := NewService(repo, hasher, &sequenceIDs{}, clock, NewRateLimiter(clock, 32), nil)
		cookies := NewCookiePolicy(origin)
		middleware := NewMiddleware(service, cookies, csrf, origin, httpx.NewResolver(nil))
		return Router(NewHandler(service, csrf, cookies), middleware)
	}
	unknown := build(&memoryRepo{findUserErr: ErrNotFound}, &countingHasher{})
	known := build(&memoryRepo{user: User{Username: "alice", Status: "ACTIVE", PasswordHash: "hash"}}, &countingHasher{})
	unknownStatus, unknownErr := loginRequest(t, unknown, "missing")
	knownStatus, knownErr := loginRequest(t, known, "alice")
	if unknownStatus != http.StatusUnauthorized || knownStatus != http.StatusUnauthorized || unknownErr.Code != "AUTH_INVALID_CREDENTIALS" || knownErr.Code != unknownErr.Code {
		t.Fatalf("unknown=%d/%s known=%d/%s", unknownStatus, unknownErr.Code, knownStatus, knownErr.Code)
	}
	for index := 0; index < 3; index++ {
		loginRequest(t, unknown, "another")
	}
	status, apiErr := loginRequest(t, unknown, "another")
	if status != http.StatusTooManyRequests || apiErr.Code != "AUTH_RATE_LIMITED" {
		t.Fatalf("status=%d code=%s", status, apiErr.Code)
	}
}
