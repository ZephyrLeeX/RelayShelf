//go:build integration

package files_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
)

func TestDownloadOwnershipSharedGCAndReconciliation(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	adapter, err := storage.NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	service := files.NewService(db, adapter)
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	alice, bob := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, u := range []struct {
		id   uuid.UUID
		name string
	}{{alice, "alice"}, {bob, "bob"}} {
		if _, err = db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,status)VALUES($1,$2,$2,'x','ACTIVE')`, u.id, u.name); err != nil {
			t.Fatal(err)
		}
	}
	fileID := writeObject(t, ctx, adapter, []byte("shared"))
	sum := sha256.Sum256([]byte("shared"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at)VALUES($1,$2,6,'application/octet-stream','filesystem',$3,'READY',$4,$4,$4)`, fileID, sum[:], storage.ObjectKey(fileID).String(), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	messageIDs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	attachmentIDs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	for i, owner := range []uuid.UUID{alice, bob} {
		if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at)VALUES($1,$2,'x','TEXT',false,'TEMPORARY',$3,$4,$4)`, messageIDs[i], owner, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order)VALUES($1,$2,$3,$4,0)`, attachmentIDs[i], messageIDs[i], fileID, "shared.bin"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = service.AuthorizedDownload(ctx, bob, attachmentIDs[0]); !errors.Is(err, files.ErrAttachmentNotFound) {
		t.Fatalf("cross owner=%v", err)
	}
	if _, err = service.AuthorizedDownload(ctx, alice, attachmentIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM messages WHERE id=$1`, messageIDs[0]); err != nil {
		t.Fatal(err)
	}
	if err = service.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatal("shared object deleted")
	}
	if _, err = db.Exec(ctx, `DELETE FROM messages WHERE id=$1`, messageIDs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET ready_at=$2 WHERE id=$1`, fileID, now); err != nil {
		t.Fatal(err)
	}
	if err = service.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatal("grace ignored")
	}
	if _, err = db.Exec(ctx, `UPDATE file_objects SET ready_at=$2 WHERE id=$1`, fileID, now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = service.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object remains %v", err)
	}
	pendingID := writeObject(t, ctx, adapter, []byte("recover"))
	pendingHash := sha256.Sum256([]byte("recover"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at)VALUES($1,$2,7,'application/octet-stream','filesystem',$3,'PENDING',$4,$4)`, pendingID, pendingHash[:], storage.ObjectKey(pendingID).String(), now); err != nil {
		t.Fatal(err)
	}
	if err = service.Reconcile(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1`, pendingID).Scan(&status); err != nil || status != "READY" {
		t.Fatalf("pending recovery=%s %v", status, err)
	}
	missingID := uuid.Must(uuid.NewV7())
	missingHash := sha256.Sum256([]byte("missing"))
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at)VALUES($1,$2,7,'application/octet-stream','filesystem',$3,'PENDING',$4,$4)`, missingID, missingHash[:], storage.ObjectKey(missingID).String(), now); err != nil {
		t.Fatal(err)
	}
	if err = service.Reconcile(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_objects WHERE id=$1)`, missingID).Scan(&exists); err != nil || exists {
		t.Fatalf("missing pending remains %v %v", exists, err)
	}
}

func writeObject(t *testing.T, ctx context.Context, a *storage.FilesystemStorageAdapter, data []byte) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	f, err := a.CreateCommitTemp(ctx, storage.CommitTempKey(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = a.Commit(ctx, storage.CommitTempKey(id), storage.ObjectKey(id)); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDerivativeDeleteBeforeSource(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)
	root := t.TempDir()
	adapter, _ := storage.NewFilesystemStorageAdapter(root)
	if err := adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	service := files.NewService(db, adapter)
	now := time.Now().UTC()
	source := writeObject(t, ctx, adapter, []byte("source"))
	hash := sha256.Sum256([]byte("source"))
	if _, err := db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at)VALUES($1,$2,6,'x','filesystem',$3,'DELETING',$4,$4,$4)`, source, hash[:], storage.ObjectKey(source).String(), now); err != nil {
		t.Fatal(err)
	}
	derivative := uuid.Must(uuid.NewV7())
	if err := os.WriteFile(filepath.Join(root, "derivatives", derivative.String()), []byte("d"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status)VALUES($1,$2,'TEST',$3,'x',1,'READY')`, derivative, source, storage.DerivativeKey(derivative).String()); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "derivatives", derivative.String())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("derivative remains %v", err)
	}
	if _, err := adapter.Stat(ctx, storage.ObjectKey(source)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("source remains %v", err)
	}
}
