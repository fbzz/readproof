// Package e2e programmatically runs the Refund Agent reference demo and
// asserts the SHA256(original) == SHA256(replay) invariant that is the
// spec's own v0.1 acceptance test.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ctx/internal/app"
	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/source"
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
	uri := "ctx://demo/policies/refunds"

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

	// Step 2 of the demo: a standalone `ctx get` before any run exists.
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
