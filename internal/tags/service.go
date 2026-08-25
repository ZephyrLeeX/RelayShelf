package tags

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
)

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type Clock interface{ Now() time.Time }
type Service struct {
	repo  *PostgreSQLRepository
	ids   id.Generator
	clock Clock
}

func NewService(repo *PostgreSQLRepository, ids id.Generator, clock Clock) *Service {
	return &Service{repo: repo, ids: ids, clock: clock}
}
func normalize(name, color string) (string, string, string, error) {
	display := strings.TrimSpace(name)
	n := utf8.RuneCountInString(display)
	if n < 1 || n > 64 || !colorPattern.MatchString(color) {
		return "", "", "", ErrValidation
	}
	return display, strings.ToLower(display), strings.ToUpper(color), nil
}
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Tag, error) {
	return s.repo.List(ctx, userID)
}
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name, color string) (Tag, error) {
	display, normalized, safeColor, err := normalize(name, color)
	if err != nil {
		return Tag{}, err
	}
	tagID, err := s.ids.New()
	if err != nil {
		return Tag{}, err
	}
	now := s.clock.Now()
	return s.repo.Create(ctx, Tag{ID: tagID, UserID: userID, Name: display, NormalizedName: normalized, Color: safeColor, CreatedAt: now, UpdatedAt: now})
}
func (s *Service) Update(ctx context.Context, userID, tagID uuid.UUID, name, color *string) (Tag, error) {
	if name == nil && color == nil {
		return Tag{}, ErrValidation
	}
	current, err := s.repo.Get(ctx, userID, tagID)
	if err != nil {
		return Tag{}, err
	}
	nextName, nextColor := current.Name, current.Color
	if name != nil {
		nextName = *name
	}
	if color != nil {
		nextColor = *color
	}
	display, normalized, safeColor, err := normalize(nextName, nextColor)
	if err != nil {
		return Tag{}, err
	}
	if display == current.Name && safeColor == current.Color {
		return current, nil
	}
	return s.repo.Update(ctx, Tag{ID: tagID, UserID: userID, Name: display, NormalizedName: normalized, Color: safeColor, UpdatedAt: s.clock.Now()})
}
func (s *Service) Delete(ctx context.Context, userID, tagID uuid.UUID) error {
	return s.repo.Delete(ctx, userID, tagID, s.clock.Now())
}
