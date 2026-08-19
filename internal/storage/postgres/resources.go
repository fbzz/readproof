package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ctx/internal/ids"
	"ctx/internal/policy"
	"ctx/internal/resource"
)

type ResourceStore struct {
	DB *sql.DB
}

func NewResourceStore(db *sql.DB) *ResourceStore {
	return &ResourceStore{DB: db}
}

// Create transactionally inserts a Resource's sources+policies+resources
// rows — resource.Store is the single public port over all three tables.
func (s *ResourceStore) Create(ctx context.Context, r resource.Resource) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer tx.Rollback()

	sourceCfg, err := json.Marshal(r.SourceConfig)
	if err != nil {
		return fmt.Errorf("postgres: marshal source config: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	sourceID := r.SourceID
	if sourceID == "" {
		sourceID = ids.New("src")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sources (source_id, kind, config_json, created_at) VALUES ($1, $2, $3, $4)`,
		sourceID, string(r.SourceConfig.Kind), string(sourceCfg), now,
	); err != nil {
		return fmt.Errorf("postgres: insert source: %w", err)
	}

	policyID := r.PolicyID
	if policyID == "" {
		policyID = ids.New("policy")
	}
	var maxAgeSeconds sql.NullInt64
	if r.Policy.MaxAge > 0 {
		maxAgeSeconds = sql.NullInt64{Int64: int64(r.Policy.MaxAge.Seconds()), Valid: true}
	}
	var pinnedSnapshotID sql.NullString
	if r.Policy.PinnedSnapshotID != "" {
		pinnedSnapshotID = sql.NullString{String: r.Policy.PinnedSnapshotID, Valid: true}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO policies (policy_id, strategy, max_age_seconds, pinned_snapshot_id, created_at) VALUES ($1, $2, $3, $4, $5)`,
		policyID, string(r.Policy.Strategy), maxAgeSeconds, pinnedSnapshotID, now,
	); err != nil {
		return fmt.Errorf("postgres: insert policy: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO resources (uri, namespace, path, source_id, policy_id, current_snapshot_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NULL, $6, $7)`,
		r.URI, r.Namespace, r.Path, sourceID, policyID, now, now,
	); err != nil {
		return fmt.Errorf("postgres: insert resource: %w", err)
	}

	return tx.Commit()
}

func (s *ResourceStore) Get(ctx context.Context, uri string) (resource.Resource, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT r.uri, r.namespace, r.path, r.current_snapshot_id, r.created_at, r.updated_at,
		       src.source_id, src.config_json,
		       p.policy_id, p.strategy, p.max_age_seconds, p.pinned_snapshot_id
		FROM resources r
		JOIN sources src ON src.source_id = r.source_id
		JOIN policies p ON p.policy_id = r.policy_id
		WHERE r.uri = $1`, uri)

	var (
		res                  resource.Resource
		currentSnapshotID    sql.NullString
		createdAt, updatedAt string
		sourceConfigJSON     string
		strategy             string
		maxAgeSeconds        sql.NullInt64
		pinnedSnapshotID     sql.NullString
	)
	if err := row.Scan(
		&res.URI, &res.Namespace, &res.Path, &currentSnapshotID, &createdAt, &updatedAt,
		&res.SourceID, &sourceConfigJSON,
		&res.PolicyID, &strategy, &maxAgeSeconds, &pinnedSnapshotID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resource.Resource{}, fmt.Errorf("%w: %s", resource.ErrNotFound, uri)
		}
		return resource.Resource{}, fmt.Errorf("postgres: get resource: %w", err)
	}

	if err := json.Unmarshal([]byte(sourceConfigJSON), &res.SourceConfig); err != nil {
		return resource.Resource{}, fmt.Errorf("postgres: unmarshal source config: %w", err)
	}

	res.Policy = policy.Policy{Strategy: policy.Strategy(strategy)}
	if maxAgeSeconds.Valid {
		res.Policy.MaxAge = time.Duration(maxAgeSeconds.Int64) * time.Second
	}
	if pinnedSnapshotID.Valid {
		res.Policy.PinnedSnapshotID = pinnedSnapshotID.String
	}
	if currentSnapshotID.Valid {
		res.CurrentSnapshotID = currentSnapshotID.String
	}
	res.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	res.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return res, nil
}

func (s *ResourceStore) List(ctx context.Context) ([]resource.Resource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT uri FROM resources ORDER BY uri`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list resources: %w", err)
	}
	defer rows.Close()

	var uris []string
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			return nil, fmt.Errorf("postgres: scan resource uri: %w", err)
		}
		uris = append(uris, uri)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]resource.Resource, 0, len(uris))
	for _, uri := range uris {
		r, err := s.Get(ctx, uri)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *ResourceStore) UpdateCurrentSnapshot(ctx context.Context, uri, snapshotID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.DB.ExecContext(ctx,
		`UPDATE resources SET current_snapshot_id = $1, updated_at = $2 WHERE uri = $3`,
		snapshotID, now, uri,
	)
	if err != nil {
		return fmt.Errorf("postgres: update current snapshot: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", resource.ErrNotFound, uri)
	}
	return nil
}
