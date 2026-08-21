package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/api"
	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/client/remote"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/source"
)

// TestTagAPIRoundTrip drives PUT/GET/DELETE /v1/tags and a tagged resolve
// through the real HTTP handlers and the remote client, so a wire mismatch
// on either side fails here.
func TestTagAPIRoundTrip(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()
	c := remote.New(server.URL, "")
	defer c.Close()

	ctx := t.Context()
	fixture := filepath.Join(t.TempDir(), "refunds.md")
	const original = "Products can be refunded within 30 days.\n"
	const updated = "Products can be refunded within 14 days.\n"
	if err := os.WriteFile(fixture, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	uri := "readproof://demo/policies/refunds"
	if err := c.RegisterResource(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixture},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("register resource: %v", err)
	}

	first, err := c.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("initial resolve: %v", err)
	}

	set, err := c.SetTag(ctx, uri, "prod", first.Snapshot.SnapshotID)
	if err != nil {
		t.Fatalf("set tag: %v", err)
	}
	if set.ResourceURI != uri || set.Name != "prod" || set.SnapshotID != first.Snapshot.SnapshotID {
		t.Fatalf("PUT /v1/tags round-trip mismatch: %+v", set)
	}
	if set.UpdatedAt.IsZero() {
		t.Fatalf("updated_at missing from the tag response: %+v", set)
	}

	tags, err := c.ListTags(ctx, uri)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "prod" {
		t.Fatalf("unexpected tag list: %+v", tags)
	}

	if err := os.WriteFile(fixture, []byte(updated), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	tagged, err := c.Resolve(ctx, uri+"@prod")
	if err != nil {
		t.Fatalf("resolve by tag: %v", err)
	}
	if string(tagged.Content) != original {
		t.Fatalf("tagged resolve over HTTP returned live content: %q", string(tagged.Content))
	}
	if tagged.Decision != policy.DecisionUseTag {
		t.Fatalf("freshness status did not round-trip: got %s, want use_tag", tagged.Decision)
	}
	if tagged.Ref != "prod" {
		t.Fatalf("ref did not round-trip on the resolve response: got %q", tagged.Ref)
	}

	// A tagged mount records the bare URI plus the ref in the manifest.
	if err := c.RunStart(ctx, "run-tagged"); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, _, err := c.RunMount(ctx, "run-tagged", uri+"@prod"); err != nil {
		t.Fatalf("run mount by tag: %v", err)
	}
	man, err := c.RunCommit(ctx, "run-tagged")
	if err != nil {
		t.Fatalf("run commit: %v", err)
	}
	if len(man.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(man.Entries))
	}
	if man.Entries[0].URI != uri || man.Entries[0].Ref != "prod" {
		t.Fatalf("manifest entry did not record uri+ref: %+v", man.Entries[0])
	}

	if err := c.DeleteTag(ctx, uri, "prod"); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	remaining, err := c.ListTags(ctx, uri)
	if err != nil {
		t.Fatalf("list tags after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no tags after delete, got %+v", remaining)
	}
	if err := c.DeleteTag(ctx, uri, "prod"); err == nil {
		t.Fatalf("expected an error deleting a tag that no longer exists")
	}
}

// TestTagAPIErrorStatuses pins the HTTP status each failure mode maps to —
// a bad tag name or a foreign snapshot is the caller's fault (400), an
// unknown tag or snapshot is a 404.
func TestTagAPIErrorStatuses(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()
	c := remote.New(server.URL, "")
	defer c.Close()

	ctx := t.Context()
	fixture := filepath.Join(t.TempDir(), "refunds.md")
	if err := os.WriteFile(fixture, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	uri := "readproof://demo/policies/refunds"
	otherURI := "readproof://demo/policies/shipping"
	for _, u := range []string{uri, otherURI} {
		if err := c.RegisterResource(ctx, resource.Resource{
			URI:       u,
			Namespace: "demo",
			Path:      strings.TrimPrefix(u, "readproof://demo/"),
			SourceConfig: source.Config{
				Kind:       source.KindFilesystem,
				Filesystem: &source.FilesystemConfig{Path: fixture},
			},
			Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
		}); err != nil {
			t.Fatalf("register %s: %v", u, err)
		}
	}
	foreign, err := c.Resolve(ctx, otherURI)
	if err != nil {
		t.Fatalf("resolve other resource: %v", err)
	}

	assertStatus := func(t *testing.T, want int, method, path string, body string) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status = %d, want %d", method, path, resp.StatusCode, want)
		}
	}

	assertStatus(t, http.StatusBadRequest, http.MethodPut, "/v1/tags",
		`{"uri":"`+uri+`","tag":"not a tag","snapshot_id":"`+foreign.Snapshot.SnapshotID+`"}`)
	assertStatus(t, http.StatusBadRequest, http.MethodPut, "/v1/tags",
		`{"uri":"`+uri+`","tag":"prod","snapshot_id":"`+foreign.Snapshot.SnapshotID+`"}`)
	assertStatus(t, http.StatusNotFound, http.MethodPut, "/v1/tags",
		`{"uri":"`+uri+`","tag":"prod","snapshot_id":"snap_missing"}`)
	assertStatus(t, http.StatusNotFound, http.MethodDelete, "/v1/tags?uri="+uri+"&tag=prod", "")
	assertStatus(t, http.StatusBadRequest, http.MethodGet, "/v1/tags", "")
	assertStatus(t, http.StatusNotFound, http.MethodPost, "/v1/resolve", `{"uri":"`+uri+`@nope"}`)
}
