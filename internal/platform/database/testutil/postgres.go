//go:build integration

// Package testutil provides isolated PostgreSQL databases for integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewDatabase creates an isolated database from DATABASE_URL, applies embedded
// migrations, and registers cleanup that closes connections and drops it.
func NewDatabase(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := NewEmptyDatabase(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate temporary database: %v", err)
	}
	return pool
}

// NewEmptyDatabase creates an isolated database from DATABASE_URL without
// applying migrations. Migration tests can use it to exercise empty databases.
func NewEmptyDatabase(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL must point to a bootstrap PostgreSQL database")
	}
	bootstrapConfig, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	bootstrap, err := pgx.ConnectConfig(ctx, bootstrapConfig)
	if err != nil {
		t.Fatalf("connect bootstrap database: %v", err)
	}
	name := databaseName(t)
	if _, err := bootstrap.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		bootstrap.Close(ctx)
		t.Fatalf("create temporary database: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse pool DATABASE_URL: %v", err)
	}
	poolConfig.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_, _ = bootstrap.Exec(ctx, "DROP DATABASE "+name)
		bootstrap.Close(ctx)
		t.Fatalf("connect temporary database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = bootstrap.Exec(context.Background(), "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", name)
		_, _ = bootstrap.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name)
		bootstrap.Close(context.Background())
	})
	return pool
}

func databaseName(t testing.TB) string {
	t.Helper()
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatalf("generate temporary database name: %v", err)
	}
	return fmt.Sprintf("relayshelf_it_%s", hex.EncodeToString(bytes))
}
