package messages

import (
	"time"

	"github.com/google/uuid"
)

const (
	MaxBodyBytes    = 1 << 20
	MaxPreviewBytes = 16 << 10
	DefaultLimit    = 30
	MaxLimit        = 100
	Temporary       = "TEMPORARY"
	Permanent       = "PERMANENT"
	Text            = "TEXT"
	Markdown        = "MARKDOWN"
)

type Tag struct {
	ID                   uuid.UUID
	Name, Color          string
	CreatedAt, UpdatedAt time.Time
}

type Message struct {
	ID, OwnerID                    uuid.UUID
	BodyPlaintext                  *string
	BodyCiphertext, BodyNonce      []byte
	BodyEncryptionVersion          *int16
	BodyFormat                     string
	DetectedType, DetectedLanguage *string
	Sensitive                      bool
	Lifecycle                      string
	Favorite                       bool
	ExpiresAt, TrashedAt, PurgeAt  *time.Time
	SourceUserID, SourceMessageID  *uuid.UUID
	CreatedDeviceID                *uuid.UUID
	Version                        int64
	CreatedAt, UpdatedAt           time.Time
	Tags                           []Tag
}

type Summary struct {
	Message
	BodyPreview   *string
	BodyTruncated bool
}

type Page struct {
	Items      []Summary
	NextCursor *string
}

type CreateCommand struct {
	Body, BodyFormat, Lifecycle, IdempotencyKey string
	Sensitive                                   bool
	TagIDs                                      []uuid.UUID
}

type EditCommand struct {
	ExpectedVersion                int64
	Body, BodyFormat               *string
	DetectedType, DetectedLanguage OptionalString
}

type OptionalString struct {
	Set   bool
	Value *string
}

type ListFilter struct {
	Lifecycle *string
	Favorite  *bool
	TagIDs    []uuid.UUID
	Cursor    *Cursor
	Limit     int
}

type DirectSendCommand struct {
	RecipientID                      uuid.UUID
	Body, BodyFormat, IdempotencyKey string
	Sensitive                        bool
}

type ForwardCommand struct {
	SourceID, RecipientID uuid.UUID
	ExpectedVersion       int64
	IdempotencyKey        string
}

// AttachmentCopier is the Phase 5 extension point. The text-only Phase 3
// implementation intentionally has no implementation and never accesses files.
type AttachmentCopier interface {
	CopyForForward(sourceMessageID, destinationMessageID uuid.UUID) error
}
