package sqlite_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/storage/sqlite"
)

// TestMigrateUpgradesA0001OnlyDatabase proves 0002 applies to a database
// created by v0.1 (schema 0001, with rows already in the tables it alters),
// not just to a fresh one — the upgrade path every existing .readproof
// directory takes on first open after this change.
func TestMigrateUpgradesA0001OnlyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readproof.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	initSQL, err := os.ReadFile(filepath.Join("migrations", "0001_init.sql"))
	if err != nil {
		t.Fatalf("read 0001: %v", err)
	}
	for _, stmt := range strings.Split(string(initSQL), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("apply 0001 statement: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		"0001_init.sql", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("record 0001: %v", err)
	}

	// A pre-existing manifest row, so the ALTERs run against real data.
	seedPreUpgradeRows(t, db)

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate an existing 0001 database: %v", err)
	}

	var ref string
	if err := db.QueryRow(`SELECT ref FROM manifest_entries WHERE manifest_id = 'manifest_seed'`).Scan(&ref); err != nil {
		t.Fatalf("read backfilled manifest_entries.ref: %v", err)
	}
	if ref != "" {
		t.Fatalf("existing manifest entry got ref = %q, want the empty default", ref)
	}
	if err := db.QueryRow(`SELECT ref FROM run_mounts WHERE run_id = 'run_seed'`).Scan(&ref); err != nil {
		t.Fatalf("read backfilled run_mounts.ref: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM tags`).Scan(&count); err != nil {
		t.Fatalf("tags table missing after migrate: %v", err)
	}

	// Re-running is a no-op: applied migrations are recorded, not replayed.
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("re-running Migrate must be a no-op, got: %v", err)
	}
}

func seedPreUpgradeRows(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	stmts := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO sources (source_id, kind, config_json, created_at) VALUES ('src_seed', 'filesystem', '{}', ?)`, []any{now}},
		{`INSERT INTO policies (policy_id, strategy, created_at) VALUES ('policy_seed', 'require_fresh', ?)`, []any{now}},
		{`INSERT INTO resources (uri, namespace, path, source_id, policy_id, created_at, updated_at)
		  VALUES ('readproof://demo/x', 'demo', 'x', 'src_seed', 'policy_seed', ?, ?)`, []any{now, now}},
		{`INSERT INTO snapshots (snapshot_id, resource_uri, source_revision, content_hash, observed_at, created_at, content_type, bytes, provenance_json)
		  VALUES ('snap_seed', 'readproof://demo/x', 'rev', 'sha256:abc', ?, ?, 'text/plain', 3, '{}')`, []any{now, now}},
		{`INSERT INTO materializations (materialization_id, snapshot_id, strategy, content_hash, bytes, created_at)
		  VALUES ('mat_seed', 'snap_seed', 'raw', 'sha256:abc', 3, ?)`, []any{now}},
		{`INSERT INTO manifests (manifest_id, run_id, created_at) VALUES ('manifest_seed', 'run_seed', ?)`, []any{now}},
		{`INSERT INTO manifest_entries (manifest_id, position, uri, snapshot_id, materialization_id, content_hash)
		  VALUES ('manifest_seed', 0, 'readproof://demo/x', 'snap_seed', 'mat_seed', 'sha256:abc')`, nil},
		{`INSERT INTO runs (run_id, status, created_at, manifest_id) VALUES ('run_seed', 'committed', ?, 'manifest_seed')`, []any{now}},
		{`INSERT INTO run_mounts (run_id, position, uri, snapshot_id, materialization_id, content_hash)
		  VALUES ('run_seed', 0, 'readproof://demo/x', 'snap_seed', 'mat_seed', 'sha256:abc')`, nil},
	}
	for _, s := range stmts {
		if _, err := db.Exec(s.query, s.args...); err != nil {
			t.Fatalf("seed row: %v (%s)", err, s.query)
		}
	}
}
