package users

import (
	"context"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgreSQLRepository struct{ q *generated.Queries }

func NewPostgreSQLRepository(db generated.DBTX) *PostgreSQLRepository {
	return &PostgreSQLRepository{q: generated.New(db)}
}

func pgUUID(value uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: value, Valid: true} }
func parseUUID(value string) (pgtype.UUID, error) {
	u, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, ErrNotFound
	}
	return pgUUID(u), nil
}
func fromPG(row generated.User) User {
	return User{ID: uuid.UUID(row.ID.Bytes), Username: row.Username, DisplayName: row.DisplayName, PasswordHash: row.PasswordHash, IsAdmin: row.IsAdmin, Status: Status(row.Status), CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrUsernameTaken
	}
	return err
}
func (r *PostgreSQLRepository) Create(ctx context.Context, user User) (User, error) {
	row, err := r.q.CreateUser(ctx, generated.CreateUserParams{ID: pgUUID(user.ID), Username: user.Username, DisplayName: user.DisplayName, PasswordHash: user.PasswordHash, IsAdmin: user.IsAdmin, CreatedAt: pgtype.Timestamptz{Time: user.CreatedAt, Valid: true}})
	return fromPG(row), translate(err)
}
func (r *PostgreSQLRepository) GetByUsername(ctx context.Context, name string) (User, error) {
	row, err := r.q.GetUserByUsername(ctx, name)
	return fromPG(row), translate(err)
}
func (r *PostgreSQLRepository) GetByID(ctx context.Context, value string) (User, error) {
	id, err := parseUUID(value)
	if err != nil {
		return User{}, err
	}
	row, err := r.q.GetUserByID(ctx, id)
	return fromPG(row), translate(err)
}
func (r *PostgreSQLRepository) Disable(ctx context.Context, value string, now time.Time) error {
	id, err := parseUUID(value)
	if err != nil {
		return err
	}
	n, err := r.q.DisableUser(ctx, generated.DisableUserParams{ID: id, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *PostgreSQLRepository) Delete(ctx context.Context, value string) error {
	id, err := parseUUID(value)
	if err != nil {
		return err
	}
	n, err := r.q.DeleteUser(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
