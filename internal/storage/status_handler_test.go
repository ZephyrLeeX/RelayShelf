package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusHandlerProjectsMonitorMemoryOnly(t *testing.T) {
	probeStarted := make(chan struct{})
	m := NewMonitorWithProbe(func(context.Context) error {
		close(probeStarted)
		select {}
	}, time.Hour, 1)
	go m.probeOnce(t.Context())
	<-probeStarted

	started := time.Now()
	w := httptest.NewRecorder()
	NewStatusHandler(m).GetStorageRuntimeStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/storage/status", nil))
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("in-memory status blocked for %s", elapsed)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "/mnt/") || strings.Contains(w.Body.String(), "nas.example") {
		t.Fatalf("response leaked infrastructure: %s", w.Body.String())
	}
}

func TestStatusHandlerHealthyAndDegradedReasons(t *testing.T) {
	m := NewMonitorWithProbe(func(context.Context) error { return nil }, time.Second, 1)
	assert := func(healthy bool, reason string) {
		t.Helper()
		w := httptest.NewRecorder()
		NewStatusHandler(m).GetStorageRuntimeStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/storage/status", nil))
		var got struct {
			Healthy bool   `json:"healthy"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Healthy != healthy || got.Reason != reason {
			t.Fatalf("got %+v want healthy=%t reason=%s", got, healthy, reason)
		}
	}
	assert(true, "HEALTHY")
	m.settle(false, "NAS_UNAVAILABLE")
	assert(false, "NAS_UNAVAILABLE")
	m.settle(true, "")
	assert(true, "HEALTHY")
	m.settle(false, "NAS_FULL")
	assert(false, "NAS_FULL")
	m.settle(true, "")
	m.settle(false, "NAS_TIMEOUT")
	assert(false, "NAS_TIMEOUT")
}
