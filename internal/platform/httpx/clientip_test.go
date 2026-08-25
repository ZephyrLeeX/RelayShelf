package httpx

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestResolverTrustBoundary(t *testing.T) {
	r := NewResolver([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	untrusted := httptest.NewRequest("GET", "http://example.test/", nil)
	untrusted.RemoteAddr = "192.0.2.10:1234"
	untrusted.Header.Set("X-Forwarded-For", "127.0.0.1")
	info, err := r.Resolve(untrusted)
	if err != nil || info.ClientIP.String() != "192.0.2.10" {
		t.Fatalf("untrusted result=%+v err=%v", info, err)
	}
	trusted := httptest.NewRequest("GET", "http://internal/", nil)
	trusted.RemoteAddr = "10.0.0.2:1234"
	trusted.Header.Set("X-Forwarded-For", "198.51.100.4")
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trusted.Header.Set("X-Forwarded-Host", "example.test")
	info, err = r.Resolve(trusted)
	if err != nil || info.ClientIP.String() != "198.51.100.4" || info.Scheme != "https" || info.Host != "example.test" {
		t.Fatalf("trusted result=%+v err=%v", info, err)
	}
	trusted.Header.Set("X-Forwarded-For", "bad")
	if _, err = r.Resolve(trusted); err == nil {
		t.Fatal("malformed forwarded address accepted")
	}
	trusted.Header.Set("X-Forwarded-For", "127.0.0.1, 198.51.100.4")
	info, err = r.Resolve(trusted)
	if err != nil || info.ClientIP.String() != "198.51.100.4" {
		t.Fatalf("spoofed leftmost address won: %+v %v", info, err)
	}
}
