package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/google/uuid"
)

const DefaultHeartbeat = 25 * time.Second

type SessionValidator interface {
	ValidateSession(context.Context, uuid.UUID) (auth.Authentication, error)
}

type Handler struct {
	hub       *Hub
	validator SessionValidator
	heartbeat time.Duration
}

func NewHandler(hub *Hub, validator SessionValidator) *Handler {
	return &Handler{hub: hub, validator: validator, heartbeat: DefaultHeartbeat}
}

func (h *Handler) GetEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		auth.WriteError(w, r, http.StatusInternalServerError, "SSE_UNSUPPORTED", "event streaming is unavailable")
		return
	}
	a, ok := auth.FromContext(r.Context())
	if !ok {
		auth.WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ctx, sub, unregister := h.hub.Subscribe(r.Context(), a.User.ID)
	defer unregister()
	heartbeat := time.NewTicker(h.heartbeat)
	defer heartbeat.Stop()
	expiry := nearest(a.Session.ExpiresAt, a.Session.AbsoluteExpiresAt)
	delay := time.Until(expiry)
	if delay < 0 {
		delay = 0
	}
	expiryTimer := time.NewTimer(delay)
	defer expiryTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-expiryTimer.C:
			return
		case event := <-sub.Events:
			if writeEvent(w, event) != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			validated, err := h.validator.ValidateSession(ctx, a.Session.ID)
			if err != nil {
				return
			}
			next := nearest(validated.Session.ExpiresAt, validated.Session.AbsoluteExpiresAt)
			if !expiryTimer.Stop() {
				select {
				case <-expiryTimer.C:
				default:
				}
			}
			delay = time.Until(next)
			if delay < 0 {
				return
			}
			expiryTimer.Reset(delay)
			if _, err = w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func nearest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func writeEvent(w http.ResponseWriter, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return errors.New("invalid sse event")
	}
	_, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, data)
	return err
}
