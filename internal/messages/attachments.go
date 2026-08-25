package messages

import (
	"bytes"
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type attachmentQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (r *PostgreSQLRepository) loadAttachments(ctx context.Context, q attachmentQuerier, messageID uuid.UUID, limit int) ([]Attachment, error) {
	query := `SELECT ma.id,ma.original_filename,ma.client_mime,fo.detected_mime,fo.size_bytes,ma.display_order,ma.file_object_id FROM message_attachments ma JOIN file_objects fo ON fo.id=ma.file_object_id WHERE ma.message_id=$1 ORDER BY ma.display_order,ma.id`
	args := []any{messageID}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err = rows.Scan(&a.ID, &a.OriginalFilename, &a.ClientMime, &a.DetectedMime, &a.SizeBytes, &a.DisplayOrder, &a.FileObjectID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type uploadForBinding struct {
	ID, FileID uuid.UUID
	Filename   string
	ClientMime *string
}

func (s *Service) bindUploads(ctx context.Context, tx pgx.Tx, ownerID, messageID uuid.UUID, uploadIDs []uuid.UUID, startOrder int, now time.Time) error {
	if len(uploadIDs) == 0 {
		return nil
	}
	seen := map[uuid.UUID]struct{}{}
	sorted := append([]uuid.UUID(nil), uploadIDs...)
	for _, id := range uploadIDs {
		if id == uuid.Nil {
			return ErrValidation
		}
		if _, ok := seen[id]; ok {
			return ErrValidation
		}
		seen[id] = struct{}{}
	}
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i][:], sorted[j][:]) < 0 })
	rows, err := tx.Query(ctx, `SELECT us.id,us.file_object_id,us.original_filename,us.client_mime,us.status,us.consumed_at,fo.status FROM upload_sessions us JOIN file_objects fo ON fo.id=us.file_object_id WHERE us.user_id=$1 AND us.id=ANY($2::uuid[]) ORDER BY us.id FOR UPDATE OF us,fo`, ownerID, sorted)
	if err != nil {
		return err
	}
	byID := map[uuid.UUID]uploadForBinding{}
	for rows.Next() {
		var u uploadForBinding
		var status, fileStatus string
		var consumed *time.Time
		if err = rows.Scan(&u.ID, &u.FileID, &u.Filename, &u.ClientMime, &status, &consumed, &fileStatus); err != nil {
			return err
		}
		if consumed != nil {
			return ErrUploadAlreadyConsumed
		}
		if status != "COMPLETED" || fileStatus != "READY" || u.FileID == uuid.Nil {
			return ErrValidation
		}
		byID[u.ID] = u
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(byID) != len(uploadIDs) {
		return ErrValidation
	}
	for order, uploadID := range uploadIDs {
		u := byID[uploadID]
		attachmentID, idErr := s.ids.New()
		if idErr != nil {
			return idErr
		}
		if _, err = tx.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,client_mime,display_order,metadata,created_at) VALUES($1,$2,$3,$4,$5,$6,'{}',$7)`, attachmentID, messageID, u.FileID, u.Filename, u.ClientMime, startOrder+order, now); err != nil {
			return err
		}
		ct, updateErr := tx.Exec(ctx, `UPDATE upload_sessions SET consumed_at=$3,consumed_message_id=$2,updated_at=$3 WHERE id=$1 AND consumed_at IS NULL AND status='COMPLETED'`, uploadID, messageID, now)
		if updateErr != nil {
			return updateErr
		}
		if ct.RowsAffected() != 1 {
			return ErrUploadAlreadyConsumed
		}
	}
	return nil
}

func (s *Service) copyAttachments(ctx context.Context, tx pgx.Tx, sourceID, destinationID uuid.UUID, now time.Time) error {
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM message_attachments WHERE message_id=$1`, sourceID).Scan(&total); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT ma.file_object_id,ma.original_filename,ma.client_mime,ma.display_order FROM message_attachments ma JOIN file_objects fo ON fo.id=ma.file_object_id WHERE ma.message_id=$1 AND fo.status='READY' ORDER BY ma.display_order,ma.id`, sourceID)
	if err != nil {
		return err
	}
	type sourceAttachment struct {
		fileID uuid.UUID
		name   string
		mime   *string
		order  int
	}
	items := []sourceAttachment{}
	for rows.Next() {
		var item sourceAttachment
		if err = rows.Scan(&item.fileID, &item.name, &item.mime, &item.order); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		idValue, idErr := s.ids.New()
		if idErr != nil {
			return idErr
		}
		if _, err = tx.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,client_mime,display_order,metadata,created_at) VALUES($1,$2,$3,$4,$5,$6,'{}',$7)`, idValue, destinationID, item.fileID, item.name, item.mime, item.order, now); err != nil {
			return err
		}
	}
	var copied int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM message_attachments WHERE message_id=$1`, destinationID).Scan(&copied); err != nil {
		return err
	}
	if copied != total {
		return ErrValidation
	}
	return nil
}

func (s *Service) AddAttachments(ctx context.Context, ownerID, messageID uuid.UUID, expected int64, uploadIDs []uuid.UUID) (Message, error) {
	if expected < 1 || len(uploadIDs) == 0 {
		return Message{}, ErrValidation
	}
	now := s.clock.Now()
	var result Message
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		m, err := loadOwned(ctx, tx, ownerID, messageID, true)
		if err != nil {
			return err
		}
		if m.Version != expected {
			return ErrVersionConflict
		}
		if m.TrashedAt != nil {
			return ErrTrashed
		}
		var start int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(display_order)+1,0) FROM message_attachments WHERE message_id=$1`, messageID).Scan(&start); err != nil {
			return err
		}
		if err = s.bindUploads(ctx, tx, ownerID, messageID, uploadIDs, start, now); err != nil {
			return err
		}
		m.Version++
		m.UpdatedAt = now
		if err = saveMessage(ctx, tx, m); err != nil {
			return err
		}
		result = m
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	return s.Detail(ctx, ownerID, result.ID)
}

func (s *Service) RemoveAttachment(ctx context.Context, ownerID, messageID, attachmentID uuid.UUID, expected int64) (Message, error) {
	if expected < 1 {
		return Message{}, ErrValidation
	}
	now := s.clock.Now()
	err := s.repo.withTx(ctx, func(tx pgx.Tx) error {
		m, err := loadOwned(ctx, tx, ownerID, messageID, true)
		if err != nil {
			return err
		}
		if m.Version != expected {
			return ErrVersionConflict
		}
		if m.TrashedAt != nil {
			return ErrTrashed
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM message_attachments WHERE message_id=$1`, messageID).Scan(&count); err != nil {
			return err
		}
		if m.BodyPlaintext == nil && len(m.BodyCiphertext) == 0 && count <= 1 {
			return ErrContentRequired
		}
		ct, err := tx.Exec(ctx, `DELETE FROM message_attachments WHERE id=$1 AND message_id=$2`, attachmentID, messageID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() != 1 {
			return ErrNotFound
		}
		m.Version++
		m.UpdatedAt = now
		return saveMessage(ctx, tx, m)
	})
	if err != nil {
		return Message{}, err
	}
	return s.Detail(ctx, ownerID, messageID)
}
