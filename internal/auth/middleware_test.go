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
	handler := mw.CSRF(next)
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
	if status := request(http.MethodGet, "", "", ""); status != http.StatusOK || !passed {
		t.Fatalf("safe status=%d", status)
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
