package tags

import (
	"context"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgreSQLRepository struct {
	pool    *pgxpool.Pool
	queries *generated.Queries
}

func NewPostgreSQLRepository(pool *pgxpool.Pool) *PostgreSQLRepository {
	return &PostgreSQLRepository{pool: pool, queries: generated.New(pool)}
}
func pgu(v uuid.UUID) pgtype.UUID        { return pgtype.UUID{Bytes: v, Valid: true} }
func pgt(v time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: v, Valid: true} }
func domain(r generated.Tag) Tag {
	return Tag{ID: uuid.UUID(r.ID.Bytes), UserID: uuid.UUID(r.UserID.Bytes), Name: r.Name, NormalizedName: r.NormalizedName, Color: r.Color, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}
}
func duplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
func mapRowError(row generated.Tag, err error) (Tag, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	if duplicate(err) {
		return Tag{}, ErrDuplicate
	}
	return domain(row), err
}

func (r *PostgreSQLRepository) List(ctx context.Context, userID uuid.UUID) ([]Tag, error) {
	rows, err := r.queries.ListOwnedTags(ctx, pgu(userID))
	if err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain(row))
	}
	return out, nil
}
func (r *PostgreSQLRepository) Get(ctx context.Context, userID, tagID uuid.UUID) (Tag, error) {
	row, err := r.queries.GetOwnedTag(ctx, generated.GetOwnedTagParams{ID: pgu(tagID), UserID: pgu(userID)})
	return mapRowError(row, err)
}
func (r *PostgreSQLRepository) Create(ctx context.Context, t Tag) (Tag, error) {
	row, err := r.queries.CreateOwnedTag(ctx, generated.CreateOwnedTagParams{ID: pgu(t.ID), UserID: pgu(t.UserID), Name: t.Name, NormalizedName: t.NormalizedName, Color: t.Color, CreatedAt: pgt(t.CreatedAt)})
	return mapRowError(row, err)
}
func (r *PostgreSQLRepository) Update(ctx context.Context, t Tag) (Tag, error) {
	row, err := r.queries.UpdateOwnedTag(ctx, generated.UpdateOwnedTagParams{ID: pgu(t.ID), UserID: pgu(t.UserID), Name: t.Name, NormalizedName: t.NormalizedName, Color: t.Color, UpdatedAt: pgt(t.UpdatedAt)})
	return mapRowError(row, err)
}
func (r *PostgreSQLRepository) Delete(ctx context.Context, userID, tagID uuid.UUID, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := generated.New(tx)
	locked, err := q.LockTagAffectedMessages(ctx, generated.LockTagAffectedMessagesParams{TagID: pgu(tagID), OwnerID: pgu(userID)})
	if err != nil {
		return err
	}
	rows, err := q.DeleteOwnedTag(ctx, generated.DeleteOwnedTagParams{ID: pgu(tagID), UserID: pgu(userID)})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	if len(locked) > 0 {
		if err = q.BumpTagAffectedMessages(ctx, generated.BumpTagAffectedMessagesParams{Column1: locked, UpdatedAt: pgt(now), OwnerID: pgu(userID)}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
