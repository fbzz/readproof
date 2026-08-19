package run

import (
	"context"
	"fmt"
	"time"

	"ctx/internal/ids"
	"ctx/internal/manifest"
	"ctx/internal/resolver"
)

type Status string

const (
	StatusOpen      Status = "open"
	StatusCommitted Status = "committed"
)

type Run struct {
	RunID       string
	Status      Status
	CreatedAt   time.Time
	CommittedAt *time.Time
	ManifestID  string
}

// MountEntry is one resolved resource staged into an open Run before commit.
type MountEntry struct {
	Position          int
	URI               string
	SnapshotID        string
	MaterializationID string
	ContentHash       string
}

// RunStore persists in-progress runs across separate CLI process
// invocations (there is no daemon holding this state in memory).
type RunStore interface {
	StartRun(ctx context.Context, runID string) error
	GetRun(ctx context.Context, runID string) (Run, error)
	AppendMount(ctx context.Context, runID string, e MountEntry) error
	ListMounts(ctx context.Context, runID string) ([]MountEntry, error)
	MarkCommitted(ctx context.Context, runID, manifestID string) error
}

// Builder is the CLI-only orchestrator standing in for the future SDK's
// ctx.run({id}).mount(uri)...commit() flow.
type Builder struct {
	Runs      RunStore
	Manifests manifest.Store
	Resolver  *resolver.Resolver
	Clock     func() time.Time
}

func (b *Builder) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}
	return time.Now().UTC()
}

func (b *Builder) Start(ctx context.Context, runID string) error {
	return b.Runs.StartRun(ctx, runID)
}

// Mount resolves uri via the same pipeline `ctx get` uses, then stages it as
// the next entry in the run.
func (b *Builder) Mount(ctx context.Context, runID, uri string) (resolver.ResolveResult, error) {
	result, err := b.Resolver.Resolve(ctx, uri)
	if err != nil {
		return resolver.ResolveResult{}, err
	}
	mounts, err := b.Runs.ListMounts(ctx, runID)
	if err != nil {
		return resolver.ResolveResult{}, fmt.Errorf("run: list mounts: %w", err)
	}
	entry := MountEntry{
		Position:          len(mounts),
		URI:               uri,
		SnapshotID:        result.Snapshot.SnapshotID,
		MaterializationID: result.Materialization.MaterializationID,
		ContentHash:       result.Materialization.ContentHash,
	}
	if err := b.Runs.AppendMount(ctx, runID, entry); err != nil {
		return resolver.ResolveResult{}, fmt.Errorf("run: append mount: %w", err)
	}
	return result, nil
}

// Commit builds and persists the immutable Manifest from all staged mounts,
// preserving entry order by construction.
func (b *Builder) Commit(ctx context.Context, runID string) (manifest.Manifest, error) {
	mounts, err := b.Runs.ListMounts(ctx, runID)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("run: list mounts: %w", err)
	}
	entries := make([]manifest.Entry, len(mounts))
	for i, m := range mounts {
		entries[i] = manifest.Entry{
			Position:          m.Position,
			URI:               m.URI,
			SnapshotID:        m.SnapshotID,
			MaterializationID: m.MaterializationID,
			ContentHash:       m.ContentHash,
		}
	}
	man := manifest.Manifest{
		ManifestID: ids.New("manifest"),
		RunID:      runID,
		CreatedAt:  b.now(),
		Entries:    entries,
	}
	if err := b.Manifests.Create(ctx, man); err != nil {
		return manifest.Manifest{}, fmt.Errorf("run: create manifest: %w", err)
	}
	if err := b.Runs.MarkCommitted(ctx, runID, man.ManifestID); err != nil {
		return manifest.Manifest{}, fmt.Errorf("run: mark committed: %w", err)
	}
	return man, nil
}

// Run is the single-shot convenience wrapper: Start -> Mount* -> Commit.
func (b *Builder) Run(ctx context.Context, runID string, uris []string) (manifest.Manifest, error) {
	if err := b.Start(ctx, runID); err != nil {
		return manifest.Manifest{}, err
	}
	for _, uri := range uris {
		if _, err := b.Mount(ctx, runID, uri); err != nil {
			return manifest.Manifest{}, err
		}
	}
	return b.Commit(ctx, runID)
}
