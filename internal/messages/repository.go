package messages

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/sql/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func pgu(v uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: v, Valid: true} }
func pgup(v *uuid.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return pgu(*v)
}
func pgt(v time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: v, Valid: true} }
func pgtp(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgt(*v)
}
func pgs(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}
func pg2(v *int16) pgtype.Int2 {
	if v == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: *v, Valid: true}
}
func uuidp(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	out := uuid.UUID(v.Bytes)
	return &out
}
func timep(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	out := v.Time
	return &out
}
func stringp(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	out := v.String
	return &out
}
func int2p(v pgtype.Int2) *int16 {
	if !v.Valid {
		return nil
	}
	out := v.Int16
	return &out
}

func domainMessage(r generated.Message) Message {
	return Message{ID: uuid.UUID(r.ID.Bytes), OwnerID: uuid.UUID(r.OwnerID.Bytes), BodyPlaintext: stringp(r.BodyPlaintext), BodyCiphertext: r.BodyCiphertext, BodyNonce: r.BodyNonce, BodyEncryptionVersion: int2p(r.BodyEncryptionVersion), BodyFormat: r.BodyFormat, DetectedType: stringp(r.DetectedType), DetectedLanguage: stringp(r.DetectedLanguage), Sensitive: r.Sensitive, Lifecycle: r.Lifecycle, Favorite: r.IsFavorite, ExpiresAt: timep(r.ExpiresAt), TrashedAt: timep(r.TrashedAt), PurgeAt: timep(r.PurgeAt), SourceUserID: uuidp(r.SourceUserID), SourceMessageID: uuidp(r.SourceMessageID), CreatedDeviceID: uuidp(r.CreatedDeviceID), Version: r.Version, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}
}
func domainTag(r generated.Tag) Tag {
	return Tag{ID: uuid.UUID(r.ID.Bytes), Name: r.Name, Color: r.Color, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}
}

