package search

import (
	"context"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/google/uuid"
)

const (
	MaxQueryBytes   = 1024
	MaxTokens       = 16
	MaxTokenRunes   = 128
	MinTokenRunes   = 2
	MaxTypeBytes    = 64
	DefaultLimit    = messages.DefaultLimit
	MaxLimit        = messages.MaxLimit
	maxPreviewBytes = messages.MaxPreviewBytes
)

type Query struct {
	Tokens                      []string
	Lifecycle                   *string
	Favorite                    *bool
	TagIDs                      []uuid.UUID
	DetectedType                *string
	CreatedAfter, CreatedBefore *time.Time
	Cursor                      *Cursor
	Limit                       int
}

type Page = messages.Page

type repository interface {
	Search(ctx context.Context, ownerID uuid.UUID, query Query, now time.Time) ([]messages.Message, error)
}
