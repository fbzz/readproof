package mcp

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"readproof/internal/api"
	"readproof/internal/app"
	"readproof/internal/client/remote"
	"readproof/internal/evidence"
)

// TestServerOverRemoteClient runs the same MCP surface against a readproofd
// reached over HTTP instead of an embedded data directory. The server is
// written entirely against client.Client, so this is the test that proves
// the claim: nothing in internal/mcp knows which implementation it got, and
// `readproof mcp --server https://…` therefore behaves like `readproof mcp
// --data-dir …`.
func TestServerOverRemoteClient(t *testing.T) {
	fixturePath := filepath.Join(t.TempDir(), "refunds.md")
	if err := os.WriteFile(fixturePath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	defer a.Close()

	// An API key on the server proves the MCP layer inherits readproofd's auth
	// rather than bypassing it: the same client the CLI builds carries it.
	const apiKey = "test-key"
	server := httptest.NewServer(api.NewHandler(a, api.Options{APIKey: apiKey}))
	defer server.Close()

	c := remote.New(server.URL, apiKey)
	defer c.Close()

	registerDemoResource(t, c, fixturePath)
	cs := connect(t, c)
	ctx := context.Background()

	listed, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(listed.Resources) != 1 || listed.Resources[0].URI != demoURI {
		t.Fatalf("unexpected resource list over HTTP: %+v", listed.Resources)
	}

	read, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: demoURI})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if read.Contents[0].Text != originalContent {
		t.Fatalf("read over HTTP = %q, want %q", read.Contents[0].Text, originalContent)
	}
	snapshotID := metaString(t, read.Contents[0].Meta, "snapshot_id")
	if got := metaString(t, read.Contents[0].Meta, "decision"); got != "fetch" {
		t.Fatalf("decision over HTTP = %q, want fetch", got)
	}

	callTool[TagInfo](t, cs, "readproof_tag_set", map[string]any{"uri": demoURI, "tag": "prod", "snapshot_id": snapshotID})

	callTool[RunStartOut](t, cs, "readproof_run_start", map[string]any{"run_id": "run-a"})
	// Mounting by tag over the wire must keep the ref on the manifest
	// entry, since that is what `readproof diff`'s why-line reads back.
	mounted := callTool[MountOut](t, cs, "readproof_run_mount", map[string]any{"run_id": "run-a", "uri": demoURI + "@prod"})
	if mounted.Resolved.Ref != "prod" || mounted.Resolved.Decision != "use_tag" {
		t.Fatalf("tagged mount lost its ref/decision over HTTP: %+v", mounted.Resolved)
	}
	man := callTool[ManifestOut](t, cs, "readproof_run_commit", map[string]any{"run_id": "run-a"})
	if len(man.Entries) != 1 || man.Entries[0].Ref != "prod" || man.Entries[0].URI != demoURI {
		t.Fatalf("manifest entry did not record uri+ref over HTTP: %+v", man.Entries)
	}

	// The document changes; the committed manifest must not.
	if err := os.WriteFile(fixturePath, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	replayed := callTool[ReplayOut](t, cs, "readproof_replay", map[string]any{"target": "run-a", "include_content": true})
	if !replayed.AllMatch {
		t.Fatalf("replay over HTTP failed: %+v", replayed.Entries)
	}
	if replayed.Entries[0].Content == nil || replayed.Entries[0].Content.Text != originalContent {
		t.Fatalf("replayed content over HTTP = %+v, want the original bytes", replayed.Entries[0].Content)
	}

	bundle := callTool[evidence.Bundle](t, cs, "readproof_evidence_export", map[string]any{"target": "run-a"})
	direct, err := evidence.Build(ctx, c, "run-a", evidence.Options{})
	if err != nil {
		t.Fatalf("evidence.Build over HTTP: %v", err)
	}
	if bundle.Predicate.Merkle.Root != direct.Predicate.Merkle.Root {
		t.Fatalf("merkle root over MCP+HTTP = %s, want %s", bundle.Predicate.Merkle.Root, direct.Predicate.Merkle.Root)
	}
	if !bundle.Predicate.Replay.AllMatch {
		t.Fatalf("bundle over HTTP reports a failed replay: %+v", bundle.Predicate.Replay)
	}

	// The remote client flattens readproofd's 404 body into a plain error;
	// isNotFound has to recognize it or unknown URIs would surface as
	// generic failures instead of resource-not-found.
	if _, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "readproof://demo/policies/missing"}); err == nil {
		t.Fatalf("expected an error reading an unregistered resource over HTTP")
	} else if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("unregistered resource over HTTP produced %v, want a not-found error", err)
	}
}