func (r *PostgreSQLRepository) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func loadOwned(ctx context.Context, tx pgx.Tx, ownerID, id uuid.UUID, lock bool) (Message, error) {
	q := generated.New(tx)
	var row generated.Message
	var err error
	if lock {
		row, err = q.LockOwnedMessage(ctx, generated.LockOwnedMessageParams{ID: pgu(id), OwnerID: pgu(ownerID)})
	} else {
		row, err = q.GetOwnedMessage(ctx, generated.GetOwnedMessageParams{ID: pgu(id), OwnerID: pgu(ownerID)})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return domainMessage(row), err
}
func loadTags(ctx context.Context, q *generated.Queries, id uuid.UUID) ([]Tag, error) {
	rows, err := q.ListMessageTags(ctx, pgu(id))
	if err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(rows))
	for _, row := range rows {
		out = append(out, domainTag(row))
	}
	return out, nil
}
func (r *PostgreSQLRepository) Get(ctx context.Context, ownerID, id uuid.UUID) (Message, error) {
	row, err := r.queries.GetOwnedMessage(ctx, generated.GetOwnedMessageParams{ID: pgu(id), OwnerID: pgu(ownerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	m := domainMessage(row)
	m.Tags, err = loadTags(ctx, r.queries, id)
	if err == nil {
		m.Attachments, err = r.loadAttachments(ctx, r.pool, id, 0)
		m.AttachmentTotal = len(m.Attachments)
	}
	return m, err
}

func insertParams(m Message) generated.InsertMessageParams {
	return generated.InsertMessageParams{ID: pgu(m.ID), OwnerID: pgu(m.OwnerID), BodyPlaintext: pgs(m.BodyPlaintext), BodyCiphertext: m.BodyCiphertext, BodyNonce: m.BodyNonce, BodyEncryptionVersion: pg2(m.BodyEncryptionVersion), BodyFormat: m.BodyFormat, DetectedType: pgs(m.DetectedType), DetectedLanguage: pgs(m.DetectedLanguage), Sensitive: m.Sensitive, Lifecycle: m.Lifecycle, IsFavorite: m.Favorite, ExpiresAt: pgtp(m.ExpiresAt), TrashedAt: pgtp(m.TrashedAt), PurgeAt: pgtp(m.PurgeAt), SourceUserID: pgup(m.SourceUserID), SourceMessageID: pgup(m.SourceMessageID), CreatedDeviceID: pgup(m.CreatedDeviceID), Version: m.Version, CreatedAt: pgt(m.CreatedAt), UpdatedAt: pgt(m.UpdatedAt)}
}
func insertMessage(ctx context.Context, tx pgx.Tx, m Message) error {
	return generated.New(tx).InsertMessage(ctx, insertParams(m))
}
func saveMessage(ctx context.Context, tx pgx.Tx, m Message) error {
	p := insertParams(m)
	rows, err := generated.New(tx).SaveMessage(ctx, generated.SaveMessageParams{ID: p.ID, OwnerID: p.OwnerID, BodyPlaintext: p.BodyPlaintext, BodyCiphertext: p.BodyCiphertext, BodyNonce: p.BodyNonce, BodyEncryptionVersion: p.BodyEncryptionVersion, BodyFormat: p.BodyFormat, DetectedType: p.DetectedType, DetectedLanguage: p.DetectedLanguage, Sensitive: p.Sensitive, Lifecycle: p.Lifecycle, IsFavorite: p.IsFavorite, ExpiresAt: p.ExpiresAt, TrashedAt: p.TrashedAt, PurgeAt: p.PurgeAt, Version: p.Version, UpdatedAt: p.UpdatedAt})
	if err == nil && rows != 1 {
		return ErrNotFound
	}
	return err
}
func settings(ctx context.Context, tx pgx.Tx) (time.Duration, time.Duration, error) {
	row, err := generated.New(tx).GetMessageSettings(ctx)
	return time.Duration(row.TemporaryTtlHours) * time.Hour, time.Duration(row.TrashTtlHours) * time.Hour, err
}

func uuidArray(ids []uuid.UUID) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		out = append(out, pgu(id))
	}
	return out
}
func validateTags(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, ids []uuid.UUID) error {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return ErrValidation
		}
		unique[id] = struct{}{}
	}
	if len(unique) != len(ids) {
		return ErrValidation
	}
	if len(ids) == 0 {
		return nil
	}
	count, err := generated.New(tx).CountOwnedTags(ctx, generated.CountOwnedTagsParams{UserID: pgu(ownerID), Column2: uuidArray(ids)})
	if err != nil {
		return err
	}
	if count != int64(len(ids)) {
		return ErrValidation
	}
	return nil
}
func replaceTags(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, ids []uuid.UUID) error {
	q := generated.New(tx)
	if err := q.DeleteMessageTags(ctx, pgu(messageID)); err != nil {
		return err
	}
	for _, tagID := range ids {
		if err := q.InsertMessageTag(ctx, generated.InsertMessageTagParams{MessageID: pgu(messageID), TagID: pgu(tagID)}); err != nil {
			return err
		}
	}
	return nil
}

type idemResult struct {
	Found            bool
	ResourceID       uuid.UUID
	ResponseMetadata []byte
}

func claimIdempotency(ctx context.Context, tx pgx.Tx, userID uuid.UUID, operation, key string, hash [32]byte, now time.Time) (idemResult, error) {
	q := generated.New(tx)
	lockKey := userID.String() + "|" + operation + "|" + base64.RawURLEncoding.EncodeToString([]byte(key))
	if err := q.LockIdempotencyClaim(ctx, lockKey); err != nil {
		return idemResult{}, err
	}
	params := generated.GetIdempotencyClaimParams{UserID: pgu(userID), Operation: operation, Key: key}
	row, err := q.GetIdempotencyClaim(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return idemResult{}, nil
	}
	if err != nil {
		return idemResult{}, err
	}
	if !row.ExpiresAt.Time.After(now) {
		if err = q.DeleteIdempotencyClaim(ctx, generated.DeleteIdempotencyClaimParams(params)); err != nil {
			return idemResult{}, err
		}
		return idemResult{}, nil
	}
	if !bytes.Equal(row.RequestHash, hash[:]) {
		return idemResult{}, ErrIdempotencyKeyReused
	}
	if !row.ResourceID.Valid {
		return idemResult{}, errors.New("idempotency result missing resource")
	}
	return idemResult{Found: true, ResourceID: uuid.UUID(row.ResourceID.Bytes), ResponseMetadata: row.ResponseMetadata}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, id, userID uuid.UUID, operation, key string, hash [32]byte, resourceID uuid.UUID, metadata []byte, now time.Time) error {
	return generated.New(tx).InsertIdempotencyResult(ctx, generated.InsertIdempotencyResultParams{ID: pgu(id), UserID: pgu(userID), Operation: operation, Key: key, RequestHash: hash[:], ResourceID: pgu(resourceID), ResponseMetadata: metadata, CreatedAt: pgt(now), ExpiresAt: pgt(now.Add(24 * time.Hour))})
}

