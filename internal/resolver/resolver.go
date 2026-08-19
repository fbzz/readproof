package resolver

import (
	"context"
	"fmt"
	"time"

	"ctx/internal/ids"
	"ctx/internal/materialization"
	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/snapshot"
	"ctx/internal/source"
	"ctx/internal/storage/blob"
)

// ResolveResult is what a single Resolve call produces. Manifest-entry
// creation is deliberately NOT part of this result — that's run.Builder's
// job, since Resolve backs both `ctx get` and `run.Builder.Mount`.
type ResolveResult struct {
	Resource        resource.Resource
	Snapshot        snapshot.Snapshot
	Materialization materialization.Materialization
	Content         []byte
	Decision        policy.Decision
}

type Resolver struct {
	Resources        resource.Store
	Snapshots        snapshot.Store
	Materializations materialization.Store
	Blobs            blob.Store
	Sources          *source.Registry
	Materializer     materialization.Materializer
	// Clock defaults to time.Now().UTC() when nil; overridable for tests.
	Clock func() time.Time
}

func (r *Resolver) now() time.Time {
	if r.Clock != nil {
		return r.Clock()
	}
	return time.Now().UTC()
}

// Resolve runs the full resolution pipeline: parse URI -> resource lookup ->
// policy/freshness evaluation -> fetch-or-reuse -> snapshot creation ->
// get-or-create materialization -> result.
func (r *Resolver) Resolve(ctx context.Context, uri string) (ResolveResult, error) {
	if _, err := resource.ParseURI(uri); err != nil {
		return ResolveResult{}, err
	}

	res, err := r.Resources.Get(ctx, uri)
	if err != nil {
		return ResolveResult{}, fmt.Errorf("resolver: %w", err)
	}

	var current snapshot.Snapshot
	hasCurrent := res.CurrentSnapshotID != ""
	if hasCurrent {
		current, err = r.Snapshots.Get(ctx, res.CurrentSnapshotID)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: load current snapshot: %w", err)
		}
	}

	now := r.now()
	decision := policy.Evaluate(res.Policy, hasCurrent, current.ObservedAt, now)

	var snap snapshot.Snapshot
	var content []byte

	switch decision {
	case policy.DecisionUsePinned:
		snap, err = r.Snapshots.Get(ctx, res.Policy.PinnedSnapshotID)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: load pinned snapshot: %w", err)
		}
		content, err = r.Blobs.Get(snap.ContentHash)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: load pinned blob: %w", err)
		}

	case policy.DecisionUseCurrent:
		snap = current
		content, err = r.Blobs.Get(snap.ContentHash)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: load cached blob: %w", err)
		}

	default: // policy.DecisionFetch
		fr, err := r.Sources.Fetch(ctx, source.FetchRequest{Config: res.SourceConfig})
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: fetch source: %w", err)
		}
		hash, err := r.Blobs.Put(fr.Content)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: store blob: %w", err)
		}
		// A new snapshot row is always created on fetch, even if content is
		// byte-identical to the current snapshot: snapshots are an
		// observation log, blobs are the dedup layer.
		snap = snapshot.Snapshot{
			SnapshotID:     ids.New("snap"),
			ResourceURI:    uri,
			SourceRevision: fr.SourceRevision,
			ContentHash:    hash,
			ObservedAt:     now,
			CreatedAt:      now,
			ContentType:    fr.ContentType,
			Bytes:          int64(len(fr.Content)),
			Provenance:     fr.Metadata,
		}
		if err := r.Snapshots.Create(ctx, snap); err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: create snapshot: %w", err)
		}
		if err := r.Resources.UpdateCurrentSnapshot(ctx, uri, snap.SnapshotID); err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: update current snapshot: %w", err)
		}
		content = fr.Content
	}

	mat, found, err := r.Materializations.GetBySnapshot(ctx, snap.SnapshotID, materialization.StrategyRaw)
	if err != nil {
		return ResolveResult{}, fmt.Errorf("resolver: lookup materialization: %w", err)
	}
	if !found {
		out, err := r.Materializer.Materialize(ctx, snap, content)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: materialize: %w", err)
		}
		matHash, err := r.Blobs.Put(out)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: store materialization blob: %w", err)
		}
		mat = materialization.Materialization{
			MaterializationID: ids.New("mat"),
			SnapshotID:        snap.SnapshotID,
			Strategy:          materialization.StrategyRaw,
			ContentHash:       matHash,
			Bytes:             int64(len(out)),
			CreatedAt:         now,
		}
		if err := r.Materializations.Create(ctx, mat); err != nil {
			return ResolveResult{}, fmt.Errorf("resolver: create materialization: %w", err)
		}
	}

	return ResolveResult{
		Resource:        res,
		Snapshot:        snap,
		Materialization: mat,
		Content:         content,
		Decision:        decision,
	}, nil
}
