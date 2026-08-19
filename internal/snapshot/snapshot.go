package snapshot

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Store.Get when a Snapshot doesn't exist.
var ErrNotFound = errors.New("snapshot: not found")

// Snapshot is an immutable representation of a Resource at an observed
// point in time. It must never mutate after creation.
type Snapshot struct {
	SnapshotID     string
	ResourceURI    string
	SourceRevision string
	// ContentHash is "sha256:<hex>" and content-addresses the blob store.
	ContentHash string
	ObservedAt  time.Time
	CreatedAt   time.Time
	ContentType string
	Bytes       int64
	Provenance  map[string]string
}

type Store interface {
	Create(ctx context.Context, s Snapshot) error
	Get(ctx context.Context, id string) (Snapshot, error)
	// ListByResource returns snapshots newest-first.
	ListByResource(ctx context.Context, uri string) ([]Snapshot, error)
}
