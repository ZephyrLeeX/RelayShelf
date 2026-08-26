package search

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgreSQLRepository struct{ pool *pgxpool.Pool }

func NewPostgreSQLRepository(pool *pgxpool.Pool) *PostgreSQLRepository {
	return &PostgreSQLRepository{pool: pool}
}

const resultSelect = `
SELECT m.id, m.owner_id, m.body_plaintext, m.body_format, m.detected_type,
       m.detected_language, m.sensitive, m.lifecycle, m.is_favorite,
       m.expires_at, m.trashed_at, m.purge_at, m.source_user_id,
       m.source_message_id, m.version, m.created_at, m.updated_at
FROM messages m
WHERE m.owner_id = $1 AND m.trashed_at IS NULL
  AND (m.lifecycle = 'PERMANENT' OR m.expires_at > $2)`

func buildQuery(ownerID uuid.UUID, query Query, now time.Time) (string, []any) {
	args := []any{ownerID, now}
	var sql strings.Builder
	parameter := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	tokens := append([]string(nil), query.Tokens...)
	sort.SliceStable(tokens, func(left, right int) bool {
		return utf8.RuneCountInString(tokens[left]) > utf8.RuneCountInString(tokens[right])
	})
	if len(tokens) > 0 {
		pattern := parameter(likePattern(tokens[0]))
		fmt.Fprintf(&sql, `WITH text_matches AS (
SELECT m.id AS message_id
FROM messages m
WHERE m.owner_id = $1 AND m.trashed_at IS NULL
  AND (m.lifecycle = 'PERMANENT' OR m.expires_at > $2)
  AND m.sensitive = false AND m.body_plaintext IS NOT NULL
  AND m.body_plaintext ILIKE %s ESCAPE '\'
UNION
SELECT m.id
FROM message_attachments attachment
JOIN messages m ON m.id = attachment.message_id
WHERE m.owner_id = $1 AND m.trashed_at IS NULL
  AND (m.lifecycle = 'PERMANENT' OR m.expires_at > $2)
  AND attachment.original_filename ILIKE %s ESCAPE '\'
UNION
SELECT m.id
FROM tags tag
JOIN message_tags relation ON relation.tag_id = tag.id
JOIN messages m ON m.id = relation.message_id
WHERE tag.user_id = $1 AND m.owner_id = $1 AND m.trashed_at IS NULL
  AND (m.lifecycle = 'PERMANENT' OR m.expires_at > $2)
  AND tag.normalized_name ILIKE %s ESCAPE '\'
)
`, pattern, pattern, pattern)
	}
	sql.WriteString(resultSelect)
	if len(tokens) > 0 {
		sql.WriteString(" AND EXISTS (SELECT 1 FROM text_matches matched_message WHERE matched_message.message_id = m.id)")
	}
	remainingTokens := []string(nil)
	if len(tokens) > 1 {
		remainingTokens = tokens[1:]
	}
	for _, token := range remainingTokens {
		pattern := parameter(likePattern(token))
		sql.WriteString(` AND (
    (m.sensitive = false AND m.body_plaintext IS NOT NULL AND m.body_plaintext ILIKE `)
		sql.WriteString(pattern)
		sql.WriteString(` ESCAPE '\')
    OR EXISTS (
        SELECT 1 FROM message_attachments attachment
        WHERE attachment.message_id = m.id AND attachment.original_filename ILIKE `)
		sql.WriteString(pattern)
		sql.WriteString(` ESCAPE '\'
    )
    OR EXISTS (
        SELECT 1 FROM message_tags relation
        JOIN tags tag ON tag.id = relation.tag_id
        WHERE relation.message_id = m.id AND tag.user_id = $1 AND tag.normalized_name ILIKE `)
		sql.WriteString(pattern)
		sql.WriteString(` ESCAPE '\'
    )
)`)
	}
	if query.Lifecycle != nil {
		sql.WriteString(" AND m.lifecycle = ")
		sql.WriteString(parameter(*query.Lifecycle))
	}
	if query.Favorite != nil {
		sql.WriteString(" AND m.is_favorite = ")
		sql.WriteString(parameter(*query.Favorite))
	}
	if len(query.TagIDs) > 0 {
		placeholder := parameter(query.TagIDs)
		sql.WriteString(` AND NOT EXISTS (
    SELECT 1 FROM unnest(`)
		sql.WriteString(placeholder)
		sql.WriteString(`::uuid[]) requested(tag_id)
    WHERE NOT EXISTS (
        SELECT 1 FROM message_tags relation
        JOIN tags tag ON tag.id = relation.tag_id
        WHERE relation.message_id = m.id AND tag.user_id = $1 AND tag.id = requested.tag_id
    )
)`)
	}
	if query.DetectedType != nil {
		sql.WriteString(" AND lower(m.detected_type) = ")
		sql.WriteString(parameter(*query.DetectedType))
	}
	if query.CreatedAfter != nil {
		sql.WriteString(" AND m.created_at >= ")
		sql.WriteString(parameter(query.CreatedAfter.UTC()))
	}
	if query.CreatedBefore != nil {
		sql.WriteString(" AND m.created_at < ")
		sql.WriteString(parameter(query.CreatedBefore.UTC()))
	}
	if query.Cursor != nil {
		createdAt := parameter(query.Cursor.CreatedAt.UTC())
		messageID := parameter(query.Cursor.MessageID)
		sql.WriteString(" AND (m.created_at, m.id) < (")
		sql.WriteString(createdAt)
		sql.WriteString(", ")
		sql.WriteString(messageID)
		sql.WriteByte(')')
	}
	sql.WriteString(" ORDER BY m.created_at DESC, m.id DESC LIMIT ")
	sql.WriteString(parameter(query.Limit + 1))
	return sql.String(), args
}

