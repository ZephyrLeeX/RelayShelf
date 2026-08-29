package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/webui"
)

func embeddedAssetNames(t *testing.T) []string {
	t.Helper()
	names, err := webui.AssetNames()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// TestSecurityHeadersCoverDocumentAPIAndAssetResponses verifies the Phase 11
// header baseline through the real top-level router: the embedded SPA document,
// hashed static assets, and JSON API responses all carry the same baseline.
func TestSecurityHeadersCoverDocumentAPIAndAssetResponses(t *testing.T) {
	origin, err := url.Parse("https://public.example")
	if err != nil {
		t.Fatal(err)
	}
	middleware := auth.NewMiddleware(nil, auth.NewCookiePolicy(origin), nil, origin, httpx.NewResolver(nil))
	api := auth.Router(&apiHandler{}, middleware)
	router := newHTTPRouter(middleware.Host, api, health(http.StatusOK), health(http.StatusOK))

	cases := []struct {
		name string
		path string
	}{
		{"spa document", "/temporary"},
		{"spa root", "/"},
		{"api json", "/api/v1/search?q=postgres"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://public.example"+testCase.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			headers := response.Header()
			if got := headers.Get("Content-Security-Policy"); got != httpx.ContentSecurityPolicy {
				t.Fatalf("csp=%q", got)
			}
			if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("nosniff=%q", got)
			}
			if got := headers.Get("Referrer-Policy"); got != httpx.ReferrerPolicy {
				t.Fatalf("referrer=%q", got)
			}
			if got := headers.Get("Permissions-Policy"); got != httpx.PermissionsPolicy {
				t.Fatalf("permissions=%q", got)
			}
		})
	}

	// Hashed Vite assets must stay immutable while keeping the same baseline.
	assets := embeddedAssetNames(t)
	if len(assets) == 0 {
		t.Fatal("embedded web distribution has no hashed assets")
	}
	request := httptest.NewRequest(http.MethodGet, "https://public.example"+assets[0], nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("asset status=%d", response.Code)
	}
	if cache := response.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Fatalf("asset cache=%q", cache)
	}
	if got := response.Header().Get("Content-Security-Policy"); got != httpx.ContentSecurityPolicy {
		t.Fatalf("asset csp=%q", got)
	}
}

// TestSensitiveAndAttachmentHeadersPreservedUnderBaseline documents that the
// no-store attachment/sensitive overrides set by module handlers are not
// weakened by the global security-header middleware.
func TestSensitiveAndAttachmentHeadersPreservedUnderBaseline(t *testing.T) {
	origin, _ := url.Parse("https://public.example")
	middleware := auth.NewMiddleware(nil, auth.NewCookiePolicy(origin), nil, origin, httpx.NewResolver(nil))
	api := auth.Router(&apiHandler{}, middleware)
	router := newHTTPRouter(middleware.Host, api, health(http.StatusOK), health(http.StatusOK))

	request := httptest.NewRequest(http.MethodGet, "https://public.example/api/v1/search?q=x", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
	if cache := response.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("search cache=%q", cache)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff=%q", got)
	}
}
