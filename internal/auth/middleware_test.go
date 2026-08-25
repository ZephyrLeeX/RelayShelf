package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
)

func TestCSRFMiddleware(t *testing.T) {
	origin, _ := url.Parse("https://example.test")
	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	authn := validAuthentication(time.Now())
	mw := NewMiddleware(nil, CookiePolicy{}, csrf, origin, httpx.NewResolver(nil))
	passed := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true })
	handler := mw.RequireSameOrigin(mw.CSRF(next))
	request := func(method, originHeader, referer, token string) int {
		passed = false
		r := httptest.NewRequest(method, "https://example.test/api/v1/x", nil)
		r.Header.Set("Origin", originHeader)
		r.Header.Set("Referer", referer)
		r.Header.Set("X-CSRF-Token", token)
		r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, AuthContext{Authentication: authn}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	valid := csrf.Token(authn.Session.ID)
	if status := request(http.MethodPost, "https://example.test", "", valid); status != http.StatusOK || !passed {
		t.Fatalf("valid status=%d passed=%v", status, passed)
	}
	if status := request(http.MethodPost, "https://example.test", "", "bad"); status != http.StatusForbidden {
		t.Fatalf("invalid token status=%d", status)
	}
	if status := request(http.MethodPost, "https://evil.test", "", valid); status != http.StatusForbidden {
		t.Fatalf("wrong origin status=%d", status)
	}
	if status := request(http.MethodDelete, "", "https://example.test/path", valid); status != http.StatusOK || !passed {
		t.Fatalf("referer status=%d", status)
	}
	if status := request(http.MethodPost, "", "", valid); status != http.StatusForbidden {
		t.Fatalf("missing origin status=%d", status)
	}
}

func TestRequestActivityPipeline(t *testing.T) {
	origin, _ := url.Parse("https://example.test")
	clock := &fakeClock{now: time.Now()}
	authn := validAuthentication(clock.now)
	repo := &memoryRepo{auth: authn, sessions: []Session{authn.Session}}
	service := NewService(repo, testHasher(), &sequenceIDs{}, clock, NewRateLimiter(clock, 10))
	csrf := NewCSRF([]byte("01234567890123456789012345678901"))
	cookies := NewCookiePolicy(origin)
	mw := NewMiddleware(service, cookies, csrf, origin, httpx.NewResolver(nil))
	router := Router(NewHandler(service, csrf, cookies), mw)
	token, _, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	request := func(method, path, originHeader, csrfToken string) int {
		r := httptest.NewRequest(method, "https://example.test"+path, nil)
		r.AddCookie(&http.Cookie{Name: cookies.Name, Value: token})
		r.Header.Set("Origin", originHeader)
		r.Header.Set("X-CSRF-Token", csrfToken)
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}

	validCSRF := csrf.Token(authn.Session.ID)
	if status := request(http.MethodPost, "/api/v1/auth/logout", "https://example.test", validCSRF); status != http.StatusNoContent {
		t.Fatalf("valid unsafe status=%d", status)
	}
	if repo.touchCount != 1 || repo.authLookups != 1 {
		t.Fatalf("valid unsafe touches=%d auth lookups=%d", repo.touchCount, repo.authLookups)
	}

	for _, test := range []struct {
		name, origin, token string
	}{
		{name: "invalid csrf", origin: "https://example.test", token: "invalid"},
		{name: "wrong origin", origin: "https://evil.test", token: validCSRF},
		{name: "missing origin and referer", token: validCSRF},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo.touchCount, repo.authLookups = 0, 0
			repo.auth = authn
			if status := request(http.MethodPost, "/api/v1/auth/logout", test.origin, test.token); status != http.StatusForbidden {
				t.Fatalf("status=%d", status)
			}
			if repo.touchCount != 0 {
				t.Fatalf("touches=%d", repo.touchCount)
			}
			if repo.authLookups != 1 {
				t.Fatalf("auth lookups=%d", repo.authLookups)
			}
		})
	}

	repo.touchCount, repo.authLookups = 0, 0
	repo.auth = authn
	if status := request(http.MethodPost, "/api/v1/messages", "https://example.test", "invalid"); status != http.StatusForbidden {
		t.Fatalf("Phase 3 unsafe route bypassed CSRF: status=%d", status)
	}
	if repo.touchCount != 0 || repo.authLookups != 1 {
		t.Fatalf("Phase 3 rejection touches=%d lookups=%d", repo.touchCount, repo.authLookups)
	}

	repo.touchCount, repo.authLookups = 0, 0
	repo.auth = authn
	if status := request(http.MethodGet, "/api/v1/auth/session", "", ""); status != http.StatusOK {
		t.Fatalf("safe interactive GET status=%d", status)
	}
	if repo.touchCount != 1 || repo.authLookups != 1 {
		t.Fatalf("safe GET touches=%d auth lookups=%d", repo.touchCount, repo.authLookups)
	}
}