func (r *PostgreSQLRepository) Search(ctx context.Context, ownerID uuid.UUID, query Query, now time.Time) ([]messages.Message, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	sql, args := buildQuery(ownerID, query, now)
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	result := make([]messages.Message, 0, query.Limit+1)
	for rows.Next() {
		message, scanErr := scanMessage(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		result = append(result, message)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	ids := make([]uuid.UUID, 0, len(result))
	byID := make(map[uuid.UUID]*messages.Message, len(result))
	for index := range result {
		ids = append(ids, result[index].ID)
		byID[result[index].ID] = &result[index]
	}
	if err = hydrateTags(ctx, tx, ownerID, ids, byID); err != nil {
		return nil, err
	}
	if err = hydrateAttachments(ctx, tx, ownerID, ids, byID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanMessage(row rowScanner) (messages.Message, error) {
	var message messages.Message
	var body, detectedType, detectedLanguage pgtype.Text
	var expiresAt, trashedAt, purgeAt pgtype.Timestamptz
	var sourceUserID, sourceMessageID pgtype.UUID
	err := row.Scan(&message.ID, &message.OwnerID, &body, &message.BodyFormat, &detectedType,
		&detectedLanguage, &message.Sensitive, &message.Lifecycle, &message.Favorite,
		&expiresAt, &trashedAt, &purgeAt, &sourceUserID, &sourceMessageID,
		&message.Version, &message.CreatedAt, &message.UpdatedAt)
	if err != nil {
		return messages.Message{}, err
	}
	if body.Valid {
		message.BodyPlaintext = &body.String
	}
	if detectedType.Valid {
		message.DetectedType = &detectedType.String
	}
	if detectedLanguage.Valid {
		message.DetectedLanguage = &detectedLanguage.String
	}
	if expiresAt.Valid {
		message.ExpiresAt = &expiresAt.Time
	}
	if trashedAt.Valid {
		message.TrashedAt = &trashedAt.Time
	}
	if purgeAt.Valid {
		message.PurgeAt = &purgeAt.Time
	}
	if sourceUserID.Valid {
		value := uuid.UUID(sourceUserID.Bytes)
		message.SourceUserID = &value
	}
	if sourceMessageID.Valid {
		value := uuid.UUID(sourceMessageID.Bytes)
		message.SourceMessageID = &value
	}
	message.Tags = []messages.Tag{}
	message.Attachments = []messages.Attachment{}
	return message, nil
}

func hydrateTags(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, ids []uuid.UUID, byID map[uuid.UUID]*messages.Message) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT relation.message_id, tag.id, tag.name, tag.color, tag.created_at, tag.updated_at
FROM message_tags relation
JOIN tags tag ON tag.id = relation.tag_id AND tag.user_id = $1
JOIN messages m ON m.id = relation.message_id AND m.owner_id = $1
WHERE relation.message_id = ANY($2::uuid[])
ORDER BY relation.message_id, tag.normalized_name, tag.id`, ownerID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID uuid.UUID
		var tag messages.Tag
		if err = rows.Scan(&messageID, &tag.ID, &tag.Name, &tag.Color, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return err
		}
		if message := byID[messageID]; message != nil {
			message.Tags = append(message.Tags, tag)
		}
	}
	return rows.Err()
}

func hydrateAttachments(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, ids []uuid.UUID, byID map[uuid.UUID]*messages.Message) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `WITH ranked AS (
    SELECT attachment.message_id, attachment.id, attachment.original_filename, attachment.client_mime,
           object.detected_mime, object.size_bytes, attachment.display_order, attachment.file_object_id,
           row_number() OVER (PARTITION BY attachment.message_id ORDER BY attachment.display_order, attachment.id) AS ordinal,
           count(*) OVER (PARTITION BY attachment.message_id) AS total
    FROM message_attachments attachment
    JOIN file_objects object ON object.id = attachment.file_object_id
    JOIN messages m ON m.id = attachment.message_id AND m.owner_id = $1
    WHERE attachment.message_id = ANY($2::uuid[])
)
SELECT message_id, id, original_filename, client_mime, detected_mime, size_bytes,
       display_order, file_object_id, total
FROM ranked WHERE ordinal <= 3
ORDER BY message_id, display_order, id`, ownerID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID uuid.UUID
		var attachment messages.Attachment
		var clientMime pgtype.Text
		var total int
		if err = rows.Scan(&messageID, &attachment.ID, &attachment.OriginalFilename, &clientMime,
			&attachment.DetectedMime, &attachment.SizeBytes, &attachment.DisplayOrder,
			&attachment.FileObjectID, &total); err != nil {
			return err
		}
		if clientMime.Valid {
			attachment.ClientMime = &clientMime.String
		}
		if message := byID[messageID]; message != nil {
			message.AttachmentTotal = total
			message.Attachments = append(message.Attachments, attachment)
		}
	}
	return rows.Err()
}
