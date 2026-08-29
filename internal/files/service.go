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
type ThumbnailDownload struct {
	AttachmentID, DerivativeID uuid.UUID
	MIME                       string
	Key                        storage.Key
	Size                       int64
	Modified                   time.Time
}
type Service struct {
	pool    *pgxpool.Pool
	store   storage.Adapter
	monitor *storage.Monitor
}

func NewService(pool *pgxpool.Pool, store storage.Adapter) *Service {
	return &Service{pool: pool, store: store}
}

// SetMonitor installs the storage health monitor. When storage is known
// degraded, attachment reads reject immediately instead of blocking behind a
// hard-mounted NFS outage; a nil monitor keeps the unmonitored behavior.
func (s *Service) SetMonitor(monitor *storage.Monitor) { s.monitor = monitor }

func (s *Service) degraded() bool { return !s.monitor.Healthy() }

func (s *Service) AuthorizedDownload(ctx context.Context, ownerID, attachmentID uuid.UUID) (Download, error) {
	if s.degraded() {
		return Download{}, ErrStorageUnavailable
	}
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

var previewMIMEs = map[string]struct{}{
	"image/jpeg": {}, "image/png": {}, "image/gif": {}, "image/webp": {},
	"application/pdf": {},
	"audio/mpeg":      {}, "audio/mp4": {}, "audio/ogg": {}, "audio/wav": {}, "audio/webm": {},
	"video/mp4": {}, "video/webm": {}, "video/ogg": {}, "video/quicktime": {},
}

// AuthorizedPreview preserves Attachment -> Message owner authorization and
// only permits server-detected passive media types to render on the app origin.
func (s *Service) AuthorizedPreview(ctx context.Context, ownerID, attachmentID uuid.UUID) (Download, error) {
	d, err := s.AuthorizedDownload(ctx, ownerID, attachmentID)
	if err != nil {
		return Download{}, err
	}
	if !isPreviewMIME(d.MIME) {
		return Download{}, ErrPreviewNotFound
	}
	return d, nil
}

func isPreviewMIME(value string) bool {
	_, allowed := previewMIMEs[strings.ToLower(strings.TrimSpace(value))]
	return allowed
}

func (s *Service) AuthorizedThumbnail(ctx context.Context, ownerID, attachmentID uuid.UUID) (ThumbnailDownload, error) {
	var d ThumbnailDownload
	var key string
	err := s.pool.QueryRow(ctx, `SELECT ma.id,fd.id,fd.mime,fd.storage_key,fd.size_bytes,fd.updated_at FROM message_attachments ma JOIN messages m ON m.id=ma.message_id JOIN file_objects fo ON fo.id=ma.file_object_id JOIN file_derivatives fd ON fd.source_file_id=fo.id AND fd.kind='THUMBNAIL_SMALL' AND fd.status='READY' WHERE ma.id=$1 AND m.owner_id=$2 AND fo.status='READY'`, attachmentID, ownerID).Scan(&d.AttachmentID, &d.DerivativeID, &d.MIME, &key, &d.Size, &d.Modified)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrThumbnailNotFound
	}
	if err != nil {
		return d, err
	}
	d.Key = storage.Key(key)
	if d.Key.Validate() != nil || !strings.HasPrefix(key, "derivatives/") || (d.MIME != "image/jpeg" && d.MIME != "image/png") || d.Size <= 0 {
		return ThumbnailDownload{}, ErrStorageIntegrity
	}
	return d, nil
}

func ThumbnailETag(d ThumbnailDownload) string {
	return `"thumb-` + d.DerivativeID.String() + `-` + fmt.Sprint(d.Size) + `"`
}

