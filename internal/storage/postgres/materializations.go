package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fbzz/readproof/internal/materialization"
)

type MaterializationStore struct {
	DB *sql.DB
}

func NewMaterializationStore(db *sql.DB) *MaterializationStore {
	return &MaterializationStore{DB: db}
}

func (s *MaterializationStore) Create(ctx context.Context, m materialization.Materialization) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO materializations (materialization_id, snapshot_id, strategy, content_hash, bytes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		m.MaterializationID, m.SnapshotID, string(m.Strategy), m.ContentHash, m.Bytes,
		m.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("postgres: insert materialization: %w", err)
	}
	return nil
}

func (s *MaterializationStore) Get(ctx context.Context, id string) (materialization.Materialization, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT materialization_id, snapshot_id, strategy, content_hash, bytes, created_at
		FROM materializations WHERE materialization_id = $1`, id)
	m, err := scanMaterialization(row)
	if err != nil {
		return materialization.Materialization{}, fmt.Errorf("postgres: get materialization %s: %w", id, err)
	}
	return m, nil
}

func (s *MaterializationStore) GetBySnapshot(ctx context.Context, snapshotID string, strategy materialization.Strategy) (materialization.Materialization, bool, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT materialization_id, snapshot_id, strategy, content_hash, bytes, created_at
		FROM materializations WHERE snapshot_id = $1 AND strategy = $2`, snapshotID, string(strategy))
	m, err := scanMaterialization(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return materialization.Materialization{}, false, nil
		}
		return materialization.Materialization{}, false, fmt.Errorf("postgres: get materialization by snapshot: %w", err)
	}
	return m, true, nil
}

func scanMaterialization(row scanner) (materialization.Materialization, error) {
	var (
		m         materialization.Materialization
		strategy  string
		createdAt string
	)
	if err := row.Scan(&m.MaterializationID, &m.SnapshotID, &strategy, &m.ContentHash, &m.Bytes, &createdAt); err != nil {
		return materialization.Materialization{}, err
	}
	m.Strategy = materialization.Strategy(strategy)
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return m, nil
}
