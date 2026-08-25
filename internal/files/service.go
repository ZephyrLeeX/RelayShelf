package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Download struct {
	AttachmentID   uuid.UUID
	Filename, MIME string
	Key            storage.Key
	SHA            []byte
	Size           int64
	Modified       time.Time
}
type Service struct {
	pool  *pgxpool.Pool
	store storage.Adapter
}

func NewService(pool *pgxpool.Pool, store storage.Adapter) *Service {
	return &Service{pool: pool, store: store}
}

func (s *Service) AuthorizedDownload(ctx context.Context, ownerID, attachmentID uuid.UUID) (Download, error) {
	var d Download
	var key string
	err := s.pool.QueryRow(ctx, `SELECT ma.id,ma.original_filename,fo.detected_mime,fo.storage_key,fo.sha256,fo.size_bytes,COALESCE(fo.ready_at,fo.updated_at) FROM message_attachments ma JOIN messages m ON m.id=ma.message_id JOIN file_objects fo ON fo.id=ma.file_object_id WHERE ma.id=$1 AND m.owner_id=$2 AND fo.status='READY'`, attachmentID, ownerID).Scan(&d.AttachmentID, &d.Filename, &d.MIME, &key, &d.SHA, &d.Size, &d.Modified)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrAttachmentNotFound
	}
	if err != nil {
		return d, err
	}
	d.Key = storage.Key(key)
	if d.Key.Validate() != nil || !strings.HasPrefix(key, "objects/") {
		return Download{}, ErrStorageIntegrity
	}
	return d, nil
}
func ETag(d Download) string {
	h := sha256.New()
	_, _ = h.Write([]byte("relayshelf-etag-v1:"))
	_, _ = h.Write(d.SHA)
	_, _ = fmt.Fprintf(h, ":%d", d.Size)
	return `"` + base64.RawURLEncoding.EncodeToString(h.Sum(nil)) + `"`
}
func (s *Service) Open(ctx context.Context, d Download) (storage.File, error) {
	f, err := s.store.Open(ctx, d.Key)
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != d.Size {
		_ = f.Close()
		return nil, ErrStorageIntegrity
	}
	return f, nil
}

type objectRow struct {
	ID          uuid.UUID
	Hash        []byte
	Size        int64
	Key, Status string
}

