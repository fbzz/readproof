// This file re-runs the exact same Refund Agent demo assertions as
// demo_test.go, but through a real HTTP round-trip: an httptest server
// wrapping internal/api over an embedded App, driven by the
// internal/client/remote client — proving the CLI's --server mode
// (client.Client -> HTTP -> readproofd -> App) behaves identically to
// embedded mode for every operation the CLI uses, not just resolve.
package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fbzz/readproof/internal/api"
	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/client/remote"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/source"
)

func TestRefundAgentDemoReplayInvariant_RemoteClient(t *testing.T) {
	dataDir := t.TempDir()
	fixtureDir := t.TempDir()
	fixturePath := filepath.Join(fixtureDir, "refunds.md")

	const originalContent = "Products can be refunded within 30 days.\n"
	const updatedContent = "Products can be refunded within 14 days.\n"

	if err := os.WriteFile(fixturePath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a, err := app.Open(dataDir)
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	defer a.Close()

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()

	c := remote.New(server.URL, "")
	defer c.Close()

	ctx := context.Background()
	uri := "readproof://demo/policies/refunds"

	// Exercise every client operation the CLI uses, not just resolve/replay,
	// so a wire round-trip bug in any one of them fails this test.
	if err := c.RegisterResource(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixturePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("register resource: %v", err)
	}

	resources, err := c.ListResources(ctx)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != uri {
		t.Fatalf("unexpected resource list: %+v", resources)
	}

	got, err := c.GetResource(ctx, uri)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if got.SourceConfig.Kind != source.KindFilesystem || got.SourceConfig.Filesystem.Path != fixturePath {
		t.Fatalf("resource round-trip lost source config: %+v", got.SourceConfig)
	}
	if got.Policy.Strategy != policy.StrategyRequireFresh {
		t.Fatalf("resource round-trip lost policy: %+v", got.Policy)
	}

	initial, err := c.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("initial resolve: %v", err)
	}
	if string(initial.Content) != originalContent {
		t.Fatalf("resolve content mismatch: got %q", string(initial.Content))
	}
	if initial.Decision != policy.DecisionFetch {
		t.Fatalf("expected DecisionFetch on first resolve, got %v", initial.Decision)
	}

	history, err := c.History(ctx, uri)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 snapshot in history, got %d", len(history))
	}

	snap, err := c.GetSnapshot(ctx, history[0].SnapshotID)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snap.ContentHash != history[0].ContentHash {
		t.Fatalf("get snapshot mismatch with history entry")
	}

	if err := c.RunStart(ctx, "run-a"); err != nil {
		t.Fatalf("run start: %v", err)
	}
	mountResult, position, err := c.RunMount(ctx, "run-a", uri)
	if err != nil {
		t.Fatalf("run mount: %v", err)
	}
	if position != 0 {
		t.Fatalf("expected position 0 for the first mount, got %d", position)
	}
	if string(mountResult.Content) != originalContent {
		t.Fatalf("mount content mismatch: got %q", string(mountResult.Content))
	}
	manA, err := c.RunCommit(ctx, "run-a")
	if err != nil {
		t.Fatalf("run commit: %v", err)
	}
	if len(manA.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry for run-a, got %d", len(manA.Entries))
	}

	setTag, err := c.SetTag(ctx, uri, "prod", manA.Entries[0].SnapshotID)
	if err != nil {
		t.Fatalf("set tag: %v", err)
	}
	if setTag.SnapshotID != manA.Entries[0].SnapshotID {
		t.Fatalf("set tag returned %+v", setTag)
	}
	listed, err := c.ListTags(ctx, uri)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "prod" {
		t.Fatalf("unexpected tag list: %+v", listed)
	}

	if err := os.WriteFile(fixturePath, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	if err := c.RunStart(ctx, "run-b"); err != nil {
		t.Fatalf("run start (b): %v", err)
	}
	if _, _, err := c.RunMount(ctx, "run-b", uri); err != nil {
		t.Fatalf("run mount (b): %v", err)
	}
	manB, err := c.RunCommit(ctx, "run-b")
	if err != nil {
		t.Fatalf("run commit (b): %v", err)
	}
	if manA.Entries[0].ContentHash == manB.Entries[0].ContentHash {
		t.Fatalf("expected manifest content hashes to differ after the source changed")
	}

	// A tagged mount over HTTP delivers the OLD bytes and records the ref.
	if err := c.RunStart(ctx, "run-c"); err != nil {
		t.Fatalf("run start (c): %v", err)
	}
	mountC, _, err := c.RunMount(ctx, "run-c", uri+"@prod")
	if err != nil {
		t.Fatalf("run mount (c, by tag): %v", err)
	}
	if string(mountC.Content) != originalContent {
		t.Fatalf("tagged mount content mismatch: got %q, want %q", string(mountC.Content), originalContent)
	}
	if mountC.Decision != policy.DecisionUseTag || mountC.Ref != "prod" {
		t.Fatalf("tagged mount lost its decision/ref over the wire: decision=%s ref=%q", mountC.Decision, mountC.Ref)
	}
	manC, err := c.RunCommit(ctx, "run-c")
	if err != nil {
		t.Fatalf("run commit (c): %v", err)
	}
	if manC.Entries[0].URI != uri || manC.Entries[0].Ref != "prod" {
		t.Fatalf("run-c manifest entry did not record uri+ref over the wire: %+v", manC.Entries[0])
	}
	replayC, err := c.Replay(ctx, "run-c")
	if err != nil {
		t.Fatalf("replay run-c: %v", err)
	}
	if !replayC.AllMatch() || string(replayC.Entries[0].Content) != originalContent {
		t.Fatalf("replay of the tagged run failed: %+v", replayC.Entries)
	}

	if err := c.DeleteTag(ctx, uri, "prod"); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if _, err := c.Resolve(ctx, uri+"@prod"); err == nil {
		t.Fatalf("expected an error resolving a deleted tag")
	}

	fetchedManA, err := c.GetManifest(ctx, "run-a")
	if err != nil {
		t.Fatalf("get manifest by run id: %v", err)
	}
	if fetchedManA.ManifestID != manA.ManifestID {
		t.Fatalf("get manifest by run id returned a different manifest")
	}
	fetchedManAByID, err := c.GetManifest(ctx, manA.ManifestID)
	if err != nil {
		t.Fatalf("get manifest by manifest id: %v", err)
	}
	if fetchedManAByID.ManifestID != manA.ManifestID {
		t.Fatalf("get manifest by manifest id returned a different manifest")
	}

	diffResult, err := c.Diff(ctx, "run-a", "run-b")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	changed, added, removed, unchanged := diffResult.Counts()
	if changed != 1 || added != 0 || removed != 0 || unchanged != 0 {
		t.Fatalf("unexpected diff counts: changed=%d added=%d removed=%d unchanged=%d", changed, added, removed, unchanged)
	}
	if diffResult.Entries[0].UnifiedDiff == "" {
		t.Fatalf("expected a non-empty unified diff for the changed entry")
	}
	// The provenance behind the change must survive the wire round-trip —
	// it's what `readproof diff`'s "why" line prints.
	changedEntry := diffResult.Entries[0]
	if changedEntry.SourceRevisionA == "" || changedEntry.SourceRevisionB == "" || changedEntry.SourceRevisionA == changedEntry.SourceRevisionB {
		t.Fatalf("diff provenance lost over the wire: %q vs %q", changedEntry.SourceRevisionA, changedEntry.SourceRevisionB)
	}
	if changedEntry.ObservedAtA.IsZero() || changedEntry.ObservedAtB.IsZero() {
		t.Fatalf("diff observed_at lost over the wire: %+v", changedEntry)
	}

	replayResult, err := c.Replay(ctx, "run-a")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replayResult.AllMatch() {
		t.Fatalf("replay verification failed: %+v", replayResult.Entries)
	}
	entry := replayResult.Entries[0]
	if entry.RecordedHash != entry.ReplayedHash {
		t.Fatalf("SHA256 mismatch: recorded=%s replayed=%s", entry.RecordedHash, entry.ReplayedHash)
	}
	if string(entry.Content) != originalContent {
		t.Fatalf("replayed content does not match the original: got %q, want %q", string(entry.Content), originalContent)
	}
}
