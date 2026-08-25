package uploads

import (
	"context"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CreateInserter interface {
	Insert(context.Context, Session) error
}

type Repository interface {
	WithCreateReservation(context.Context, func(context.Context, Reservation, CreateInserter) error) error
	Get(context.Context, uuid.UUID, uuid.UUID) (Session, error)
	ListParts(context.Context, uuid.UUID) ([]Part, error)
	InvalidatePart(context.Context, uuid.UUID, int) error
	CommitPart(context.Context, uuid.UUID, uuid.UUID, int, int64, time.Time) error
	Complete(context.Context, uuid.UUID, uuid.UUID, time.Time, func(Session, []Part) error) (Session, error)
	FindDueActiveUploads(context.Context, time.Time, int32) ([]uuid.UUID, error)
	FindExpiredCleanupCandidates(context.Context) ([]uuid.UUID, error)
	MarkExpired(context.Context, uuid.UUID, time.Time) (Session, bool, error)
	DeleteParts(context.Context, uuid.UUID) error
	ActiveUploadIDs(context.Context, []uuid.UUID) (map[uuid.UUID]struct{}, error)
}

type FailureHooks struct {
	BeforeCreateCommit       func() error
	BeforePartMarker         func() error
	BeforeCompleteTransition func() error
}

type PostgreSQLRepository struct {
	pool  *pgxpool.Pool
	hooks FailureHooks
}

func NewPostgreSQLRepository(pool *pgxpool.Pool) *PostgreSQLRepository {
	return &PostgreSQLRepository{pool: pool}
}

func NewPostgreSQLRepositoryWithFailureHooks(pool *pgxpool.Pool, hooks FailureHooks) *PostgreSQLRepository {
	return &PostgreSQLRepository{pool: pool, hooks: hooks}
}

func pgUUID(v uuid.UUID) pgtype.UUID        { return pgtype.UUID{Bytes: v, Valid: true} }
func pgTime(v time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: v, Valid: true} }
func pgText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}
func textPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
func domainSession(row generated.UploadSession) Session {
	var fileID *uuid.UUID
	if row.FileObjectID.Valid {
		value := uuid.UUID(row.FileObjectID.Bytes)
		fileID = &value
	}
	var completedAt *time.Time
	if row.CompletedAt.Valid {
		value := row.CompletedAt.Time
		completedAt = &value
	}
	return Session{ID: uuid.UUID(row.ID.Bytes), UserID: uuid.UUID(row.UserID.Bytes), OriginalFilename: row.OriginalFilename, ExpectedSize: row.ExpectedSize, ClientMime: textPtr(row.ClientMime), ChunkSize: row.ChunkSize, Status: row.Status, ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time, FileObjectID: fileID, CompletedAt: completedAt}
}
func domainParts(rows []generated.UploadPart) []Part {
	out := make([]Part, 0, len(rows))
	for _, row := range rows {
		out = append(out, Part{Number: int(row.PartNumber), SizeBytes: row.SizeBytes, CompletedAt: row.CompletedAt.Time})
	}
	return out
}

type txInserter struct{ q *generated.Queries }

func (i txInserter) Insert(ctx context.Context, s Session) error {
	_, err := i.q.CreateUploadSession(ctx, generated.CreateUploadSessionParams{ID: pgUUID(s.ID), UserID: pgUUID(s.UserID), OriginalFilename: s.OriginalFilename, ExpectedSize: s.ExpectedSize, ClientMime: pgText(s.ClientMime), ChunkSize: s.ChunkSize, ExpiresAt: pgTime(s.ExpiresAt), CreatedAt: pgTime(s.CreatedAt)})
	return err
}

