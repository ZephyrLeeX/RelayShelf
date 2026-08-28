package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FileFinalizer struct {
	pool  *pgxpool.Pool
	store storage.Adapter
	ids   id.Generator
	now   clock.Clock
	slots chan struct{}
	hooks FinalizeFailureHooks
}

// contentFinalizeLocks serializes finalization of identical content inside this
// process. Entries are reference counted so hashes that are no longer active do
// not accumulate for the lifetime of the server.
var contentFinalizeLocks = newContentLockRegistry()

type contentLockRegistry struct {
	mu    sync.Mutex
	locks map[string]*contentLock
}

type contentLock struct {
	mu   sync.Mutex
	refs int
}

func newContentLockRegistry() *contentLockRegistry {
	return &contentLockRegistry{locks: make(map[string]*contentLock)}
}

func (r *contentLockRegistry) lock(key string) func() {
	r.mu.Lock()
	entry := r.locks[key]
	if entry == nil {
		entry = &contentLock{}
		r.locks[key] = entry
	}
	entry.refs++
	r.mu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		r.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(r.locks, key)
		}
		r.mu.Unlock()
	}
}

type FinalizeFailureHooks struct {
	AfterPending, AfterWrite, BeforeSync, BeforeRename, AfterRename, BeforeReady, BeforeStagingDelete func() error
	// DuringHash fires after the first buffered read of the staging hash
	// pass, the crash window for the SHA-256 phase.
	DuringHash func() error
}

type fileObject struct {
	ID                uuid.UUID
	SHA               []byte
	Size              int64
	MIME, Key, Status string
}

func NewFileFinalizer(pool *pgxpool.Pool, store storage.Adapter, ids id.Generator, now clock.Clock, concurrency int) *FileFinalizer {
	return NewFileFinalizerWithFailureHooks(pool, store, ids, now, concurrency, FinalizeFailureHooks{})
}
func NewFileFinalizerWithFailureHooks(pool *pgxpool.Pool, store storage.Adapter, ids id.Generator, now clock.Clock, concurrency int, hooks FinalizeFailureHooks) *FileFinalizer {
	if concurrency < 1 {
		panic("finalize concurrency must be positive")
	}
	return &FileFinalizer{pool: pool, store: store, ids: ids, now: now, slots: make(chan struct{}, concurrency), hooks: hooks}
}

