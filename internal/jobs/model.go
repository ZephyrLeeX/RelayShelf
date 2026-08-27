package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const (
	TypeGenerateThumbnail = "GENERATE_THUMBNAIL"
	SubjectFileObject     = "FILE_OBJECT"
	StatusPending         = "PENDING"
	StatusRunning         = "RUNNING"
	StatusCompleted       = "COMPLETED"
	StatusFailed          = "FAILED"
	DefaultMaxAttempts    = 8
	DefaultPollInterval   = 45 * time.Second
	DefaultStuckTimeout   = 15 * time.Minute
	DefaultDrainLimit     = 32
)

type Job struct {
	ID                              uuid.UUID
	Type, SubjectType, Status       string
	SubjectID                       *uuid.UUID
	Attempts                        int
	NextRunAt                       time.Time
	StartedAt                       *time.Time
	LastErrorCode, LastErrorSummary *string
	CreatedAt, UpdatedAt            time.Time
}

type Handler interface {
	Handle(context.Context, Job) error
}

type Clock interface{ Now() time.Time }
