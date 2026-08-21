package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"readproof/internal/snapshot"
	"readproof/internal/tag"
)

// ErrTagNotFound is an alias for tag.ErrNotFound, kept exported here for
// callers that prefer referring to the storage package directly.
var ErrTagNotFound = tag.ErrNotFound

type TagStore struct {
	DB *sql.DB
}

func NewTagStore(db *sql.DB) *TagStore {
	return &TagStore{DB: db}
}

// Set upserts a tag after checking that the snapshot exists and was
// observed for this exact resource — the foreign keys alone would allow a
// tag on resource A to point at a snapshot of resource B.
func (s *TagStore) Set(ctx context.Context, t tag.Tag) error {
	if err := tag.ValidateName(t.Name); err != nil {
		return err
	}

	var owner string
	err := s.DB.QueryRowContext(ctx, `SELECT resource_uri FROM snapshots WHERE snapshot_id = ?`, t.SnapshotID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("sqlite: set tag: %w: %s", snapshot.ErrNotFound, t.SnapshotID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: set tag: look up snapshot: %w", err)
	}
	if owner != t.ResourceURI {
		return fmt.Errorf("%w: snapshot %s belongs to %s, not %s", tag.ErrSnapshotMismatch, t.SnapshotID, owner, t.ResourceURI)
	}

	updatedAt := t.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO tags (resource_uri, tag, snapshot_id, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (resource_uri, tag) DO UPDATE SET snapshot_id = excluded.snapshot_id, updated_at = excluded.updated_at`,
		t.ResourceURI, t.Name, t.SnapshotID, updatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: set tag: %w", err)
	}
	return nil
}

func (s *TagStore) Get(ctx context.Context, uri, name string) (tag.Tag, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT resource_uri, tag, snapshot_id, updated_at FROM tags WHERE resource_uri = ? AND tag = ?`, uri, name)
	t, err := scanTag(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tag.Tag{}, fmt.Errorf("%w: %s@%s", ErrTagNotFound, uri, name)
		}
		return tag.Tag{}, fmt.Errorf("sqlite: get tag: %w", err)
	}
	return t, nil
}

func (s *TagStore) List(ctx context.Context, uri string) ([]tag.Tag, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT resource_uri, tag, snapshot_id, updated_at FROM tags WHERE resource_uri = ? ORDER BY tag ASC`, uri)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tags: %w", err)
	}
	defer rows.Close()

	var out []tag.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan tag: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *TagStore) Delete(ctx context.Context, uri, name string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM tags WHERE resource_uri = ? AND tag = ?`, uri, name)
	if err != nil {
		return fmt.Errorf("sqlite: delete tag: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s@%s", ErrTagNotFound, uri, name)
	}
	return nil
}

func scanTag(row scanner) (tag.Tag, error) {
	var (
		t         tag.Tag
		updatedAt string
	)
	if err := row.Scan(&t.ResourceURI, &t.Name, &t.SnapshotID, &updatedAt); err != nil {
		return tag.Tag{}, err
	}
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return t, nil
}