func TestAuthenticateWithoutTouchPreservesNonInteractiveCapability(t *testing.T) {
	origin, _ := url.Parse("https://example.test")
	clock := &fakeClock{now: time.Now()}
	authn := validAuthentication(clock.now)
	repo := &memoryRepo{auth: authn}
	service := NewService(repo, testHasher(), &sequenceIDs{}, clock, NewRateLimiter(clock, 10))
	cookies := NewCookiePolicy(origin)
	mw := NewMiddleware(service, cookies, NewCSRF([]byte("01234567890123456789012345678901")), origin, httpx.NewResolver(nil))
	token, _, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	handler := mw.Authenticate(false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodGet, "https://example.test/future-stream", nil)
	r.AddCookie(&http.Cookie{Name: cookies.Name, Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || repo.touchCount != 0 || repo.authLookups != 1 {
		t.Fatalf("status=%d touches=%d auth lookups=%d", w.Code, repo.touchCount, repo.authLookups)
	}
}

func TestExpiredSessionFailsWhileCookieExists(t *testing.T) {
	origin, _ := url.Parse("https://example.test")
	clock := &fakeClock{now: time.Now()}
	cookies := NewCookiePolicy(origin)
	token, _, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		expire func(*Authentication)
	}{
		{name: "idle expired", expire: func(a *Authentication) { a.Session.ExpiresAt = clock.now }},
		{name: "absolute expired", expire: func(a *Authentication) { a.Session.AbsoluteExpiresAt = clock.now }},
	} {
		t.Run(test.name, func(t *testing.T) {
			authn := validAuthentication(clock.now)
			test.expire(&authn)
			repo := &memoryRepo{auth: authn}
			service := NewService(repo, testHasher(), &sequenceIDs{}, clock, NewRateLimiter(clock, 10))
			mw := NewMiddleware(service, cookies, NewCSRF([]byte("01234567890123456789012345678901")), origin, httpx.NewResolver(nil))
			handler := mw.Authenticate(false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("expired session reached handler") }))
			r := httptest.NewRequest(http.MethodGet, "https://example.test/api/v1/auth/session", nil)
			r.AddCookie(&http.Cookie{Name: cookies.Name, Value: token, Expires: clock.now.Add(AbsoluteLifetime)})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized || repo.touchCount != 0 {
				t.Fatalf("status=%d touches=%d", w.Code, repo.touchCount)
			}
		})
	}
}

func TestHostValidationAndUntrustedForwarding(t *testing.T) {
	origin, _ := url.Parse("https://public.test")
	mw := NewMiddleware(nil, CookiePolicy{}, nil, origin, httpx.NewResolver([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}))
	passed := false
	handler := mw.Host(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true }))
	r := httptest.NewRequest("GET", "http://internal/", nil)
	r.RemoteAddr = "192.0.2.2:1"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "public.test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || passed {
		t.Fatal("untrusted forwarding was accepted")
	}
	r = httptest.NewRequest("GET", "http://internal/", nil)
	r.RemoteAddr = "10.0.0.2:1"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "public.test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !passed {
		t.Fatalf("trusted forwarding status=%d passed=%v", w.Code, passed)
	}
}
