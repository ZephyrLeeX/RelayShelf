package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/ZephyrLeeX/RelayShelf/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockID int64 = 823174592113

type Migration struct {
	Version   int64
	Name, SQL string
}

func embeddedMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, err
	}
	result := make([]Migration, 0, len(entries))
	seen := map[int64]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := entry.Name()
		split := strings.SplitN(name, "_", 2)
		if len(split) != 2 || len(split[0]) != 6 {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		version, err := strconv.ParseInt(split[0], 10, 64)
		if err != nil || version <= 0 || seen[version] {
			return nil, fmt.Errorf("invalid migration version in %q", name)
		}
		sql, err := migrations.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		seen[version] = true
		result = append(result, Migration{version, name, string(sql)})
	}
	if len(result) == 0 {
		return nil, errors.New("no embedded migrations")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}
func LatestVersion() (int64, error) {
	ms, err := embeddedMigrations()
	if err != nil {
		return 0, err
	}
	return ms[len(ms)-1].Version, nil
}
func ensureMetadata(ctx context.Context, db pgx.Tx) error {
	_, err := db.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())")
	return err
}
func CurrentVersion(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (int64, error) {
	var v int64
	err := db.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return 0, nil
		}
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockID) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err = ensureMetadata(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	ms, err := embeddedMigrations()
	if err != nil {
		return err
	}
	current, err := CurrentVersion(ctx, conn)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.Version <= current {
			continue
		}
		if err := ApplyMigration(ctx, conn, m); err != nil {
			return err
		}
	}
	return nil
}

// ApplyMigration executes one migration and records its version atomically.
// It is exposed within the repository's internal namespace so integration tests
// can verify the rollback guarantee with deliberately invalid SQL.
func ApplyMigration(ctx context.Context, conn *pgxpool.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, m.SQL); err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.Version)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("apply migration %s: %w", m.Name, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", m.Name, err)
	}
	return nil
}

func CheckCompatible(ctx context.Context, db *pgxpool.Pool) error {
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	latest, err := LatestVersion()
	if err != nil {
		return err
	}
	if current < latest {
		return fmt.Errorf("database schema is at version %d; binary requires %d: run relayshelf migrate", current, latest)
	}
	if current > latest {
		return fmt.Errorf("database schema is at version %d; binary supports %d", current, latest)
	}
	return nil
}
