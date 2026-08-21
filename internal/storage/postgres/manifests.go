package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fbzz/readproof/internal/manifest"
)

// ErrManifestNotFound is an alias for manifest.ErrNotFound, kept exported
// here for callers that prefer referring to the storage package directly.
var ErrManifestNotFound = manifest.ErrNotFound

type ManifestStore struct {
	DB *sql.DB
}

func NewManifestStore(db *sql.DB) *ManifestStore {
	return &ManifestStore{DB: db}
}

func (s *ManifestStore) Create(ctx context.Context, m manifest.Manifest) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO manifests (manifest_id, run_id, created_at) VALUES ($1, $2, $3)`,
		m.ManifestID, m.RunID, m.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("postgres: insert manifest: %w", err)
	}

	for _, e := range m.Entries {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO manifest_entries (manifest_id, position, uri, ref, snapshot_id, materialization_id, content_hash)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			m.ManifestID, e.Position, e.URI, e.Ref, e.SnapshotID, e.MaterializationID, e.ContentHash,
		); err != nil {
			return fmt.Errorf("postgres: insert manifest entry: %w", err)
		}
	}
	return tx.Commit()
}

func (s *ManifestStore) Get(ctx context.Context, manifestID string) (manifest.Manifest, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT manifest_id, run_id, created_at FROM manifests WHERE manifest_id = $1`, manifestID)
	return s.loadFromRow(ctx, row)
}

// GetByIDOrRun tries manifest_id first, then falls back to run_id.
func (s *ManifestStore) GetByIDOrRun(ctx context.Context, idOrRun string) (manifest.Manifest, error) {
	m, err := s.Get(ctx, idOrRun)
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, ErrManifestNotFound) {
		return manifest.Manifest{}, err
	}
	row := s.DB.QueryRowContext(ctx, `SELECT manifest_id, run_id, created_at FROM manifests WHERE run_id = $1`, idOrRun)
	return s.loadFromRow(ctx, row)
}

func (s *ManifestStore) loadFromRow(ctx context.Context, row *sql.Row) (manifest.Manifest, error) {
	var (
		m         manifest.Manifest
		createdAt string
	)
	if err := row.Scan(&m.ManifestID, &m.RunID, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return manifest.Manifest{}, ErrManifestNotFound
		}
		return manifest.Manifest{}, fmt.Errorf("postgres: get manifest: %w", err)
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)

	rows, err := s.DB.QueryContext(ctx, `
		SELECT position, uri, ref, snapshot_id, materialization_id, content_hash
		FROM manifest_entries WHERE manifest_id = $1 ORDER BY position ASC`, m.ManifestID)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("postgres: list manifest entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e manifest.Entry
		if err := rows.Scan(&e.Position, &e.URI, &e.Ref, &e.SnapshotID, &e.MaterializationID, &e.ContentHash); err != nil {
			return manifest.Manifest{}, fmt.Errorf("postgres: scan manifest entry: %w", err)
		}
		m.Entries = append(m.Entries, e)
	}
	return m, rows.Err()
}
