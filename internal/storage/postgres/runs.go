package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fbzz/readproof/internal/run"
)

// ErrRunNotFound is an alias for run.ErrNotFound, kept exported here for
// callers that prefer referring to the storage package directly.
var ErrRunNotFound = run.ErrNotFound

type RunStore struct {
	DB *sql.DB
}

func NewRunStore(db *sql.DB) *RunStore {
	return &RunStore{DB: db}
}

func (s *RunStore) StartRun(ctx context.Context, runID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO runs (run_id, status, created_at) VALUES ($1, 'open', $2)`,
		runID, now,
	)
	if err != nil {
		return fmt.Errorf("postgres: start run: %w", err)
	}
	return nil
}

func (s *RunStore) GetRun(ctx context.Context, runID string) (run.Run, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT run_id, status, created_at, committed_at, manifest_id FROM runs WHERE run_id = $1`, runID)
	var (
		r                       run.Run
		status                  string
		createdAt               string
		committedAt, manifestID sql.NullString
	)
	if err := row.Scan(&r.RunID, &status, &createdAt, &committedAt, &manifestID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return run.Run{}, ErrRunNotFound
		}
		return run.Run{}, fmt.Errorf("postgres: get run: %w", err)
	}
	r.Status = run.Status(status)
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if committedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, committedAt.String)
		r.CommittedAt = &t
	}
	if manifestID.Valid {
		r.ManifestID = manifestID.String
	}
	return r, nil
}

func (s *RunStore) AppendMount(ctx context.Context, runID string, e run.MountEntry) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO run_mounts (run_id, position, uri, ref, snapshot_id, materialization_id, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		runID, e.Position, e.URI, e.Ref, e.SnapshotID, e.MaterializationID, e.ContentHash,
	)
	if err != nil {
		return fmt.Errorf("postgres: append mount: %w", err)
	}
	return nil
}

func (s *RunStore) ListMounts(ctx context.Context, runID string) ([]run.MountEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT position, uri, ref, snapshot_id, materialization_id, content_hash
		FROM run_mounts WHERE run_id = $1 ORDER BY position ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list mounts: %w", err)
	}
	defer rows.Close()

	var mounts []run.MountEntry
	for rows.Next() {
		var e run.MountEntry
		if err := rows.Scan(&e.Position, &e.URI, &e.Ref, &e.SnapshotID, &e.MaterializationID, &e.ContentHash); err != nil {
			return nil, fmt.Errorf("postgres: scan mount: %w", err)
		}
		mounts = append(mounts, e)
	}
	return mounts, rows.Err()
}

func (s *RunStore) MarkCommitted(ctx context.Context, runID, manifestID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.ExecContext(ctx,
		`UPDATE runs SET status = 'committed', committed_at = $1, manifest_id = $2 WHERE run_id = $3`,
		now, manifestID, runID,
	)
	if err != nil {
		return fmt.Errorf("postgres: mark committed: %w", err)
	}
	return nil
}