func (f *FileFinalizer) acquire(ctx context.Context) error {
	select {
	case f.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *FileFinalizer) Finalize(ctx context.Context, upload Session, stage staging.Provider) (Session, error) {
	if err := f.acquire(ctx); err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	defer func() { <-f.slots }()
	info, err := stage.Stat(upload.ID)
	if err != nil || !info.Mode().IsRegular() || info.Size() != upload.ExpectedSize {
		_ = f.markFailed(ctx, upload.ID)
		return Session{}, ErrStagingCorrupt
	}
	hash, mimeType, err := hashStaging(upload.ID, upload.ExpectedSize, stage, f.hooks.DuringHash)
	if err != nil {
		_ = f.markFailed(ctx, upload.ID)
		return Session{}, ErrStagingCorrupt
	}
	unlockContent := contentFinalizeLocks.lock(string(hash[:]) + ":" + strconv.FormatInt(upload.ExpectedSize, 10))
	defer unlockContent()

	obj, found, err := f.find(ctx, hash[:], upload.ExpectedSize)
	if err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	if found {
		switch obj.Status {
		case "PENDING":
			obj, found, err = f.reconcilePending(ctx, obj)
			if err != nil {
				return Session{}, ErrFinalizeRetryable
			}
			if !found {
				break
			}
		case "DELETING":
			return Session{}, ErrFinalizeRetryable
		}
		if found {
			if obj.Status != "READY" || f.completeWith(ctx, upload, obj.ID) != nil {
				return Session{}, ErrFinalizeRetryable
			}
			f.deleteStaging(stage, upload.ID)
			return completedSession(upload, obj.ID, f.now.Now()), nil
		}
	}
	space, spaceErr := f.store.Space(ctx)
	if spaceErr != nil || uint64(upload.ExpectedSize) > space.AvailableBytes {
		return Session{}, ErrFinalizeRetryable
	}
	obj, created, err := f.reservePending(ctx, hash[:], upload.ExpectedSize, mimeType)
	if errors.Is(err, ErrStorageQuota) {
		return Session{}, err
	}
	if err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	if !created {
		if obj.Status == "PENDING" {
			obj, found, err = f.reconcilePending(ctx, obj)
			if err != nil {
				return Session{}, ErrFinalizeRetryable
			}
			if !found {
				obj, created, err = f.reservePending(ctx, hash[:], upload.ExpectedSize, mimeType)
				if err != nil || !created {
					return Session{}, ErrFinalizeRetryable
				}
			}
		}
		if obj.Status == "READY" {
			if err = f.completeWith(ctx, upload, obj.ID); err == nil {
				f.deleteStaging(stage, upload.ID)
				return completedSession(upload, obj.ID, f.now.Now()), nil
			}
		}
		if !created {
			return Session{}, ErrFinalizeRetryable
		}
	}
	keepPending := false
	defer func() {
		if !keepPending {
			_ = f.store.Delete(context.WithoutCancel(ctx), storage.CommitTempKey(obj.ID))
			_, _ = f.pool.Exec(context.WithoutCancel(ctx), `DELETE FROM file_objects WHERE id=$1 AND status='PENDING'`, obj.ID)
		}
	}()
	if f.hooks.AfterPending != nil {
		if err = f.hooks.AfterPending(); err != nil {
			return Session{}, ErrFinalizeRetryable
		}
	}
	tempKey := storage.CommitTempKey(obj.ID)
	_ = f.store.Delete(ctx, tempKey)
	temp, err := f.store.CreateCommitTemp(ctx, tempKey)
	if err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	closed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
	}()
	source, err := stage.Open(upload.ID)
	if err != nil {
		return Session{}, ErrStagingCorrupt
	}
	reader, ok := source.(io.Reader)
	if !ok {
		_ = source.Close()
		return Session{}, ErrStagingCorrupt
	}
	written, copyErr := copyContext(ctx, temp, reader, make([]byte, 256<<10))
	closeSourceErr := source.Close()
	if copyErr != nil || closeSourceErr != nil || written != upload.ExpectedSize {
		return Session{}, ErrFinalizeRetryable
	}
	if f.hooks.AfterWrite != nil {
		if err = f.hooks.AfterWrite(); err != nil {
			return Session{}, ErrFinalizeRetryable
		}
	}
	if f.hooks.BeforeSync != nil {
		if err = f.hooks.BeforeSync(); err != nil {
			return Session{}, ErrFinalizeRetryable
		}
	}
	if err = temp.Sync(); err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	if err = temp.Close(); err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	closed = true
	if f.hooks.BeforeRename != nil {
		if err = f.hooks.BeforeRename(); err != nil {
			return Session{}, ErrFinalizeRetryable
		}
	}
	if err = f.store.Commit(ctx, tempKey, storage.ObjectKey(obj.ID)); err != nil {
		if exists, statErr := f.finalExists(ctx, obj); exists || statErr != nil {
			keepPending = true
		}
		return Session{}, ErrFinalizeRetryable
	}
	keepPending = true
	if f.hooks.AfterRename != nil {
		if err = f.hooks.AfterRename(); err != nil {
			return Session{}, ErrFinalizeRetryable
		}
	}
	finalInfo, err := f.store.Stat(ctx, storage.ObjectKey(obj.ID))
	if err != nil || !finalInfo.Mode().IsRegular() || finalInfo.Size() != upload.ExpectedSize {
		return Session{}, ErrFinalizeRetryable
	}
	if f.hooks.BeforeReady != nil {
		if err = f.hooks.BeforeReady(); err != nil {
			return Session{}, ErrFinalizeRetryable
		}
	}
	if err = f.readyAndComplete(ctx, upload, obj); err != nil {
		return Session{}, ErrFinalizeRetryable
	}
	keepPending = true
	f.deleteStaging(stage, upload.ID)
	return completedSession(upload, obj.ID, f.now.Now()), nil
}

func completedSession(upload Session, fileID uuid.UUID, now time.Time) Session {
	upload.Status, upload.FileObjectID = Completed, &fileID
	upload.CompletedAt, upload.UpdatedAt = &now, now
	return upload
}

func (f *FileFinalizer) deleteStaging(stage staging.Provider, uploadID uuid.UUID) {
	if f.hooks.BeforeStagingDelete != nil && f.hooks.BeforeStagingDelete() != nil {
		return
	}
	_ = stage.Delete(uploadID)
}

