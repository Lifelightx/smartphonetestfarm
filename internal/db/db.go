// Package db manages the SQLite database lifecycle for the provider.
// It handles opening the connection, running embedded migrations,
// and exposing typed query methods used by the supervisor.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; registers "sqlite" driver name
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a *sql.DB and exposes typed query methods.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs any pending
// migrations. WAL mode and foreign keys are enabled automatically.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}

	// SQLite works best with a single writer connection.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	db := &DB{sql: sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: migrate: %w", err)
	}

	slog.Info("db: opened", "path", path)
	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.sql.Close()
}

// Ping verifies the connection is still alive.
func (db *DB) Ping(ctx context.Context) error {
	return db.sql.PingContext(ctx)
}

// ── Migrations ────────────────────────────────────────────────────────────────

func (db *DB) migrate() error {
	// Ensure the migrations tracking table exists.
	_, err := db.sql.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     TEXT NOT NULL PRIMARY KEY,
			applied_at  TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Read all .sql files from the embedded FS, sorted by name.
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := strings.TrimSuffix(entry.Name(), ".sql")

		// Check if already applied.
		var count int
		err := db.sql.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if count > 0 {
			continue // already applied
		}

		// Read and execute the SQL.
		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		if _, err := db.sql.Exec(string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}

		// Record as applied.
		_, err = db.sql.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC().Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		slog.Info("db: migration applied", "version", version)
	}

	return nil
}
