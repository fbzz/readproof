package materialization

import (
	"context"
	"time"

	"readproof/internal/snapshot"
)

type Strategy string

const StrategyRaw Strategy = "raw"

// Materialization is the actual representation delivered to a consumer,
// derived from a Snapshot.
type Materialization struct {
	MaterializationID string
	SnapshotID        string
	Strategy          Strategy
	ContentHash       string
	Bytes             int64
	CreatedAt         time.Time
}

type Materializer interface {
	Materialize(ctx context.Context, snap snapshot.Snapshot, content []byte) ([]byte, error)
}

// RawMaterializer is the only strategy in v0.1: pass-through, deterministic.
type RawMaterializer struct{}

func (RawMaterializer) Materialize(_ context.Context, _ snapshot.Snapshot, content []byte) ([]byte, error) {
	return content, nil
}

type Store interface {
	Create(ctx context.Context, m Materialization) error
	Get(ctx context.Context, id string) (Materialization, error)
	// GetBySnapshot is the dedup lookup: a materialization is reused for a
	// given (snapshot, strategy) pair rather than rebuilt on every resolve.
	GetBySnapshot(ctx context.Context, snapshotID string, strategy Strategy) (Materialization, bool, error)
}
