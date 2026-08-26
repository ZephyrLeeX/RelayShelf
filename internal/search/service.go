package search

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/google/uuid"
)

type Clock interface{ Now() time.Time }

type Service struct {
	repository repository
	clock      Clock
}

func NewService(repository repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

func (s *Service) Search(ctx context.Context, ownerID uuid.UUID, query Query) (Page, error) {
	if ownerID == uuid.Nil {
		return Page{}, ErrValidation
	}
	if query.Limit == 0 {
		query.Limit = DefaultLimit
	}
	if query.Limit < 1 || query.Limit > MaxLimit {
		return Page{}, ErrValidation
	}
	tokens, err := normalizeTokens(query.Tokens)
	if err != nil {
		return Page{}, err
	}
	query.Tokens = tokens
	if query.Lifecycle != nil && *query.Lifecycle != messages.Temporary && *query.Lifecycle != messages.Permanent {
		return Page{}, ErrValidation
	}
	if query.DetectedType != nil {
		value := strings.TrimSpace(*query.DetectedType)
		if value == "" || !utf8.ValidString(value) || len(value) > MaxTypeBytes {
			return Page{}, ErrValidation
		}
		value = strings.ToLower(value)
		query.DetectedType = &value
	}
	if query.CreatedAfter != nil && query.CreatedBefore != nil && !query.CreatedAfter.Before(*query.CreatedBefore) {
		return Page{}, ErrValidation
	}
	uniqueTags := make([]uuid.UUID, 0, len(query.TagIDs))
	seenTags := make(map[uuid.UUID]struct{}, len(query.TagIDs))
	for _, tagID := range query.TagIDs {
		if tagID == uuid.Nil {
			return Page{}, ErrValidation
		}
		if _, exists := seenTags[tagID]; !exists {
			seenTags[tagID] = struct{}{}
			uniqueTags = append(uniqueTags, tagID)
		}
	}
	query.TagIDs = uniqueTags

	rows, err := s.repository.Search(ctx, ownerID, query, s.clock.Now())
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: make([]messages.Summary, 0, min(len(rows), query.Limit))}
	if len(rows) > query.Limit {
		marker := rows[query.Limit-1]
		cursor := EncodeCursor(Cursor{CreatedAt: marker.CreatedAt, MessageID: marker.ID})
		page.NextCursor = &cursor
		rows = rows[:query.Limit]
	}
	for _, message := range rows {
		summary := messages.Summary{Message: message, AttachmentCount: message.AttachmentTotal}
		summary.BodyPlaintext = nil
		if !message.Sensitive && message.BodyPlaintext != nil {
			summary.BodyPreview, summary.BodyTruncated = preview(*message.BodyPlaintext)
		}
		page.Items = append(page.Items, summary)
	}
	return page, nil
}

func preview(body string) (*string, bool) {
	if len(body) <= maxPreviewBytes {
		value := body
		return &value, false
	}
	cut := maxPreviewBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	value := body[:cut]
	return &value, true
}
