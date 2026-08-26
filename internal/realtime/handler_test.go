package realtime

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/google/uuid"
)

type fakeValidator struct {
	a     auth.Authentication
	mu    sync.Mutex
	calls int
}

func (v *fakeValidator) ValidateSession(context.Context, uuid.UUID) (auth.Authentication, error) {
	v.mu.Lock()
	v.calls++
	v.mu.Unlock()
	return v.a, nil
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	flushes int
}

func (f *flushRecorder) Flush() { f.mu.Lock(); f.flushes++; f.mu.Unlock() }

func TestSSEWritesEventAndHeartbeat(t *testing.T) {
	now := time.Now()
	user, device, session := uuid.New(), uuid.New(), uuid.New()
	a := auth.Authentication{User: auth.User{ID: user, Status: "ACTIVE"}, Device: auth.Device{ID: device, UserID: user}, Session: auth.Session{ID: session, UserID: user, DeviceID: device, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour)}}
	hub := NewHub()
	validator := &fakeValidator{a: a}
	handler := NewHandler(hub, validator)
	handler.heartbeat = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(auth.ContextWithAuthentication(context.Background(), a))
	req := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() { handler.GetEvents(rec, req); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		hub.mu.RLock()
		n := len(hub.users[user])
		hub.mu.RUnlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber not registered")
		}
		time.Sleep(time.Millisecond)
	}
	e := Event{ID: uuid.New(), Type: MessageUpdated, ResourceID: uuid.New(), OccurredAt: now}
	hub.Publish(user, e)
	for {
		validator.mu.Lock()
		calls := validator.calls
		validator.mu.Unlock()
		if calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("heartbeat did not validate session")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type=%q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "id: "+e.ID.String()) || !strings.Contains(body, `"resourceId":"`+e.ResourceID.String()+`"`) {
		t.Fatalf("invalid frame %q", body)
	}
}
