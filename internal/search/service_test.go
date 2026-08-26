package search

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/google/uuid"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type fakeRepository struct {
	rows  []messages.Message
	err   error
	query Query
	ctx   context.Context
}

func (repository *fakeRepository) Search(ctx context.Context, _ uuid.UUID, query Query, _ time.Time) ([]messages.Message, error) {
	repository.ctx = ctx
	repository.query = query
	return repository.rows, repository.err
}

func TestServiceValidationDedupPreviewAndPagination(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	for index := 0; index < 31; index++ {
		body := strings.Repeat("界", 6000)
		repository.rows = append(repository.rows, messages.Message{ID: uuid.Must(uuid.NewV7()), OwnerID: uuid.Must(uuid.NewV7()), BodyPlaintext: &body, CreatedAt: now.Add(-time.Duration(index) * time.Second)})
	}
	service := NewService(repository, fixedClock{now})
	lifecycle := messages.Permanent
	detectedType := " SQL "
	tagID := uuid.Must(uuid.NewV7())
	page, err := service.Search(context.Background(), uuid.Must(uuid.NewV7()), Query{Tokens: []string{"docker"}, Lifecycle: &lifecycle, DetectedType: &detectedType, TagIDs: []uuid.UUID{tagID, tagID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != DefaultLimit || page.NextCursor == nil || len(repository.query.TagIDs) != 1 || *repository.query.DetectedType != "sql" {
		t.Fatalf("page=%d cursor=%v query=%+v", len(page.Items), page.NextCursor, repository.query)
	}
	if page.Items[0].BodyPreview == nil || len(*page.Items[0].BodyPreview) > maxPreviewBytes || !page.Items[0].BodyTruncated {
		t.Fatalf("preview bytes=%d truncated=%v", len(*page.Items[0].BodyPreview), page.Items[0].BodyTruncated)
	}
	if page.Items[0].BodyPlaintext != nil {
		t.Fatal("summary retained full plaintext")
	}
	if _, err = DecodeCursor(*page.NextCursor); err != nil {
		t.Fatalf("next cursor: %v", err)
	}
}

func TestServiceRejectsInvalidFilters(t *testing.T) {
	service := NewService(&fakeRepository{}, fixedClock{time.Now()})
	owner := uuid.Must(uuid.NewV7())
	badLifecycle := "ALL"
	emptyType := " "
	after, before := time.Now(), time.Now().Add(-time.Hour)
	for _, query := range []Query{{Limit: 101}, {Lifecycle: &badLifecycle}, {DetectedType: &emptyType}, {CreatedAfter: &after, CreatedBefore: &before}, {TagIDs: []uuid.UUID{uuid.Nil}}} {
		if _, err := service.Search(context.Background(), owner, query); !errors.Is(err, ErrValidation) {
			t.Fatalf("query=%+v error=%v", query, err)
		}
	}
}

func TestServicePropagatesContext(t *testing.T) {
	repository := &fakeRepository{err: context.Canceled}
	service := NewService(repository, fixedClock{time.Now()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Search(ctx, uuid.Must(uuid.NewV7()), Query{})
	if !errors.Is(err, context.Canceled) || repository.ctx != ctx {
		t.Fatalf("error=%v same context=%v", err, repository.ctx == ctx)
	}
}
