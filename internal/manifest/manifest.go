package manifest

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Store.Get/GetByIDOrRun when no Manifest
// matches.
var ErrNotFound = errors.New("manifest: not found")

// Entry is one resolved resource within a Manifest. Position is preserved —
// entry order is a hard invariant since it can change effective model input.
type Entry struct {
	Position int
	// URI is always the bare ctx://<ns>/<path>; Ref records the "@<tag>"
	// the caller mounted it by ("" for a plain URI). Ref is informational
	// provenance — replay and diff key off SnapshotID/ContentHash, so a tag
	// that later moves can never change what a committed manifest replays.
	URI               string
	Ref               string
	SnapshotID        string
	MaterializationID string
	ContentHash       string
}

// Manifest is the immutable record of all context resolved during a
// logical execution ("run").
type Manifest struct {
	ManifestID string
	RunID      string
	CreatedAt  time.Time
	Entries    []Entry
}

type Store interface {
	Create(ctx context.Context, m Manifest) error
	Get(ctx context.Context, manifestID string) (Manifest, error)
	// GetByIDOrRun tries manifest_id first, then falls back to run_id.
	GetByIDOrRun(ctx context.Context, idOrRun string) (Manifest, error)
}
