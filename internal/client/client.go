// Package client defines the operations the readproof CLI needs, with two
// implementations: local (direct in-process calls into an *app.App) and
// remote (HTTP calls to a running readproofd). Every CLI command is written
// against this interface, so it behaves identically regardless of which
// implementation backs it — the --server flag / READPROOF_SERVER_URL env
// var is the only thing that decides which one gets constructed.
package client

import (
	"context"

	"readproof/internal/diff"
	"readproof/internal/manifest"
	"readproof/internal/replay"
	"readproof/internal/resolver"
	"readproof/internal/resource"
	"readproof/internal/snapshot"
	"readproof/internal/tag"
)

type Client interface {
	Close() error

	RegisterResource(ctx context.Context, r resource.Resource) error
	ListResources(ctx context.Context) ([]resource.Resource, error)
	GetResource(ctx context.Context, uri string) (resource.Resource, error)
	GetSnapshot(ctx context.Context, id string) (snapshot.Snapshot, error)
	History(ctx context.Context, uri string) ([]snapshot.Snapshot, error)

	// SetTag points (uri, name) at snapshotID, creating or moving the tag,
	// and returns the stored tag. The snapshot must belong to uri.
	SetTag(ctx context.Context, uri, name, snapshotID string) (tag.Tag, error)
	// ListTags returns a resource's tags, sorted by name.
	ListTags(ctx context.Context, uri string) ([]tag.Tag, error)
	DeleteTag(ctx context.Context, uri, name string) error

	// Resolve accepts either a bare "readproof://ns/path" or a tagged
	// "readproof://ns/path@<tag>". A tagged reference resolves to exactly that
	// snapshot: no source fetch, and the resource's policy is not consulted.
	Resolve(ctx context.Context, uri string) (resolver.ResolveResult, error)

	RunStart(ctx context.Context, runID string) error
	// RunMount returns the resolve result and the position this mount was
	// assigned within the run. Like Resolve, uri may carry "@<tag>"; the
	// manifest records the bare URI plus the ref.
	RunMount(ctx context.Context, runID, uri string) (result resolver.ResolveResult, position int, err error)
	RunCommit(ctx context.Context, runID string) (manifest.Manifest, error)

	GetManifest(ctx context.Context, idOrRun string) (manifest.Manifest, error)
	Diff(ctx context.Context, targetA, targetB string) (diff.Result, error)
	Replay(ctx context.Context, idOrRun string) (replay.Result, error)
}
