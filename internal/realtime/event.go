package realtime

import (
	"time"

	"github.com/google/uuid"
)

const (
	MessageCreated = "message.created"
	MessageUpdated = "message.updated"
	MessageDeleted = "message.deleted"
	TagCreated     = "tag.created"
	TagUpdated     = "tag.updated"
	TagDeleted     = "tag.deleted"
)

type Event struct {
	ID             uuid.UUID  `json:"id"`
	Type           string     `json:"type"`
	ResourceID     uuid.UUID  `json:"resourceId"`
	Version        *int64     `json:"version,omitempty"`
	OriginDeviceID *uuid.UUID `json:"originDeviceId,omitempty"`
	OccurredAt     time.Time  `json:"occurredAt"`
}

type Publisher interface{ Publish(uuid.UUID, Event) }
type NopPublisher struct{}

func (NopPublisher) Publish(uuid.UUID, Event) {}
