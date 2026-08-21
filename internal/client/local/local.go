// Package local implements client.Client as thin, direct calls into an
// already-open *app.App — the exact same calls the CLI made before the
// client abstraction existed. Behavior is unchanged from the walking
// skeleton; this package only moves those calls behind the interface.
package local

import (
	"context"

	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/diff"
	"github.com/fbzz/readproof/internal/manifest"
	"github.com/fbzz/readproof/internal/replay"
	"github.com/fbzz/readproof/internal/resolver"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/snapshot"
	"github.com/fbzz/readproof/internal/tag"
)

type Client struct {
	App *app.App
}

func New(a *app.App) *Client {
	return &Client{App: a}
}

func (c *Client) Close() error {
	return c.App.Close()
}

func (c *Client) RegisterResource(ctx context.Context, r resource.Resource) error {
	// Same early refusal the HTTP API applies, so an embedded client and a
	// --server client reject the same definitions with the same message. A
	// no-op under the CLI's unrestricted source policy.
	if err := c.App.Sources.Validate(r.SourceConfig); err != nil {
		return err
	}
	return c.App.Resources.Create(ctx, r)
}

func (c *Client) ListResources(ctx context.Context) ([]resource.Resource, error) {
	return c.App.Resources.List(ctx)
}

func (c *Client) GetResource(ctx context.Context, uri string) (resource.Resource, error) {
	return c.App.Resources.Get(ctx, uri)
}

func (c *Client) GetSnapshot(ctx context.Context, id string) (snapshot.Snapshot, error) {
	return c.App.Snapshots.Get(ctx, id)
}

func (c *Client) History(ctx context.Context, uri string) ([]snapshot.Snapshot, error) {
	return c.App.Snapshots.ListByResource(ctx, uri)
}

func (c *Client) SetTag(ctx context.Context, uri, name, snapshotID string) (tag.Tag, error) {
	if err := c.App.Tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: name, SnapshotID: snapshotID}); err != nil {
		return tag.Tag{}, err
	}
	// Read back so callers see the stored UpdatedAt, exactly as the remote
	// client does with PUT /v1/tags' response body.
	return c.App.Tags.Get(ctx, uri, name)
}

func (c *Client) ListTags(ctx context.Context, uri string) ([]tag.Tag, error) {
	return c.App.Tags.List(ctx, uri)
}

func (c *Client) DeleteTag(ctx context.Context, uri, name string) error {
	return c.App.Tags.Delete(ctx, uri, name)
}

func (c *Client) Resolve(ctx context.Context, uri string) (resolver.ResolveResult, error) {
	return c.App.Resolver.Resolve(ctx, uri)
}

func (c *Client) RunStart(ctx context.Context, runID string) error {
	return c.App.RunBuilder.Start(ctx, runID)
}

func (c *Client) RunMount(ctx context.Context, runID, uri string) (resolver.ResolveResult, int, error) {
	mounts, err := c.App.Runs.ListMounts(ctx, runID)
	if err != nil {
		return resolver.ResolveResult{}, 0, err
	}
	position := len(mounts)

	result, err := c.App.RunBuilder.Mount(ctx, runID, uri)
	if err != nil {
		return resolver.ResolveResult{}, 0, err
	}
	return result, position, nil
}

func (c *Client) RunCommit(ctx context.Context, runID string) (manifest.Manifest, error) {
	return c.App.RunBuilder.Commit(ctx, runID)
}

func (c *Client) GetManifest(ctx context.Context, idOrRun string) (manifest.Manifest, error) {
	return c.App.Manifests.GetByIDOrRun(ctx, idOrRun)
}

func (c *Client) Diff(ctx context.Context, targetA, targetB string) (diff.Result, error) {
	manA, err := c.App.Manifests.GetByIDOrRun(ctx, targetA)
	if err != nil {
		return diff.Result{}, err
	}
	manB, err := c.App.Manifests.GetByIDOrRun(ctx, targetB)
	if err != nil {
		return diff.Result{}, err
	}
	return diff.Compute(ctx, manA, manB, c.App.Blobs, c.App.Snapshots)
}

func (c *Client) Replay(ctx context.Context, idOrRun string) (replay.Result, error) {
	return c.App.Replayer.Replay(ctx, idOrRun)
}
