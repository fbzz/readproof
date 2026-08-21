package run_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"readproof/internal/manifest"
	"readproof/internal/run"
)

// A run id that was never started has no mounts, so committing it used to
// produce a perfectly valid, perfectly empty manifest — a provenance record
// asserting "this run read nothing" about a run that never existed. It has
// to fail instead, and fail with the sentinel every caller (HTTP 404, the
// MCP error result, the CLI's non-zero exit) keys off.
func TestCommitUnknownRunFails(t *testing.T) {
	a := newDemoApp(t)
	ctx := context.Background()

	man, err := a.RunBuilder.Commit(ctx, "run-never-started")
	if err == nil {
		t.Fatalf("committing an unknown run succeeded, returning manifest %q", man.ManifestID)
	}
	if !errors.Is(err, run.ErrNotFound) {
		t.Fatalf("error = %v, want run.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "run-never-started") {
		t.Errorf("error %q does not name the run id", err)
	}
	if man.ManifestID != "" {
		t.Errorf("failed commit returned manifest %q, want the zero manifest", man.ManifestID)
	}

	// Nothing was persisted on the way out, either.
	if _, err := a.Manifests.GetByIDOrRun(ctx, "run-never-started"); !errors.Is(err, manifest.ErrNotFound) {
		t.Errorf("a manifest exists for the unknown run: %v", err)
	}
}

// A run has exactly one manifest. A second commit must not mint a second
// one: `readproof manifest run-a` and `readproof replay run-a` resolve a
// run id to a manifest, and two candidates would make that answer
// arbitrary.
func TestCommitTwiceFails(t *testing.T) {
	a := newDemoApp(t)
	ctx := context.Background()

	const runID = "run-double-commit"
	if err := a.RunBuilder.Start(ctx, runID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := a.RunBuilder.Mount(ctx, runID, refundsURI); err != nil {
		t.Fatalf("mount: %v", err)
	}
	first, err := a.RunBuilder.Commit(ctx, runID)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}

	second, err := a.RunBuilder.Commit(ctx, runID)
	if err == nil {
		t.Fatalf("second commit succeeded, minting manifest %q alongside %q", second.ManifestID, first.ManifestID)
	}
	if !errors.Is(err, run.ErrAlreadyCommitted) {
		t.Fatalf("error = %v, want run.ErrAlreadyCommitted", err)
	}
	// The error points at the manifest that already exists, so the caller
	// can go read it instead of guessing.
	if !strings.Contains(err.Error(), first.ManifestID) {
		t.Errorf("error %q does not name the existing manifest %q", err, first.ManifestID)
	}

	got, err := a.Manifests.GetByIDOrRun(ctx, runID)
	if err != nil {
		t.Fatalf("get manifest by run id: %v", err)
	}
	if got.ManifestID != first.ManifestID {
		t.Errorf("run %s now resolves to manifest %s, want %s", runID, got.ManifestID, first.ManifestID)
	}
	if len(got.Entries) != 1 {
		t.Errorf("manifest has %d entries, want 1", len(got.Entries))
	}
}

// Mounting into a run that was never started used to resolve the resource
// (possibly creating a snapshot) and write an orphan run_mounts row that no
// commit could ever pick up. The guard has to fire before the resolve so the
// resource's history stays untouched.
func TestMountUnknownRunFails(t *testing.T) {
	a := newDemoApp(t)
	ctx := context.Background()

	before, err := a.Snapshots.ListByResource(ctx, refundsURI)
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}

	_, err = a.RunBuilder.Mount(ctx, "run-never-started", refundsURI)
	if !errors.Is(err, run.ErrNotFound) {
		t.Fatalf("mount into unknown run: err = %v, want run.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "run-never-started") {
		t.Errorf("error %q does not name the run id", err)
	}

	after, err := a.Snapshots.ListByResource(ctx, refundsURI)
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("mount into unknown run created a snapshot: %d -> %d", len(before), len(after))
	}
	if mounts, _ := a.Runs.ListMounts(ctx, "run-never-started"); len(mounts) != 0 {
		t.Errorf("orphan mounts written for unknown run: %d", len(mounts))
	}
}

func TestMountCommittedRunFails(t *testing.T) {
	a := newDemoApp(t)
	ctx := context.Background()

	if err := a.RunBuilder.Start(ctx, "run-done"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := a.RunBuilder.Mount(ctx, "run-done", refundsURI); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if _, err := a.RunBuilder.Commit(ctx, "run-done"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	_, err := a.RunBuilder.Mount(ctx, "run-done", refundsURI)
	if !errors.Is(err, run.ErrAlreadyCommitted) {
		t.Fatalf("mount into committed run: err = %v, want run.ErrAlreadyCommitted", err)
	}
}
