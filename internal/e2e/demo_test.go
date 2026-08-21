// Package e2e programmatically runs the Refund Agent reference demo and
// asserts the SHA256(original) == SHA256(replay) invariant that is the
// spec's own v0.1 acceptance test.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"readproof/internal/app"
	"readproof/internal/diff"
	"readproof/internal/policy"
	"readproof/internal/resource"
	"readproof/internal/source"
	"readproof/internal/tag"
)

func TestRefundAgentDemoReplayInvariant(t *testing.T) {
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

	ctx := context.Background()
	uri := "readproof://demo/policies/refunds"

	err = a.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixturePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	})
	if err != nil {
		t.Fatalf("register resource: %v", err)
	}

	// Step 2 of the demo: a standalone `readproof get` before any run exists.
	if _, err := a.Resolver.Resolve(ctx, uri); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}

	// Step 3: simulate an agent run -> manifest_A.
	manA, err := a.RunBuilder.Run(ctx, "run-a", []string{uri})
	if err != nil {
		t.Fatalf("run-a: %v", err)
	}
	if len(manA.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry for run-a, got %d", len(manA.Entries))
	}

	// Step 3b: freeze what run-a saw behind a `prod` tag.
	if err := a.Tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: manA.Entries[0].SnapshotID}); err != nil {
		t.Fatalf("tag set: %v", err)
	}

	// Step 4: the policy document changes.
	if err := os.WriteFile(fixturePath, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	// Step 5: resolve again in a new run -> manifest_B.
	manB, err := a.RunBuilder.Run(ctx, "run-b", []string{uri})
	if err != nil {
		t.Fatalf("run-b: %v", err)
	}

	// Step 6 (implicit): manifests must actually differ.
	if manA.Entries[0].ContentHash == manB.Entries[0].ContentHash {
		t.Fatalf("expected manifest content hashes to differ after the source changed")
	}

	// Step 6b: a run that mounts @prod gets the OLD bytes, even though the
	// resource's require_fresh policy would otherwise re-fetch.
	manC, err := a.RunBuilder.Run(ctx, "run-c", []string{uri + "@prod"})
	if err != nil {
		t.Fatalf("run-c: %v", err)
	}
	if manC.Entries[0].URI != uri || manC.Entries[0].Ref != "prod" {
		t.Fatalf("run-c manifest entry did not record uri+ref: %+v", manC.Entries[0])
	}
	if manC.Entries[0].ContentHash != manA.Entries[0].ContentHash {
		t.Fatalf("run-c mounted by @prod must match run-a's content hash, got %s vs %s",
			manC.Entries[0].ContentHash, manA.Entries[0].ContentHash)
	}

	replayC, err := a.Replayer.Replay(ctx, "run-c")
	if err != nil {
		t.Fatalf("replay run-c: %v", err)
	}
	if !replayC.AllMatch() {
		t.Fatalf("replay of the tagged run failed verification: %+v", replayC.Entries)
	}
	if string(replayC.Entries[0].Content) != originalContent {
		t.Fatalf("replayed tagged content = %q, want %q", string(replayC.Entries[0].Content), originalContent)
	}

	// Step 6c: the diff carries the provenance behind the change — what
	// `readproof diff`'s "why" line prints.
	diffResult, err := diff.Compute(ctx, manA, manB, a.Blobs, a.Snapshots)
	if err != nil {
		t.Fatalf("diff run-a run-b: %v", err)
	}
	if len(diffResult.Entries) != 1 || diffResult.Entries[0].Status != diff.StatusChanged {
		t.Fatalf("expected exactly one changed entry, got %+v", diffResult.Entries)
	}
	changed := diffResult.Entries[0]
	if changed.SourceRevisionA == "" || changed.SourceRevisionB == "" || changed.SourceRevisionA == changed.SourceRevisionB {
		t.Fatalf("expected distinct, non-empty source revisions per side: %q vs %q", changed.SourceRevisionA, changed.SourceRevisionB)
	}
	if changed.ObservedAtA.IsZero() || changed.ObservedAtB.IsZero() {
		t.Fatalf("expected observed_at on both sides: %+v", changed)
	}
	if changed.RefA != "" || changed.RefB != "" {
		t.Fatalf("neither run-a nor run-b mounted by tag, but refs are set: %+v", changed)
	}

	// A diff against the tagged run reports the ref that produced it.
	taggedDiff, err := diff.Compute(ctx, manB, manC, a.Blobs, a.Snapshots)
	if err != nil {
		t.Fatalf("diff run-b run-c: %v", err)
	}
	if taggedDiff.Entries[0].RefB != "prod" {
		t.Fatalf("expected ref_b = prod for the tagged run, got %q", taggedDiff.Entries[0].RefB)
	}

	// Step 7: replay manifest_A and verify the SHA256 invariant, without
	// touching the (now-changed) live source.
	replayResult, err := a.Replayer.Replay(ctx, "run-a")
	if err != nil {
		t.Fatalf("replay run-a: %v", err)
	}
	if !replayResult.AllMatch() {
		t.Fatalf("replay verification failed: %+v", replayResult.Entries)
	}
	if len(replayResult.Entries) != 1 {
		t.Fatalf("expected 1 replayed entry, got %d", len(replayResult.Entries))
	}
	entry := replayResult.Entries[0]
	if entry.RecordedHash != entry.ReplayedHash {
		t.Fatalf("SHA256 mismatch: recorded=%s replayed=%s", entry.RecordedHash, entry.ReplayedHash)
	}
	if string(entry.Content) != originalContent {
		t.Fatalf("replayed content does not match the original: got %q, want %q", string(entry.Content), originalContent)
	}
}
