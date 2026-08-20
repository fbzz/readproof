package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"ctx/internal/ids"
	"ctx/internal/manifest"
	"ctx/internal/merkle"
	"ctx/internal/resolver"
	"ctx/internal/resource"
	"ctx/internal/telemetry"
)

// ErrNotFound is returned by RunStore.GetRun when no Run matches.
var ErrNotFound = errors.New("run: not found")

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
	Position int
	// URI is the bare ctx://<ns>/<path>; Ref is the "@<tag>" it was mounted
	// by, or "" — see manifest.Entry.
	URI               string
	Ref               string
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

// Start opens a run. ctx.run.id is on this span and on every later
// ctx.run.mount/ctx.run.commit because a run legitimately spans processes
// (`ctx run start`, then `ctx run mount` from a worker, then `ctx run
// commit`): with no ambient span to share, that attribute is the only thing
// joining them.
func (b *Builder) Start(ctx context.Context, runID string) error {
	sctx, span := telemetry.Tracer.Start(ctx, "ctx.run.start", trace.WithAttributes(
		attribute.String("ctx.run.id", runID),
	))
	defer span.End()
	err := b.Runs.StartRun(sctx, runID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// Mount resolves rawURI via the same pipeline `ctx get` uses — including a
// trailing "@<tag>" — then stages it as the next entry in the run. The
// entry records the bare URI and the ref separately, so what the run
// mounted stays readable without re-parsing the combined string.
//
// The whole mount is one ctx.run.mount span, so the ctx.resolve tree and
// the ctx.manifest.append that records it hang off the same parent: a
// reader of the trace sees "this run mounted this URI, and here is
// everything that took" rather than two unrelated subtrees.
func (b *Builder) Mount(ctx context.Context, runID, rawURI string) (result resolver.ResolveResult, err error) {
	uri, ref, err := resource.SplitRef(rawURI)
	if err != nil {
		return resolver.ResolveResult{}, err
	}

	mountAttrs := []attribute.KeyValue{
		attribute.String("ctx.run.id", runID),
		attribute.String("ctx.resource.uri", uri),
	}
	if ref != "" {
		mountAttrs = append(mountAttrs, attribute.String("ctx.resource.ref", ref))
	}
	ctx, span := telemetry.Tracer.Start(ctx, "ctx.run.mount", trace.WithAttributes(mountAttrs...))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	result, err = b.Resolver.ResolveRef(ctx, uri, ref)
	if err != nil {
		return resolver.ResolveResult{}, err
	}
	mounts, err := b.Runs.ListMounts(ctx, runID)
	if err != nil {
		err = fmt.Errorf("run: list mounts: %w", err)
		return resolver.ResolveResult{}, err
	}
	entry := MountEntry{
		Position:          len(mounts),
		URI:               uri,
		Ref:               ref,
		SnapshotID:        result.Snapshot.SnapshotID,
		MaterializationID: result.Materialization.MaterializationID,
		ContentHash:       result.Materialization.ContentHash,
	}
	span.SetAttributes(attribute.Int("ctx.manifest.position", entry.Position))

	err = func() error {
		actx, aspan := telemetry.Tracer.Start(ctx, "ctx.manifest.append", trace.WithAttributes(
			attribute.String("ctx.resource.uri", uri),
			attribute.String("ctx.snapshot.id", entry.SnapshotID),
			attribute.Int("ctx.manifest.position", entry.Position),
		))
		defer aspan.End()
		e := b.Runs.AppendMount(actx, runID, entry)
		if e != nil {
			aspan.RecordError(e)
			aspan.SetStatus(codes.Error, e.Error())
		}
		return e
	}()
	if err != nil {
		err = fmt.Errorf("run: append mount: %w", err)
		return resolver.ResolveResult{}, err
	}
	return result, nil
}

// Commit builds and persists the immutable Manifest from all staged mounts,
// preserving entry order by construction.
//
// The ctx.run.commit span carries the Merkle root of the committed entries
// — the same value `ctx evidence export` puts in the bundle's in-toto
// subject digest (see internal/merkle). That makes the trace and the
// evidence bundle joinable on a single field: given a trace, an auditor can
// tell whether a bundle they were handed describes that exact run.
func (b *Builder) Commit(ctx context.Context, runID string) (man manifest.Manifest, err error) {
	ctx, span := telemetry.Tracer.Start(ctx, "ctx.run.commit", trace.WithAttributes(
		attribute.String("ctx.run.id", runID),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	mounts, err := b.Runs.ListMounts(ctx, runID)
	if err != nil {
		err = fmt.Errorf("run: list mounts: %w", err)
		return manifest.Manifest{}, err
	}
	entries := make([]manifest.Entry, len(mounts))
	for i, m := range mounts {
		entries[i] = manifest.Entry{
			Position:          m.Position,
			URI:               m.URI,
			Ref:               m.Ref,
			SnapshotID:        m.SnapshotID,
			MaterializationID: m.MaterializationID,
			ContentHash:       m.ContentHash,
		}
	}
	man = manifest.Manifest{
		ManifestID: ids.New("manifest"),
		RunID:      runID,
		CreatedAt:  b.now(),
		Entries:    entries,
	}
	if err = b.Manifests.Create(ctx, man); err != nil {
		err = fmt.Errorf("run: create manifest: %w", err)
		return manifest.Manifest{}, err
	}
	telemetry.RecordManifestCreated(ctx)
	if err = b.Runs.MarkCommitted(ctx, runID, man.ManifestID); err != nil {
		err = fmt.Errorf("run: mark committed: %w", err)
		return manifest.Manifest{}, err
	}
	telemetry.RecordRunCommitted(ctx)
	span.SetAttributes(
		attribute.String("ctx.manifest.id", man.ManifestID),
		attribute.Int("ctx.manifest.entries", len(man.Entries)),
		attribute.String("ctx.manifest.merkle_root", merkleRoot(man.Entries)),
	)
	return man, nil
}

// merkleRoot commits to the entries a manifest was created with, using the
// same leaf/root rule as the evidence bundle.
func merkleRoot(entries []manifest.Entry) string {
	leaves := make([]string, len(entries))
	for i, e := range entries {
		leaves[i] = merkle.Leaf(e.Position, e.URI, e.ContentHash)
	}
	return merkle.Root(leaves)
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
