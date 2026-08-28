//go:build integration

package users_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
	"github.com/google/uuid"
)

func TestAdminDeleteUserPreservesSharedFileObjectAndOtherOwnerDownload(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	now := time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	recorder := audit.NewRecorder(id.UUIDv7{}, clock)
	service := users.NewAdminService(db, hasher, id.UUIDv7{}, clock, recorder)
	adminID, userA, userB := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, row := range []struct {
		id    uuid.UUID
		name  string
		admin bool
	}{{adminID, "admin", true}, {userA, "alice", false}, {userB, "bob", false}} {
		if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status,created_at,updated_at)VALUES($1,$2,$2,'hash',$3,'ACTIVE',$4,$4)`, row.id, row.name, row.admin, now); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	adapter, err := storage.NewFilesystemStorageAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = adapter.EnsureLayout(ctx); err != nil {
		t.Fatal(err)
	}
	payload := []byte("shared-bytes")
	fileID := uuid.Must(uuid.NewV7())
	temp := storage.CommitTempKey(fileID)
	f, err := adapter.CreateCommitTemp(ctx, temp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = adapter.Commit(ctx, temp, storage.ObjectKey(fileID)); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	if _, err = db.Exec(ctx, `INSERT INTO file_objects(id,sha256,size_bytes,detected_mime,storage_backend,storage_key,status,created_at,updated_at,ready_at)VALUES($1,$2,$3,'application/octet-stream','filesystem',$4,'READY',$5,$5,$5)`, fileID, sum[:], len(payload), storage.ObjectKey(fileID).String(), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	messageA, messageB, attachmentA, attachmentB := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, row := range []struct{ message, attachment, owner uuid.UUID }{{messageA, attachmentA, userA}, {messageB, attachmentB, userB}} {
		if _, err = db.Exec(ctx, `INSERT INTO messages(id,owner_id,body_plaintext,body_format,sensitive,lifecycle,expires_at,created_at,updated_at)VALUES($1,$2,'file','TEXT',false,'TEMPORARY',$3,$4,$4)`, row.message, row.owner, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `INSERT INTO message_attachments(id,message_id,file_object_id,original_filename,display_order)VALUES($1,$2,$3,'shared.bin',0)`, row.attachment, row.message, fileID); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Delete(ctx, audit.Actor{UserID: adminID}, userA); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE owner_id=$1`, userA).Scan(&count); err != nil || count != 0 {
		t.Fatalf("owner A messages=%d err=%v", count, err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM message_attachments WHERE id=$1`, attachmentB).Scan(&count); err != nil || count != 1 {
		t.Fatalf("owner B attachment=%d err=%v", count, err)
	}
	fileService := files.NewService(db, adapter)
	download, err := fileService.AuthorizedDownload(ctx, userB, attachmentB)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := fileService.Open(ctx, download)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(opened)
	_ = opened.Close()
	if err != nil || string(got) != string(payload) {
		t.Fatalf("download=%q err=%v", got, err)
	}
	if err = fileService.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); err != nil {
		t.Fatalf("shared physical object deleted: %v", err)
	}
	if err = service.Delete(ctx, audit.Actor{UserID: adminID}, userB); err != nil {
		t.Fatal(err)
	}
	if err = fileService.GC(ctx, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Stat(ctx, storage.ObjectKey(fileID)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("zero-ref object remains: %v", err)
	}
	var events int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE event_type='USER_DELETED' AND metadata ? 'username' AND NOT (metadata::text ~* 'password|token|body|secret')`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("audit events=%d err=%v", events, err)
	}
}

func TestAdminCreateAndDisableUseExistingPasswordPolicyAndRevokeSessions(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	recorder := audit.NewRecorder(id.UUIDv7{}, clock)
	service := users.NewAdminService(db, hasher, id.UUIDv7{}, clock, recorder)
	adminID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status)VALUES($1,'admin','Admin','hash',true,'ACTIVE')`, adminID); err != nil {
		t.Fatal(err)
	}
	created, err := service.Create(ctx, audit.Actor{UserID: adminID}, " NewUser ", "New User", "initial-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if created.Username != "newuser" || created.PasswordHash != "" {
		t.Fatalf("created=%+v", created)
	}
	var stored string
	if err = db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, created.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if ok, _, verifyErr := hasher.Verify(stored, "initial-password"); verifyErr != nil || !ok {
		t.Fatal("existing Argon2id policy was not used")
	}
	deviceID, sessionID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if _, err = db.Exec(ctx, `INSERT INTO devices(id,user_id,name,user_agent,first_seen_at,last_seen_at)VALUES($1,$2,'test','test',$3,$3)`, deviceID, created.ID, now); err != nil {
		t.Fatal(err)
	}
	tokenHash := make([]byte, 32)
	tokenHash[0] = 1
	if _, err = db.Exec(ctx, `INSERT INTO sessions(id,user_id,device_id,token_hash,expires_at,absolute_expires_at,last_seen_at,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, sessionID, created.ID, deviceID, tokenHash, now.Add(time.Hour), now.Add(2*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if err = service.Disable(ctx, audit.Actor{UserID: adminID}, created.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	var revokedAt *time.Time
	if err = db.QueryRow(ctx, `SELECT u.status,s.revoked_at FROM users u JOIN sessions s ON s.user_id=u.id WHERE u.id=$1`, created.ID).Scan(&status, &revokedAt); err != nil || status != "DISABLED" || revokedAt == nil {
		t.Fatalf("status=%s revoked=%v err=%v", status, revokedAt, err)
	}
	var events int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE target_id=$1 AND event_type IN ('USER_CREATED','USER_DISABLED') AND NOT (metadata::text ~* 'password|hash|token|body|secret')`, created.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("events=%d err=%v", events, err)
	}
}