func resourceMetadata(resourceID uuid.UUID, now time.Time) []byte {
	metadata, _ := json.Marshal(map[string]string{"messageId": resourceID.String(), "createdAt": now.UTC().Format(time.RFC3339Nano)})
	return metadata
}

type deliveryIdempotencyMetadata struct {
	MessageDeliveryReceipt
	Version int64 `json:"version"`
}

func deliveryMetadata(receipt MessageDeliveryReceipt, version int64) ([]byte, error) {
	return json.Marshal(deliveryIdempotencyMetadata{MessageDeliveryReceipt: receipt, Version: version})
}

func deliveryResult(idem idemResult) (MessageDeliveryReceipt, int64, error) {
	var metadata deliveryIdempotencyMetadata
	if err := json.Unmarshal(idem.ResponseMetadata, &metadata); err != nil || metadata.MessageID == uuid.Nil || metadata.CreatedAt.IsZero() || metadata.ExpiresAt.IsZero() || metadata.MessageID != idem.ResourceID {
		return MessageDeliveryReceipt{}, 0, errors.New("invalid idempotency delivery receipt")
	}
	// Phase 7 idempotency rows written before committed versions were added
	// represent newly-created messages, whose authoritative initial version is 1.
	if metadata.Version == 0 {
		metadata.Version = 1
	}
	return metadata.MessageDeliveryReceipt, metadata.Version, nil
}

func (r *PostgreSQLRepository) List(ctx context.Context, ownerID uuid.UUID, filter ListFilter, trash bool, now time.Time) ([]Message, error) {
	var rows []generated.Message
	var err error
	cursorAt := pgtype.Timestamptz{}
	cursorID := pgtype.UUID{}
	if filter.Cursor != nil {
		cursorAt = pgt(filter.Cursor.At)
		cursorID = pgu(filter.Cursor.ID)
	}
	if trash {
		rows, err = r.queries.ListTrashedMessages(ctx, generated.ListTrashedMessagesParams{OwnerID: pgu(ownerID), CursorAt: cursorAt, CursorID: cursorID, RowLimit: int32(filter.Limit + 1)})
	} else {
		lifecycle := pgtype.Text{}
		if filter.Lifecycle != nil {
			lifecycle = pgtype.Text{String: *filter.Lifecycle, Valid: true}
		}
		favorite := pgtype.Bool{}
		if filter.Favorite != nil {
			favorite = pgtype.Bool{Bool: *filter.Favorite, Valid: true}
		}
		rows, err = r.queries.ListActiveMessages(ctx, generated.ListActiveMessagesParams{OwnerID: pgu(ownerID), NowAt: pgt(now), Lifecycle: lifecycle, Favorite: favorite, TagIds: uuidArray(filter.TagIDs), CursorAt: cursorAt, CursorID: cursorID, RowLimit: int32(filter.Limit + 1)})
	}
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		m := domainMessage(row)
		m.Tags, err = loadTags(ctx, r.queries, m.ID)
		if err != nil {
			return nil, err
		}
		m.Attachments, err = r.loadAttachments(ctx, r.pool, m.ID, 3)
		if err != nil {
			return nil, err
		}
		if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM message_attachments WHERE message_id=$1`, m.ID).Scan(&m.AttachmentTotal); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
func sameUUIDSet(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[uuid.UUID]int{}
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
		if m[v] < 0 {
			return false
		}
	}
	return true
}
func currentTagIDs(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := generated.New(tx).ListCurrentMessageTagIDs(ctx, pgu(messageID))
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, uuid.UUID(row.Bytes))
	}
	return out, nil
}
