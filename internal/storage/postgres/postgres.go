// Package postgres is a drop-in PostgreSQL backend for the same storage
// interfaces internal/storage/sqlite implements (resource.Store,
// snapshot.Store, materialization.Store, manifest.Store, run.RunStore).
// Behavior and semantics mirror the sqlite package exactly; only the SQL
// dialect (positional $N placeholders) and driver differ.
package postgres

import (
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// scanner abstracts *sql.Row / *sql.Rows so shared scan helpers work on
// either QueryRow or Query results.
type scanner interface {
	Scan(dest ...any) error
}

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return db, nil
}

func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("postgres: create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("postgres: read migrations dir: %w", err)
	}

	for _, entry := range entries {
		version := entry.Name()

		var applied int
		if err := db.QueryRow("SELECT COUNT(1) FROM schema_migrations WHERE version = $1", version).Scan(&applied); err != nil {
			return fmt.Errorf("postgres: check migration %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("postgres: read migration %s: %w", version, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("postgres: begin migration tx: %w", err)
		}
		// Postgres supports multi-statement Exec natively, unlike SQLite via
		// database/sql, so the whole file is applied in one call.
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("postgres: apply migration %s: %w", version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)", version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return fmt.Errorf("postgres: record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("postgres: commit migration %s: %w", version, err)
		}
	}
	return nil
}
