package users

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultRecipientLimit  = 5
	MaxRecipientLimit      = 20
	MaxRecipientQueryRunes = 100
)

type Recipient struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
}

type DirectoryService struct{ pool *pgxpool.Pool }

func NewDirectoryService(pool *pgxpool.Pool) *DirectoryService {
	return &DirectoryService{pool: pool}
}

func (s *DirectoryService) ListRecipients(ctx context.Context, requesterID uuid.UUID, query string, limit int) ([]Recipient, error) {
	query = strings.TrimSpace(query)
	if limit == 0 {
		limit = DefaultRecipientLimit
	}
	if limit < 1 || limit > MaxRecipientLimit || utf8.RuneCountInString(query) > MaxRecipientQueryRunes {
		return nil, ErrInvalidList
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id,u.username,u.display_name
		FROM users u
		LEFT JOIN sessions s ON s.user_id=u.id
		WHERE u.status='ACTIVE'
		  AND u.id<>$1
		  AND ($2='' OR strpos(lower(u.username),lower($2))>0 OR strpos(lower(u.display_name),lower($2))>0)
		GROUP BY u.id,u.username,u.display_name,u.created_at
		ORDER BY max(s.last_seen_at) DESC NULLS LAST,u.created_at DESC,u.id DESC
		LIMIT $3`, requesterID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recipients := make([]Recipient, 0, limit)
	for rows.Next() {
		var recipient Recipient
		if err = rows.Scan(&recipient.ID, &recipient.Username, &recipient.DisplayName); err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}
