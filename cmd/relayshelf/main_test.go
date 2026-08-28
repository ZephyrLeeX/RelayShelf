package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
)

func TestHealthBypassesPublicHostValidation(t *testing.T) {
	origin, err := url.Parse("https://public.example")
	if err != nil {
		t.Fatal(err)
	}
	middleware := auth.NewMiddleware(nil, auth.CookiePolicy{}, nil, origin, httpx.NewResolver(nil))
	apiCalled := false
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	router := newHTTPRouter(middleware.Host, api, health(http.StatusOK), health(http.StatusOK))

	healthRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/health/live", nil)
	healthRequest.Host = "127.0.0.1:8080"
	healthResponse := httptest.NewRecorder()
	router.ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", healthResponse.Code, healthResponse.Body.String())
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/test", nil)
	apiRequest.Host = "127.0.0.1:8080"
	apiResponse := httptest.NewRecorder()
	router.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusForbidden || apiCalled {
		t.Fatalf("api status=%d called=%v", apiResponse.Code, apiCalled)
	}
}

func TestSearchRouteRequiresAuthenticationAndIsNoStore(t *testing.T) {
	origin, err := url.Parse("https://public.example")
	if err != nil {
		t.Fatal(err)
	}
	middleware := auth.NewMiddleware(nil, auth.NewCookiePolicy(origin), nil, origin, httpx.NewResolver(nil))
	api := auth.Router(&apiHandler{}, middleware)
	router := newHTTPRouter(middleware.Host, api, health(http.StatusOK), health(http.StatusOK))

	request := httptest.NewRequest(http.MethodGet, "https://public.example/api/v1/search?q=postgres", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
}

func TestSPAFallbackDoesNotCaptureAPIOrMissingAssets(t *testing.T) {
	router := newHTTPRouter(func(handler http.Handler) http.Handler { return handler }, http.NotFoundHandler(), health(http.StatusOK), health(http.StatusOK))

	for _, path := range []string{"/temporary", "/messages/0198a000-0000-7000-8000-000000000001", "/search?q=postgres"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("path=%s status=%d type=%q", path, response.Code, response.Header().Get("Content-Type"))
		}
	}

	for _, path := range []string{"/api/v1/not-found", "/assets/not-found.js"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d", path, response.Code)
		}
	}
}
