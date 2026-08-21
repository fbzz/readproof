// Package remote implements client.Client over HTTP calls to a running
// readproofd, using the same internal/wire types the server
// encodes/decodes.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"readproof/internal/diff"
	"readproof/internal/manifest"
	"readproof/internal/replay"
	"readproof/internal/resolver"
	"readproof/internal/resource"
	"readproof/internal/snapshot"
	"readproof/internal/tag"
	"readproof/internal/wire"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New constructs a remote client. apiKey is sent as "Authorization: Bearer
// <apiKey>" on every request when non-empty; pass "" for a readproofd
// instance with no --api-key configured.
func New(baseURL, apiKey string) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), apiKey: apiKey, http: http.DefaultClient}
}

func (c *Client) Close() error { return nil }

func (c *Client) RegisterResource(ctx context.Context, r resource.Resource) error {
	req := wire.RegisterResourceRequest{
		URI:    r.URI,
		Source: wire.SourceToWire(r.SourceConfig),
		Policy: wire.PolicyToWire(r.Policy),
	}
	var resp wire.ResourceWire
	return c.doJSON(ctx, http.MethodPost, "/v1/resources", req, &resp)
}

func (c *Client) ListResources(ctx context.Context) ([]resource.Resource, error) {
	var resp wire.ResourceListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/resources", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]resource.Resource, len(resp.Resources))
	for i, rw := range resp.Resources {
		out[i] = wire.ResourceFromWire(rw)
	}
	return out, nil
}

func (c *Client) GetResource(ctx context.Context, uri string) (resource.Resource, error) {
	var resp wire.ResourceWire
	if err := c.doJSON(ctx, http.MethodGet, "/v1/resources/get?uri="+url.QueryEscape(uri), nil, &resp); err != nil {
		return resource.Resource{}, err
	}
	return wire.ResourceFromWire(resp), nil
}

func (c *Client) GetSnapshot(ctx context.Context, id string) (snapshot.Snapshot, error) {
	var resp wire.SnapshotWire
	if err := c.doJSON(ctx, http.MethodGet, "/v1/snapshots?id="+url.QueryEscape(id), nil, &resp); err != nil {
		return snapshot.Snapshot{}, err
	}
	return wire.SnapshotFromWire(resp), nil
}

func (c *Client) History(ctx context.Context, uri string) ([]snapshot.Snapshot, error) {
	var resp wire.HistoryResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/resources/history?uri="+url.QueryEscape(uri), nil, &resp); err != nil {
		return nil, err
	}
	out := make([]snapshot.Snapshot, len(resp.Snapshots))
	for i, sw := range resp.Snapshots {
		out[i] = wire.SnapshotFromWire(sw)
	}
	return out, nil
}

func (c *Client) SetTag(ctx context.Context, uri, name, snapshotID string) (tag.Tag, error) {
	req := wire.SetTagRequest{URI: uri, Tag: name, SnapshotID: snapshotID}
	var resp wire.TagWire
	if err := c.doJSON(ctx, http.MethodPut, "/v1/tags", req, &resp); err != nil {
		return tag.Tag{}, err
	}
	return wire.TagFromWire(resp), nil
}

func (c *Client) ListTags(ctx context.Context, uri string) ([]tag.Tag, error) {
	var resp wire.TagListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tags?uri="+url.QueryEscape(uri), nil, &resp); err != nil {
		return nil, err
	}
	out := make([]tag.Tag, len(resp.Tags))
	for i, tw := range resp.Tags {
		out[i] = wire.TagFromWire(tw)
	}
	return out, nil
}

func (c *Client) DeleteTag(ctx context.Context, uri, name string) error {
	path := fmt.Sprintf("/v1/tags?uri=%s&tag=%s", url.QueryEscape(uri), url.QueryEscape(name))
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) Resolve(ctx context.Context, uri string) (resolver.ResolveResult, error) {
	var resp wire.ResolveResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/resolve", wire.ResolveRequest{URI: uri}, &resp); err != nil {
		return resolver.ResolveResult{}, err
	}
	return wire.ResolveResponseToResult(resp), nil
}

func (c *Client) RunStart(ctx context.Context, runID string) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/runs", wire.RunStartRequest{RunID: runID}, nil)
}

func (c *Client) RunMount(ctx context.Context, runID, uri string) (resolver.ResolveResult, int, error) {
	var resp wire.RunMountResponse
	req := wire.RunMountRequest{RunID: runID, URI: uri}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/mount", req, &resp); err != nil {
		return resolver.ResolveResult{}, 0, err
	}
	return wire.ResolveResponseToResult(resp.Resolve), resp.Position, nil
}

func (c *Client) RunCommit(ctx context.Context, runID string) (manifest.Manifest, error) {
	var resp wire.ManifestWire
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/commit", wire.RunCommitRequest{RunID: runID}, &resp); err != nil {
		return manifest.Manifest{}, err
	}
	return wire.ManifestFromWire(resp), nil
}

func (c *Client) GetManifest(ctx context.Context, idOrRun string) (manifest.Manifest, error) {
	var resp wire.ManifestWire
	if err := c.doJSON(ctx, http.MethodGet, "/v1/manifests?target="+url.QueryEscape(idOrRun), nil, &resp); err != nil {
		return manifest.Manifest{}, err
	}
	return wire.ManifestFromWire(resp), nil
}

func (c *Client) Diff(ctx context.Context, targetA, targetB string) (diff.Result, error) {
	path := fmt.Sprintf("/v1/diff?a=%s&b=%s", url.QueryEscape(targetA), url.QueryEscape(targetB))
	var resp wire.DiffResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return diff.Result{}, err
	}
	return wire.DiffResponseToResult(resp), nil
}

func (c *Client) Replay(ctx context.Context, idOrRun string) (replay.Result, error) {
	var resp wire.ReplayResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/replay?target="+url.QueryEscape(idOrRun), nil, &resp); err != nil {
		return replay.Result{}, err
	}
	return wire.ReplayResponseToResult(resp), nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("client: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("client: build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("client: request to %s failed: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var errResp wire.ErrorResponse
		if jsonErr := json.NewDecoder(resp.Body).Decode(&errResp); jsonErr == nil && errResp.Error != "" {
			return fmt.Errorf("readproofd: %s", errResp.Error)
		}
		return fmt.Errorf("readproofd: unexpected status %d", resp.StatusCode)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("client: decode response: %w", err)
	}
	return nil
}
