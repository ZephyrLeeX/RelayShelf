package users

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
)

const (
	MinPasswordRunes = 10
	MaxPasswordBytes = 1024
)

type PasswordHasher interface {
	Hash(password string) (string, error)
}

type Repository interface {
	Create(context.Context, User) (User, error)
	GetByUsername(context.Context, string) (User, error)
	GetByID(context.Context, string) (User, error)
	Disable(context.Context, string, time.Time) error
	Delete(context.Context, string) error
}

type Clock interface{ Now() time.Time }

type Service struct {
	repo Repository
	hash PasswordHasher
	ids  id.Generator
	now  Clock
}

func NewService(repo Repository, hash PasswordHasher, ids id.Generator, now Clock) *Service {
	return &Service{repo: repo, hash: hash, ids: ids, now: now}
}

func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordRunes || len(password) > MaxPasswordBytes {
		return ErrInvalidPassword
	}
	return nil
}

func (s *Service) Create(ctx context.Context, username, displayName, password string, admin bool) (User, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	if err = ValidatePassword(password); err != nil {
		return User{}, err
	}
	encoded, err := s.hash.Hash(password)
	if err != nil {
		return User{}, err
	}
	uid, err := s.ids.New()
	if err != nil {
		return User{}, err
	}
	now := s.now.Now()
	return s.repo.Create(ctx, User{ID: uid, Username: normalized, DisplayName: displayName, PasswordHash: encoded, IsAdmin: admin, Status: StatusActive, CreatedAt: now, UpdatedAt: now})
}

func (s *Service) DisableUser(ctx context.Context, userID string) error {
	return s.repo.Disable(ctx, userID, s.now.Now())
}
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.repo.Delete(ctx, userID)
}
