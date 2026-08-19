// Package client defines the operations the ctx CLI needs, with two
// implementations: local (direct in-process calls into an *app.App) and
// remote (HTTP calls to a running ctxd). Every CLI command is written
// against this interface, so it behaves identically regardless of which
// implementation backs it — the --server flag / CTX_SERVER_URL env var is
// the only thing that decides which one gets constructed.
package client

import (
	"context"

	"ctx/internal/diff"
	"ctx/internal/manifest"
	"ctx/internal/replay"
	"ctx/internal/resolver"
	"ctx/internal/resource"
	"ctx/internal/snapshot"
)

type Client interface {
	Close() error

	RegisterResource(ctx context.Context, r resource.Resource) error
	ListResources(ctx context.Context) ([]resource.Resource, error)
	GetResource(ctx context.Context, uri string) (resource.Resource, error)
	GetSnapshot(ctx context.Context, id string) (snapshot.Snapshot, error)
	History(ctx context.Context, uri string) ([]snapshot.Snapshot, error)

	Resolve(ctx context.Context, uri string) (resolver.ResolveResult, error)

	RunStart(ctx context.Context, runID string) error
	// RunMount returns the resolve result and the position this mount was
	// assigned within the run.
	RunMount(ctx context.Context, runID, uri string) (result resolver.ResolveResult, position int, err error)
	RunCommit(ctx context.Context, runID string) (manifest.Manifest, error)

	GetManifest(ctx context.Context, idOrRun string) (manifest.Manifest, error)
	Diff(ctx context.Context, targetA, targetB string) (diff.Result, error)
	Replay(ctx context.Context, idOrRun string) (replay.Result, error)
}