func (r *PostgreSQLRepository) WithCreateReservation(ctx context.Context, fn func(context.Context, Reservation, CreateInserter) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := generated.New(tx)
	if err = q.LockUploadReservation(ctx); err != nil {
		return err
	}
	settings, err := q.GetUploadSettings(ctx)
	if err != nil {
		return err
	}
	active, err := q.ActiveUploadReservation(ctx)
	if err != nil {
		return err
	}
	remaining, err := q.ActiveUploadRemaining(ctx)
	if err != nil {
		return err
	}
	reservation := Reservation{Settings: Settings{MaxFileSizeBytes: settings.MaxFileSizeBytes, UploadRetentionHours: settings.UploadRetentionHours}, ActiveBytes: active, ActiveRemaining: remaining}
	if err = fn(ctx, reservation, txInserter{q}); err != nil {
		return err
	}
	if r.hooks.BeforeCreateCommit != nil {
		if err = r.hooks.BeforeCreateCommit(); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PostgreSQLRepository) Get(ctx context.Context, ownerID, id uuid.UUID) (Session, error) {
	row, err := generated.New(r.pool).GetOwnedUploadSession(ctx, generated.GetOwnedUploadSessionParams{ID: pgUUID(id), UserID: pgUUID(ownerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return domainSession(row), err
}

func (r *PostgreSQLRepository) ListParts(ctx context.Context, id uuid.UUID) ([]Part, error) {
	rows, err := generated.New(r.pool).ListCompletedParts(ctx, pgUUID(id))
	return domainParts(rows), err
}

func (r *PostgreSQLRepository) InvalidatePart(ctx context.Context, id uuid.UUID, part int) error {
	return generated.New(r.pool).InvalidateUploadPart(ctx, generated.InvalidateUploadPartParams{UploadSessionID: pgUUID(id), PartNumber: int32(part)})
}

func (r *PostgreSQLRepository) CommitPart(ctx context.Context, ownerID, id uuid.UUID, part int, size int64, now time.Time) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := generated.New(tx)
	row, err := q.LockOwnedUploadSession(ctx, generated.LockOwnedUploadSessionParams{ID: pgUUID(id), UserID: pgUUID(ownerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	s := domainSession(row)
	if !s.ExpiresAt.After(now) || s.Status == Expired {
		return ErrExpired
	}
	if s.Status != Created && s.Status != Uploading {
		return ErrInvalidState
	}
	if r.hooks.BeforePartMarker != nil {
		if err = r.hooks.BeforePartMarker(); err != nil {
			return err
		}
	}
	if err = q.UpsertCompletedPart(ctx, generated.UpsertCompletedPartParams{UploadSessionID: pgUUID(id), PartNumber: int32(part), SizeBytes: size, CompletedAt: pgTime(now)}); err != nil {
		return err
	}
	if _, err = q.TransitionUploadToUploading(ctx, generated.TransitionUploadToUploadingParams{ID: pgUUID(id), UserID: pgUUID(ownerID), UpdatedAt: pgTime(now)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgreSQLRepository) Complete(ctx context.Context, ownerID, id uuid.UUID, now time.Time, validate func(Session, []Part) error) (Session, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Session{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := generated.New(tx)
	row, err := q.LockOwnedUploadSession(ctx, generated.LockOwnedUploadSessionParams{ID: pgUUID(id), UserID: pgUUID(ownerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	s := domainSession(row)
	if s.Status == Expired || (!s.ExpiresAt.After(now) && s.Status != Completing && s.Status != Completed) {
		return Session{}, ErrExpired
	}
	if s.Status == Completing || s.Status == Completed {
		if err = tx.Commit(ctx); err != nil {
			return Session{}, err
		}
		return s, nil
	}
	if s.Status != Created && s.Status != Uploading {
		return Session{}, ErrInvalidState
	}
	rows, err := q.ListCompletedParts(ctx, pgUUID(id))
	if err != nil {
		return Session{}, err
	}
	parts := domainParts(rows)
	if err = validate(s, parts); err != nil {
		return Session{}, err
	}
	if r.hooks.BeforeCompleteTransition != nil {
		if err = r.hooks.BeforeCompleteTransition(); err != nil {
			return Session{}, err
		}
	}
	changed, err := q.TransitionUploadToCompleting(ctx, generated.TransitionUploadToCompletingParams{ID: pgUUID(id), UserID: pgUUID(ownerID), UpdatedAt: pgTime(now)})
	if err != nil {
		return Session{}, err
	}
	if changed != 1 {
		return Session{}, ErrInvalidState
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	s.Status, s.UpdatedAt = Completing, now
	return s, nil
}

func (r *PostgreSQLRepository) FindDueActiveUploads(ctx context.Context, now time.Time, batch int32) ([]uuid.UUID, error) {
	rows, err := generated.New(r.pool).FindDueActiveUploads(ctx, generated.FindDueActiveUploadsParams{ExpiresAt: pgTime(now), Limit: batch})
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, uuid.UUID(row.ID.Bytes))
	}
	return out, nil
}

func (r *PostgreSQLRepository) FindExpiredCleanupCandidates(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := generated.New(r.pool).FindExpiredCleanupCandidates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, uuid.UUID(row.Bytes))
	}
	return out, nil
}

func (r *PostgreSQLRepository) MarkExpired(ctx context.Context, id uuid.UUID, now time.Time) (Session, bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Session{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := generated.New(tx)
	row, err := q.LockUploadSessionForMaintenance(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	s := domainSession(row)
	if s.Status == Expired {
		if err = tx.Commit(ctx); err != nil {
			return Session{}, false, err
		}
		return s, true, nil
	}
	if (s.Status != Created && s.Status != Uploading && s.Status != Failed) || s.ExpiresAt.After(now) {
		return s, false, tx.Commit(ctx)
	}
	changed, err := q.MarkUploadExpired(ctx, generated.MarkUploadExpiredParams{ID: pgUUID(id), UpdatedAt: pgTime(now)})
	if err != nil {
		return Session{}, false, err
	}
	if changed != 1 {
		return Session{}, false, ErrInvalidState
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, false, err
	}
	s.Status, s.UpdatedAt = Expired, now
	return s, true, nil
}

func (r *PostgreSQLRepository) DeleteParts(ctx context.Context, id uuid.UUID) error {
	return generated.New(r.pool).DeleteUploadParts(ctx, pgUUID(id))
}

func (r *PostgreSQLRepository) ActiveUploadIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	active := make(map[uuid.UUID]struct{}, len(ids))
	if len(ids) == 0 {
		return active, nil
	}
	rows, err := generated.New(r.pool).GetActiveUploadIDs(ctx, uuidArray(ids))
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		active[uuid.UUID(row.Bytes)] = struct{}{}
	}
	return active, nil
}

func uuidArray(ids []uuid.UUID) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		out = append(out, pgUUID(id))
	}
	return out
}