func (s *Service) GC(ctx context.Context, batch int, now time.Time) error {
	if batch <= 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT fo.id FROM file_objects fo WHERE fo.status='READY' AND fo.ready_at<=($1::timestamptz-interval '24 hours') AND NOT EXISTS(SELECT 1 FROM message_attachments ma WHERE ma.file_object_id=fo.id) ORDER BY fo.ready_at,fo.id FOR UPDATE SKIP LOCKED LIMIT $2`, now, batch)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `UPDATE file_objects SET status='DELETING',updated_at=$2 WHERE id=$1 AND status='READY' AND NOT EXISTS(SELECT 1 FROM message_attachments WHERE file_object_id=$1)`, id, now); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return s.Reconcile(ctx, batch)
}
func (s *Service) Reconcile(ctx context.Context, batch int) error {
	if batch <= 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id,sha256,size_bytes,storage_key,status FROM file_objects WHERE status IN ('PENDING','DELETING') ORDER BY created_at,id LIMIT $1`, batch)
	if err != nil {
		return err
	}
	objects := []objectRow{}
	for rows.Next() {
		var o objectRow
		if err = rows.Scan(&o.ID, &o.Hash, &o.Size, &o.Key, &o.Status); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	rows.Close()
	for _, o := range objects {
		if o.Status == "PENDING" {
			if err = s.reconcilePending(ctx, o); err != nil {
				return err
			}
		} else if err = s.deleteObject(ctx, o); err != nil {
			return err
		}
	}
	return s.cleanupTemps(ctx)
}
func (s *Service) reconcilePending(ctx context.Context, o objectRow) error {
	key := storage.Key(o.Key)
	if key.Validate() != nil || !strings.HasPrefix(o.Key, "objects/") {
		return ErrStorageIntegrity
	}
	info, err := s.store.Stat(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		_ = s.store.Delete(ctx, storage.CommitTempKey(o.ID))
		_, dbErr := s.pool.Exec(ctx, `DELETE FROM file_objects WHERE id=$1 AND status='PENDING' AND NOT EXISTS(SELECT 1 FROM message_attachments WHERE file_object_id=$1)`, o.ID)
		return dbErr
	}
	if err != nil {
		return ErrStorageUnavailable
	}
	if !info.Mode().IsRegular() || info.Size() != o.Size {
		return ErrStorageIntegrity
	}
	f, err := s.store.Open(ctx, key)
	if err != nil {
		return ErrStorageUnavailable
	}
	h := sha256.New()
	_, copyErr := io.CopyBuffer(h, f, make([]byte, 256<<10))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return ErrStorageUnavailable
	}
	if !bytes.Equal(h.Sum(nil), o.Hash) {
		return ErrStorageIntegrity
	}
	_, err = s.pool.Exec(ctx, `UPDATE file_objects SET status='READY',ready_at=now(),updated_at=now() WHERE id=$1 AND status='PENDING'`, o.ID)
	return err
}
func (s *Service) deleteObject(ctx context.Context, o objectRow) error {
	rows, err := s.pool.Query(ctx, `SELECT id,storage_key FROM file_derivatives WHERE source_file_id=$1 ORDER BY id`, o.ID)
	if err != nil {
		return err
	}
	type derivative struct {
		id  uuid.UUID
		key string
	}
	items := []derivative{}
	for rows.Next() {
		var d derivative
		if err = rows.Scan(&d.id, &d.key); err != nil {
			rows.Close()
			return err
		}
		items = append(items, d)
	}
	rows.Close()
	for _, d := range items {
		k := storage.Key(d.key)
		if k.Validate() != nil || !strings.HasPrefix(d.key, "derivatives/") {
			return ErrStorageIntegrity
		}
		if err = s.store.Delete(ctx, k); err != nil {
			return ErrStorageUnavailable
		}
		if _, err = s.pool.Exec(ctx, `DELETE FROM file_derivatives WHERE id=$1 AND source_file_id=$2`, d.id, o.ID); err != nil {
			return err
		}
	}
	key := storage.Key(o.Key)
	if key.Validate() != nil || !strings.HasPrefix(o.Key, "objects/") {
		return ErrStorageIntegrity
	}
	if err = s.store.Delete(ctx, key); err != nil {
		return ErrStorageUnavailable
	}
	ct, err := s.pool.Exec(ctx, `DELETE FROM file_objects WHERE id=$1 AND status='DELETING' AND NOT EXISTS(SELECT 1 FROM message_attachments WHERE file_object_id=$1)`, o.ID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() != 1 {
		return ErrStorageIntegrity
	}
	return nil
}
func (s *Service) cleanupTemps(ctx context.Context) error {
	ids, err := s.store.ListCommitTemps(ctx)
	if err != nil {
		return ErrStorageUnavailable
	}
	for _, id := range ids {
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_objects WHERE id=$1 AND status='PENDING')`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if err = s.store.Delete(ctx, storage.CommitTempKey(id)); err != nil {
				return ErrStorageUnavailable
			}
		}
	}
	return nil
}
func (s *Service) VerifyReady(ctx context.Context, batch int) error {
	rows, err := s.pool.Query(ctx, `SELECT storage_key,size_bytes FROM file_objects WHERE status='READY' ORDER BY ready_at,id LIMIT $1`, batch)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var size int64
		if err = rows.Scan(&key, &size); err != nil {
			return err
		}
		info, statErr := s.store.Stat(ctx, storage.Key(key))
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != size {
			return ErrStorageIntegrity
		}
	}
	return rows.Err()
}
