package tags

import (
	"time"

	"github.com/google/uuid"
)

type Tag struct {
	ID, UserID                  uuid.UUID
	Name, NormalizedName, Color string
	CreatedAt, UpdatedAt        time.Time
}
