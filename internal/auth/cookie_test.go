package auth

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCookiePolicyFromPublicOrigin(t *testing.T) {
	secureOrigin, _ := url.Parse("https://example.test")
	secure := NewCookiePolicy(secureOrigin)
	if secure.Name != "__Host-session" || !secure.Secure {
		t.Fatal("HTTPS cookie policy is not host-only secure")
	}
	w := httptest.NewRecorder()
	secure.Set(w, "opaque-token", time.Now().Add(time.Hour))
	value := w.Header().Get("Set-Cookie")
	for _, expected := range []string{"__Host-session=opaque-token", "Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("cookie %q missing %q", value, expected)
		}
	}
	httpOrigin, _ := url.Parse("http://localhost:8080")
	local := NewCookiePolicy(httpOrigin)
	if local.Name != "relayshelf_session" || local.Secure {
		t.Fatal("HTTP development cookie policy incorrect")
	}
}
