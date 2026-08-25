package users

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxUsernameRunes = 64

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

type User struct {
	ID           uuid.UUID
	Username     string
	DisplayName  string
	PasswordHash string
	IsAdmin      bool
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NormalizeUsername(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	count := utf8.RuneCountInString(normalized)
	if count < 1 || count > MaxUsernameRunes {
		return "", ErrInvalidUsername
	}
	return normalized, nil
}
