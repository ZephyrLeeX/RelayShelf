//go:build integration

package users_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	postgresutil "github.com/ZephyrLeeX/RelayShelf/internal/platform/database/testutil"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestUserRepositoryLifecycleAndHashOnlyPersistence(t *testing.T) {
	ctx := context.Background()
	db := postgresutil.NewDatabase(t)
	repo := users.NewPostgreSQLRepository(db)
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	service := users.NewService(repo, hasher, id.UUIDv7{}, fixedClock{now: time.Now().UTC()})
	plaintext := "never-store-this-password"
	created, err := service.Create(ctx, " Alice ", "Alice", plaintext, true)
	if err != nil {
		t.Fatal(err)
	}
	if created.Username != "alice" || !created.IsAdmin {
		t.Fatalf("created=%+v", created)
	}
	var stored string
	if err = db.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", created.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == plaintext {
		t.Fatal("plaintext password persisted")
	}
	ok, _, err := hasher.Verify(stored, plaintext)
	if err != nil || !ok {
		t.Fatal("persisted hash does not verify")
	}
	if _, err = service.Create(ctx, "ALICE", "duplicate", "another-password", false); !errors.Is(err, users.ErrUsernameTaken) {
		t.Fatalf("duplicate error=%v", err)
	}
	if err = service.DisableUser(ctx, created.ID.String()); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetByID(ctx, created.ID.String())
	if err != nil || loaded.Status != users.StatusDisabled {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if err = service.DeleteUser(ctx, created.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetByID(ctx, created.ID.String()); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("deleted lookup=%v", err)
	}
}
