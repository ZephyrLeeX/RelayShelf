//go:build integration

package users_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func bootstrapService(db *pgxpool.Pool, recorderIDs id.Generator) (*users.AdminService, *auth.Argon2idHasher) {
	now := fixedClock{now: time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)}
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	return users.NewAdminService(db, hasher, id.UUIDv7{}, now, audit.NewRecorder(recorderIDs, now)), hasher
}

func TestBootstrapInitialAdminCreatesActiveAdminWithVerifiablePasswordAndSystemAudit(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	service, hasher := bootstrapService(db, id.UUIDv7{})
	const password = "bootstrap-secret-password"
	created, err := service.BootstrapInitialAdmin(ctx, " InitialAdmin ", " Initial Administrator ", password)
	if err != nil {
		t.Fatal(err)
	}
	if created.Username != "initialadmin" || created.DisplayName != "Initial Administrator" || !created.IsAdmin || created.Status != users.StatusActive || created.PasswordHash != "" {
		t.Fatalf("created=%+v", created)
	}
	var storedHash string
	if err = db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, created.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == password {
		t.Fatal("plaintext password persisted")
	}
	if ok, _, verifyErr := hasher.Verify(storedHash, password); verifyErr != nil || !ok {
		t.Fatalf("normal password authority could not verify bootstrap hash: ok=%v err=%v", ok, verifyErr)
	}
	var eventType, username string
	var actor, device, session *uuid.UUID
	var ip, userAgent, traceID *string
	var metadataText string
	if err = db.QueryRow(ctx, `SELECT event_type,actor_user_id,device_id,session_id,ip::text,user_agent,trace_id,metadata::text,metadata->>'username' FROM audit_logs WHERE target_id=$1`, created.ID).Scan(&eventType, &actor, &device, &session, &ip, &userAgent, &traceID, &metadataText, &username); err != nil {
		t.Fatal(err)
	}
	if eventType != string(audit.EventInitialAdminBootstrapped) || actor != nil || device != nil || session != nil || ip != nil || userAgent != nil || traceID != nil || username != "initialadmin" {
		t.Fatalf("audit type=%q actor=%v device=%v session=%v ip=%v ua=%v trace=%v username=%q", eventType, actor, device, session, ip, userAgent, traceID, username)
	}
	for _, secret := range []string{password, storedHash, "DATABASE_URL", "APP_ENCRYPTION_KEY", "secret", "hash"} {
		if secret != "" && containsFold(metadataText, secret) {
			t.Fatalf("audit metadata contains forbidden material %q: %s", secret, metadataText)
		}
	}
	if _, err = service.BootstrapInitialAdmin(ctx, "second", "Second", "another-password"); !errors.Is(err, users.ErrBootstrapUnavailable) {
		t.Fatalf("second bootstrap error=%v", err)
	}
}

func TestBootstrapInitialAdminRejectsInvalidInputWithoutRows(t *testing.T) {
	tests := []struct {
		name, username, displayName, password string
		want                                  error
	}{
		{name: "invalid username", username: "   ", displayName: "Admin", password: "valid-password", want: users.ErrInvalidUsername},
		{name: "invalid display name", username: "admin", displayName: "   ", password: "valid-password", want: users.ErrInvalidUsername},
		{name: "invalid password", username: "admin", displayName: "Admin", password: "short", want: users.ErrInvalidPassword},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := postgresutil.NewDatabase(t)
			service, _ := bootstrapService(db, id.UUIDv7{})
			if _, err := service.BootstrapInitialAdmin(ctx, test.username, test.displayName, test.password); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
			var usersCount, auditCount int
			if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM audit_logs)`).Scan(&usersCount, &auditCount); err != nil || usersCount != 0 || auditCount != 0 {
				t.Fatalf("users=%d audits=%d err=%v", usersCount, auditCount, err)
			}
		})
	}
}

func TestBootstrapInitialAdminAnyExistingUserRefuses(t *testing.T) {
	tests := []struct {
		name, status string
		admin        bool
	}{
		{name: "active admin", status: "ACTIVE", admin: true},
		{name: "disabled admin", status: "DISABLED", admin: true},
		{name: "active non-admin", status: "ACTIVE", admin: false},
		{name: "disabled non-admin", status: "DISABLED", admin: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := postgresutil.NewDatabase(t)
			existingID := uuid.Must(uuid.NewV7())
			if _, err := db.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES($1,'existing','Existing','hash',$2,$3)`, existingID, test.admin, test.status); err != nil {
				t.Fatal(err)
			}
			service, _ := bootstrapService(db, id.UUIDv7{})
			if _, err := service.BootstrapInitialAdmin(ctx, "admin", "Admin", "valid-password"); !errors.Is(err, users.ErrBootstrapUnavailable) {
				t.Fatalf("error=%v", err)
			}
			var count int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("users=%d err=%v", count, err)
			}
		})
	}
}

func TestConcurrentBootstrapInitialAdminExactlyOneSucceeds(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	service, _ := bootstrapService(db, id.UUIDv7{})
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, username := range []string{"admin-one", "admin-two"} {
		go func(username string) {
			ready.Done()
			<-start
			_, err := service.BootstrapInitialAdmin(ctx, username, username, "valid-password")
			errorsOut <- err
		}(username)
	}
	ready.Wait()
	close(start)
	first, second := <-errorsOut, <-errorsOut
	successes, refusals := 0, 0
	for _, err := range []error{first, second} {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, users.ErrBootstrapUnavailable):
			refusals++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	var usersCount, auditCount int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM audit_logs WHERE event_type='INITIAL_ADMIN_BOOTSTRAPPED')`).Scan(&usersCount, &auditCount); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || refusals != 1 || usersCount != 1 || auditCount != 1 {
		t.Fatalf("successes=%d refusals=%d users=%d audits=%d", successes, refusals, usersCount, auditCount)
	}
}

type failingIDs struct{}

func (failingIDs) New() (uuid.UUID, error) { return uuid.Nil, errors.New("audit id generation failed") }

func TestBootstrapInitialAdminAuditFailureRollsBackUser(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	service, _ := bootstrapService(db, failingIDs{})
	if _, err := service.BootstrapInitialAdmin(ctx, "admin", "Admin", "valid-password"); err == nil {
		t.Fatal("expected audit failure")
	}
	var usersCount, auditCount int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM audit_logs)`).Scan(&usersCount, &auditCount); err != nil || usersCount != 0 || auditCount != 0 {
		t.Fatalf("users=%d audits=%d err=%v", usersCount, auditCount, err)
	}
}

func containsFold(value, substring string) bool {
	return substring != "" && strings.Contains(strings.ToLower(value), strings.ToLower(substring))
}
