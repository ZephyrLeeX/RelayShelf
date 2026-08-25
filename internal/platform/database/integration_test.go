//go:build integration

package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPostgreSQLConnection(t *testing.T) {
	t.Parallel()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL must point to a temporary PostgreSQL instance")
	}

	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })

	var one int
	if err := conn.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("query PostgreSQL: %v", err)
	}
	if one != 1 {
		t.Fatalf("SELECT 1 = %d, want 1", one)
	}
}
