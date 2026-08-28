package audit

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventRuntimeSettingsUpdated EventType = "RUNTIME_SETTINGS_UPDATED"
	EventUserCreated            EventType = "USER_CREATED"
	EventUserDisabled           EventType = "USER_DISABLED"
	EventUserPasswordReset      EventType = "USER_PASSWORD_RESET"
	EventUserDeleted            EventType = "USER_DELETED"
)

type Actor struct {
	UserID, DeviceID, SessionID uuid.UUID
	IP                          netip.Addr
	UserAgent, TraceID          string
}

// Event metadata is deliberately private. Callers can only create events
// through the typed constructors below, which are the audit allowlist.
type Event struct {
	Type       EventType
	TargetType string
	TargetID   uuid.UUID
	Actor      Actor
	metadata   map[string]any
	CreatedAt  time.Time
}

func RuntimeSettingsUpdated(actor Actor, changedFields []string) Event {
	return Event{Type: EventRuntimeSettingsUpdated, TargetType: "SYSTEM_SETTINGS", TargetID: uuid.Nil, Actor: actor, metadata: map[string]any{"changedFields": append([]string(nil), changedFields...)}}
}

func UserCreated(actor Actor, target uuid.UUID, username string, isAdmin bool) Event {
	return Event{Type: EventUserCreated, TargetType: "USER", TargetID: target, Actor: actor, metadata: map[string]any{"username": username, "isAdmin": isAdmin}}
}

func UserDisabled(actor Actor, target uuid.UUID) Event {
	return Event{Type: EventUserDisabled, TargetType: "USER", TargetID: target, Actor: actor, metadata: map[string]any{}}
}

func UserPasswordReset(actor Actor, target uuid.UUID) Event {
	return Event{Type: EventUserPasswordReset, TargetType: "USER", TargetID: target, Actor: actor, metadata: map[string]any{}}
}

func UserDeleted(actor Actor, target uuid.UUID, username string) Event {
	return Event{Type: EventUserDeleted, TargetType: "USER", TargetID: target, Actor: actor, metadata: map[string]any{"username": username}}
}
