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
)

func loginRequest(t *testing.T, handler http.Handler, username string) (int, httpapi.Error) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": "wrong-password"})
	r := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/auth/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var apiErr httpapi.Error
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	return w.Code, apiErr
}

func TestLoginHTTPEnumerationAndRateLimitContract(t *testing.T) {
	origin, _ := url.Parse("http://example.test")
	clock := &fakeClock{now: time.Now()}
	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	build := func(repo *memoryRepo, hasher PasswordHasher) http.Handler {
		service := NewService(repo, hasher, &sequenceIDs{}, clock, NewRateLimiter(clock, 32))
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
