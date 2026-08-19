package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ctx/internal/snapshot"
)

var ErrSnapshotNotFound = errors.New("sqlite: snapshot not found")

type SnapshotStore struct {
	DB *sql.DB
}

func NewSnapshotStore(db *sql.DB) *SnapshotStore {
	return &SnapshotStore{DB: db}
}

func (s *SnapshotStore) Create(ctx context.Context, snap snapshot.Snapshot) error {
	provenance, err := json.Marshal(snap.Provenance)
	if err != nil {
		return fmt.Errorf("sqlite: marshal provenance: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO snapshots (snapshot_id, resource_uri, source_revision, content_hash, observed_at, created_at, content_type, bytes, provenance_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.SnapshotID, snap.ResourceURI, snap.SourceRevision, snap.ContentHash,
		snap.ObservedAt.UTC().Format(time.RFC3339Nano), snap.CreatedAt.UTC().Format(time.RFC3339Nano),
		snap.ContentType, snap.Bytes, string(provenance),
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert snapshot: %w", err)
	}
	return nil
}

func (s *SnapshotStore) Get(ctx context.Context, id string) (snapshot.Snapshot, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT snapshot_id, resource_uri, source_revision, content_hash, observed_at, created_at, content_type, bytes, provenance_json
		FROM snapshots WHERE snapshot_id = ?`, id)
	snap, err := scanSnapshot(row)
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("sqlite: get snapshot %s: %w", id, err)
	}
	return snap, nil
}

func (s *SnapshotStore) ListByResource(ctx context.Context, uri string) ([]snapshot.Snapshot, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT snapshot_id, resource_uri, source_revision, content_hash, observed_at, created_at, content_type, bytes, provenance_json
		FROM snapshots WHERE resource_uri = ? ORDER BY observed_at DESC`, uri)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list snapshots: %w", err)
	}
	defer rows.Close()

	var result []snapshot.Snapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, snap)
	}
	return result, rows.Err()
}

func scanSnapshot(row scanner) (snapshot.Snapshot, error) {
	var (
		snap                   snapshot.Snapshot
		observedAt, createdAt  string
		provenanceJSON         string
	)
	if err := row.Scan(&snap.SnapshotID, &snap.ResourceURI, &snap.SourceRevision, &snap.ContentHash,
		&observedAt, &createdAt, &snap.ContentType, &snap.Bytes, &provenanceJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return snapshot.Snapshot{}, ErrSnapshotNotFound
		}
		return snapshot.Snapshot{}, fmt.Errorf("scan snapshot: %w", err)
	}
	snap.ObservedAt, _ = time.Parse(time.RFC3339Nano, observedAt)
	snap.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if err := json.Unmarshal([]byte(provenanceJSON), &snap.Provenance); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("unmarshal provenance: %w", err)
	}
	return snap, nil
}