func (s *Service) OpenThumbnail(ctx context.Context, d ThumbnailDownload) (storage.File, error) {
	f, err := s.store.Open(ctx, d.Key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrStorageIntegrity
	}
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, ErrStorageUnavailable
	}
	if !info.Mode().IsRegular() || info.Size() != d.Size {
		_ = f.Close()
		return nil, ErrStorageIntegrity
	}
	return f, nil
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
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrStorageIntegrity
	}
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, ErrStorageUnavailable
	}
	if !info.Mode().IsRegular() || info.Size() != d.Size {
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
	CreatedAt   time.Time
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
	// A completed upload is a short-lived handoff lease until an attachment is
	// bound. Once the lease expires, removing the upload lets normal orphan GC
	// reclaim the object; consumed uploads are also safe to remove here because
	// message_attachments is the long-term reference authority.
	if _, err = tx.Exec(ctx, `DELETE FROM upload_sessions WHERE status='COMPLETED' AND completed_at<=($1::timestamptz-interval '24 hours')`, now); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT fo.id FROM file_objects fo WHERE fo.status='READY' AND fo.ready_at<=($1::timestamptz-interval '24 hours') AND NOT EXISTS(SELECT 1 FROM message_attachments ma WHERE ma.file_object_id=fo.id) AND NOT EXISTS(SELECT 1 FROM upload_sessions us WHERE us.file_object_id=fo.id AND us.status='COMPLETED' AND us.consumed_at IS NULL AND us.completed_at>($1::timestamptz-interval '24 hours')) AND NOT EXISTS(SELECT 1 FROM background_jobs bj WHERE bj.job_type='GENERATE_THUMBNAIL' AND bj.subject_type='FILE_OBJECT' AND bj.subject_id=fo.id AND bj.status IN ('PENDING','RUNNING')) ORDER BY fo.ready_at,fo.id FOR UPDATE SKIP LOCKED LIMIT $2`, now, batch)
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
		if _, err = tx.Exec(ctx, `UPDATE file_objects SET status='DELETING',updated_at=$2 WHERE id=$1 AND status='READY' AND NOT EXISTS(SELECT 1 FROM message_attachments WHERE file_object_id=$1) AND NOT EXISTS(SELECT 1 FROM upload_sessions WHERE file_object_id=$1 AND status='COMPLETED' AND consumed_at IS NULL AND completed_at>($2::timestamptz-interval '24 hours')) AND NOT EXISTS(SELECT 1 FROM background_jobs bj WHERE bj.job_type='GENERATE_THUMBNAIL' AND bj.subject_type='FILE_OBJECT' AND bj.subject_id=$1 AND bj.status IN ('PENDING','RUNNING'))`, id, now); err != nil {
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
	var reconcileErrors []error
	var cursorTime time.Time
	var cursorID uuid.UUID
	haveCursor := false
	processed := 0
	for processed < batch {
		pageSize := batch - processed
		var rows pgx.Rows
		var err error
		if haveCursor {
			rows, err = s.pool.Query(ctx, `SELECT id,sha256,size_bytes,storage_key,status,created_at FROM file_objects WHERE status IN ('PENDING','DELETING') AND (created_at,id)>($1,$2) ORDER BY created_at,id LIMIT $3`, cursorTime, cursorID, pageSize)
		} else {
			rows, err = s.pool.Query(ctx, `SELECT id,sha256,size_bytes,storage_key,status,created_at FROM file_objects WHERE status IN ('PENDING','DELETING') ORDER BY created_at,id LIMIT $1`, pageSize)
		}
		if err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("scan file objects: %w", err))
			break
		}
		objects := []objectRow{}
		for rows.Next() {
			var o objectRow
			if err = rows.Scan(&o.ID, &o.Hash, &o.Size, &o.Key, &o.Status, &o.CreatedAt); err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Errorf("scan file object: %w", err))
				break
			}
			objects = append(objects, o)
		}
		if rows.Err() != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("scan file objects: %w", rows.Err()))
		}
		rows.Close()
		if len(objects) == 0 {
			break
		}
		for _, o := range objects {
			cursorTime, cursorID, haveCursor = o.CreatedAt, o.ID, true
			if o.Status == "PENDING" {
				err = s.reconcilePending(ctx, o)
			} else {
				err = s.deleteObject(ctx, o)
			}
			if err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Errorf("reconcile file object: %w", err))
				continue
			}
			processed++
			if processed == batch {
				break
			}
		}
	}
	if err := s.cleanupTemps(ctx); err != nil {
		reconcileErrors = append(reconcileErrors, err)
	}
	return errors.Join(reconcileErrors...)
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
	var cleanupErrors []error
	for _, id := range ids {
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_objects WHERE id=$1 AND status='PENDING') OR EXISTS(SELECT 1 FROM file_derivatives WHERE id=$1 AND status='PENDING')`, id).Scan(&exists); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("check commit temp owner: %w", err))
			continue
		}
		if !exists {
			if err = s.store.Delete(ctx, storage.CommitTempKey(id)); err != nil {
				cleanupErrors = append(cleanupErrors, ErrStorageUnavailable)
				continue
			}
		}
	}
	return errors.Join(cleanupErrors...)
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