func hashStaging(uploadID uuid.UUID, expected int64, stage staging.Provider, duringHash func() error) ([32]byte, string, error) {
	var result [32]byte
	file, err := stage.Open(uploadID)
	if err != nil {
		return result, "", err
	}
	defer func() { _ = file.Close() }()
	r, ok := file.(io.Reader)
	if !ok {
		return result, "", errors.New("staging is not readable")
	}
	h := sha256.New()
	first := make([]byte, 0, 512)
	buf := make([]byte, 256<<10)
	var total int64
	hashed := false
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if len(first) < 512 {
				take := min(n, 512-len(first))
				first = append(first, buf[:take]...)
			}
			_, _ = h.Write(buf[:n])
			total += int64(n)
			if !hashed && duringHash != nil {
				hashed = true
				if err := duringHash(); err != nil {
					return result, "", err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return result, "", readErr
		}
	}
	if total != expected {
		return result, "", ErrStagingCorrupt
	}
	copy(result[:], h.Sum(nil))
	mimeType := "application/octet-stream"
	if len(first) > 0 {
		mimeType = http.DetectContentType(first)
	}
	return result, mimeType, nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, er := src.Read(buf)
		if n > 0 {
			wn, ew := dst.Write(buf[:n])
			total += int64(wn)
			if ew != nil {
				return total, ew
			}
			if wn != n {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(er, io.EOF) {
			return total, nil
		}
		if er != nil {
			return total, er
		}
	}
}

func (f *FileFinalizer) find(ctx context.Context, hash []byte, size int64) (fileObject, bool, error) {
	var o fileObject
	err := f.pool.QueryRow(ctx, `SELECT id,sha256,size_bytes,detected_mime,storage_key,status FROM file_objects WHERE sha256=$1 AND size_bytes=$2`, hash, size).Scan(&o.ID, &o.SHA, &o.Size, &o.MIME, &o.Key, &o.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, false, nil
	}
	return o, err == nil, err
}

func (f *FileFinalizer) finalExists(ctx context.Context, obj fileObject) (bool, error) {
	_, err := f.store.Stat(ctx, storage.ObjectKey(obj.ID))
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// reconcilePending resolves the only two safe outcomes for a dedup reservation:
// a fully committed final object becomes READY, while a reservation with no
// final object is removed so this request can reserve and write it again.
func (f *FileFinalizer) reconcilePending(ctx context.Context, obj fileObject) (fileObject, bool, error) {
	key := storage.ObjectKey(obj.ID)
	info, err := f.store.Stat(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		if deleteErr := f.store.Delete(ctx, storage.CommitTempKey(obj.ID)); deleteErr != nil {
			return obj, true, deleteErr
		}
		ct, deleteErr := f.pool.Exec(ctx, `DELETE FROM file_objects fo WHERE fo.id=$1 AND fo.status='PENDING' AND NOT EXISTS(SELECT 1 FROM message_attachments ma WHERE ma.file_object_id=fo.id) AND NOT EXISTS(SELECT 1 FROM upload_sessions us WHERE us.file_object_id=fo.id)`, obj.ID)
		if deleteErr != nil {
			return obj, true, deleteErr
		}
		return obj, ct.RowsAffected() == 0, nil
	}
	if err != nil {
		return obj, true, err
	}
	if !info.Mode().IsRegular() || info.Size() != obj.Size {
		return obj, true, errors.New("pending final metadata mismatch")
	}
	file, err := f.store.Open(ctx, key)
	if err != nil {
		return obj, true, err
	}
	h := sha256.New()
	_, copyErr := io.CopyBuffer(h, file, make([]byte, 256<<10))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || !bytes.Equal(h.Sum(nil), obj.SHA) {
		return obj, true, errors.New("pending final hash mismatch")
	}
	ct, err := f.pool.Exec(ctx, `UPDATE file_objects SET status='READY',ready_at=$2,updated_at=$2 WHERE id=$1 AND status='PENDING' AND sha256=$3 AND size_bytes=$4`, obj.ID, f.now.Now(), obj.SHA, obj.Size)
	if err != nil || ct.RowsAffected() != 1 {
		return obj, true, errors.New("pending object changed")
	}
	obj.Status = "READY"
	return obj, true, nil
}

func (f *FileFinalizer) reservePending(ctx context.Context, hash []byte, size int64, mimeType string) (fileObject, bool, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return fileObject{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026082505::bigint)`); err != nil {
		return fileObject{}, false, err
	}
	var existing fileObject
	err = tx.QueryRow(ctx, `SELECT id,sha256,size_bytes,detected_mime,storage_key,status FROM file_objects WHERE sha256=$1 AND size_bytes=$2`, hash, size).Scan(&existing.ID, &existing.SHA, &existing.Size, &existing.MIME, &existing.Key, &existing.Status)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return fileObject{}, false, err
		}
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fileObject{}, false, err
	}
	var max *int64
	if err = tx.QueryRow(ctx, `SELECT max_storage_bytes FROM system_settings WHERE id=1`).Scan(&max); err != nil {
		return fileObject{}, false, err
	}
	var used int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes),0)::bigint FROM file_objects WHERE status IN ('PENDING','READY','DELETING')`).Scan(&used); err != nil {
		return fileObject{}, false, err
	}
	if max != nil && (size > *max || used > *max-size) {
		return fileObject{}, false, ErrStorageQuota
	}
	idValue, err := f.ids.New()
	if err != nil {
		return fileObject{}, false, err
	}
	o := fileObject{ID: idValue, SHA: hash, Size: size, MIME: mimeType, Key: storage.ObjectKey(idValue).String(), Status: "PENDING"}
	now := f.now.Now()
	_, err = tx.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at) VALUES($1,$2,$3,$4,'filesystem',$5,'PENDING',$6,$6) ON CONFLICT(sha256,size_bytes) DO NOTHING`, o.ID, o.SHA, o.Size, o.MIME, o.Key, now)
	if err != nil {
		return fileObject{}, false, err
	}
	var actual fileObject
	err = tx.QueryRow(ctx, `SELECT id,sha256,size_bytes,detected_mime,storage_key,status FROM file_objects WHERE sha256=$1 AND size_bytes=$2`, hash, size).Scan(&actual.ID, &actual.SHA, &actual.Size, &actual.MIME, &actual.Key, &actual.Status)
	if err != nil {
		return fileObject{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return fileObject{}, false, err
	}
	return actual, actual.ID == idValue, nil
}

func (f *FileFinalizer) completeWith(ctx context.Context, upload Session, fileID uuid.UUID) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var uploadStatus, objectStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM upload_sessions WHERE id=$1 AND user_id=$2 FOR UPDATE`, upload.ID, upload.UserID).Scan(&uploadStatus); err != nil || uploadStatus != Completing {
		return errors.New("upload changed")
	}
	if err = tx.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1 FOR UPDATE`, fileID).Scan(&objectStatus); err != nil || objectStatus != "READY" {
		return errors.New("file object is not ready")
	}
	now := f.now.Now()
	ct, err := tx.Exec(ctx, `UPDATE upload_sessions SET file_object_id=$3,status='COMPLETED',completed_at=$4,updated_at=$4 WHERE id=$1 AND user_id=$2 AND status='COMPLETING'`, upload.ID, upload.UserID, fileID, now)
	if err != nil || ct.RowsAffected() != 1 {
		return fmt.Errorf("complete upload")
	}
	return tx.Commit(ctx)
}

func (f *FileFinalizer) readyAndComplete(ctx context.Context, upload Session, obj fileObject) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := f.now.Now()
	var uploadStatus, objectStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM upload_sessions WHERE id=$1 AND user_id=$2 FOR UPDATE`, upload.ID, upload.UserID).Scan(&uploadStatus); err != nil || uploadStatus != Completing {
		return errors.New("upload changed")
	}
	if err = tx.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1 FOR UPDATE`, obj.ID).Scan(&objectStatus); err != nil || objectStatus != "PENDING" {
		return errors.New("pending object changed")
	}
	ct, err := tx.Exec(ctx, `UPDATE file_objects SET status='READY',ready_at=$2,updated_at=$2 WHERE id=$1 AND status='PENDING' AND sha256=$3 AND size_bytes=$4`, obj.ID, now, obj.SHA, obj.Size)
	if err != nil || ct.RowsAffected() != 1 {
		return errors.New("pending object changed")
	}
	ct, err = tx.Exec(ctx, `UPDATE upload_sessions SET file_object_id=$3,status='COMPLETED',completed_at=$4,updated_at=$4 WHERE id=$1 AND user_id=$2 AND status='COMPLETING'`, upload.ID, upload.UserID, obj.ID, now)
	if err != nil || ct.RowsAffected() != 1 {
		return errors.New("upload changed")
	}
	return tx.Commit(ctx)
}

func (f *FileFinalizer) markFailed(ctx context.Context, uploadID uuid.UUID) error {
	_, err := f.pool.Exec(ctx, `UPDATE upload_sessions SET status='FAILED',updated_at=$2 WHERE id=$1 AND status='COMPLETING'`, uploadID, f.now.Now())
	return err
}

// NewFileFinalizerWithHashFailureHooks extends the failure-hook constructor
// with the during-hash crash window used by process-level crash tests.
func NewFileFinalizerWithHashFailureHooks(pool *pgxpool.Pool, store storage.Adapter, ids id.Generator, now clock.Clock, concurrency int, hooks FinalizeFailureHooks, duringHash func() error) *FileFinalizer {
	hooks.DuringHash = duringHash
	return NewFileFinalizerWithFailureHooks(pool, store, ids, now, concurrency, hooks)
}
