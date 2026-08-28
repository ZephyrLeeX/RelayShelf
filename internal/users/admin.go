package users

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxDisplayNameRunes = 100

type AdminService struct {
	pool     *pgxpool.Pool
	hash     PasswordHasher
	ids      id.Generator
	clock    Clock
	recorder *audit.Recorder
}

func NewAdminService(pool *pgxpool.Pool, hash PasswordHasher, ids id.Generator, clock Clock, recorder *audit.Recorder) *AdminService {
	return &AdminService{pool: pool, hash: hash, ids: ids, clock: clock, recorder: recorder}
}

func (s *AdminService) List(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,username,display_name,password_hash,is_admin,status,created_at,updated_at FROM users ORDER BY created_at,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		var user User
		if err = rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.PasswordHash, &user.IsAdmin, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		user.PasswordHash = ""
		out = append(out, user)
	}
	return out, rows.Err()
}

func (s *AdminService) Create(ctx context.Context, actor audit.Actor, username, displayName, password string, isAdmin bool) (User, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if utf8.RuneCountInString(displayName) < 1 || utf8.RuneCountInString(displayName) > MaxDisplayNameRunes {
		return User{}, ErrInvalidUsername
	}
	if err = ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := s.hash.Hash(password)
	if err != nil {
		return User{}, err
	}
	userID, err := s.ids.New()
	if err != nil {
		return User{}, err
	}
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var user User
	err = tx.QueryRow(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'ACTIVE',$6,$6) RETURNING id,username,display_name,password_hash,is_admin,status,created_at,updated_at`, userID, normalized, displayName, hash, isAdmin, now).Scan(&user.ID, &user.Username, &user.DisplayName, &user.PasswordHash, &user.IsAdmin, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, adminTranslate(err)
	}
	if err = s.recorder.Record(ctx, tx, audit.UserCreated(actor, user.ID, user.Username, user.IsAdmin)); err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	user.PasswordHash = ""
	return user, nil
}

func (s *AdminService) Disable(ctx context.Context, actor audit.Actor, userID uuid.UUID) error {
	return s.mutate(ctx, func(tx pgx.Tx, now time.Time) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET status='DISABLED',updated_at=$2 WHERE id=$1`, userID, now)
		if err != nil || tag.RowsAffected() == 0 {
			if err == nil {
				return ErrNotFound
			}
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE user_id=$1`, userID, now); err != nil {
			return err
		}
		return s.recorder.Record(ctx, tx, audit.UserDisabled(actor, userID))
	})
}

func (s *AdminService) Delete(ctx context.Context, actor audit.Actor, userID uuid.UUID) error {
	return s.mutate(ctx, func(tx pgx.Tx, now time.Time) error {
		var username string
		if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&username); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE user_id=$1`, userID, now); err != nil {
			return err
		}
		// Audit first: deleting the actor causes the existing FK to safely SET NULL.
		if err := s.recorder.Record(ctx, tx, audit.UserDeleted(actor, userID, username)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// File objects are intentionally untouched. Cascading message attachment
		// deletion only removes this user's refs; the existing orphan GC remains
		// the sole physical-deletion authority.
		return nil
	})
}

func (s *AdminService) mutate(ctx context.Context, fn func(pgx.Tx, time.Time) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(tx, s.clock.Now()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func adminTranslate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrUsernameTaken
	}
	return err
}
