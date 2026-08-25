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
