// Command e2eseed creates deterministic browser-test users with real Argon2id
// password hashes. It exists only for E2E bootstrap: the production binary has
// no user-creation backdoor, so the browser suite needs this separate helper
// against the same database the server uses.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := flag.String("database-url", "", "PostgreSQL URL for the E2E database")
	flag.Parse()
	if *databaseURL == "" {
		fmt.Fprintln(os.Stderr, "-database-url is required")
		os.Exit(2)
	}
	hasher := auth.NewPasswordHasher(auth.Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	users := []struct {
		username string
		password string
		admin    bool
	}{
		{"e2e-alice", "e2e-alice-pass-12345", false},
		{"e2e-bob", "e2e-bob-pass-123456", false},
		{"e2e-admin", "e2e-admin-pass-12345", true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	for _, user := range users {
		encoded, err := hasher.Hash(user.password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hash %s: %v\n", user.username, err)
			os.Exit(1)
		}
		_, err = pool.Exec(ctx, `INSERT INTO users(id,username,display_name,password_hash,is_admin,status) VALUES(gen_random_uuid(),$1,$1,$2,$3,'ACTIVE') ON CONFLICT (username) DO UPDATE SET password_hash=EXCLUDED.password_hash, is_admin=EXCLUDED.is_admin, status='ACTIVE', updated_at=now()`, user.username, encoded, user.admin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "seed %s: %v\n", user.username, err)
			os.Exit(1)
		}
	}
	fmt.Println("e2e users seeded: e2e-alice, e2e-bob, e2e-admin")
}
