package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"readproof/internal/app"
	"readproof/internal/policy"
	"readproof/internal/resource"
	"readproof/internal/source"
)

// seedRun builds a data directory containing one committed run, and returns
// the data dir plus the on-disk path of the blob that run replays from.
func seedRun(t *testing.T, runID string) (dir, blobPath string) {
	t.Helper()
	dir = t.TempDir()

	fixture := filepath.Join(t.TempDir(), "refunds.md")
	if err := os.WriteFile(fixture, []byte("Products can be refunded within 30 days.\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a, err := app.Open(dir)
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	ctx := context.Background()
	uri := "readproof://demo/policies/refunds"
	if err := a.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixture},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	man, err := a.RunBuilder.Run(ctx, runID, []string{uri})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}

	hex := strings.TrimPrefix(man.Entries[0].ContentHash, "sha256:")
	return dir, filepath.Join(dir, "blobs", hex[:2], hex)
}

// runReplayCmd executes `readproof replay <target>` against dir exactly as
// the CLI would, and returns the error the command exits non-zero on.
func runReplayCmd(t *testing.T, dir, target string) error {
	t.Helper()

	// openClient reads these package-level flag targets; point them at the
	// seeded directory in embedded mode for the duration of the command.
	prevDataDir, prevServer := dataDir, serverURL
	dataDir, serverURL = dir, ""
	t.Cleanup(func() { dataDir, serverURL = prevDataDir, prevServer })

	// The command streams replayed content to stdout; keep test output clean.
	origStdout := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stdout = devnull
	defer func() {
		os.Stdout = origStdout
		devnull.Close()
	}()

	cmd := newReplayCmd()
	cmd.SetArgs([]string{target})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	return cmd.Execute()
}

func TestReplayCommandSucceedsOnIntactStore(t *testing.T) {
	dir, _ := seedRun(t, "run-a")
	if err := runReplayCmd(t, dir, "run-a"); err != nil {
		t.Fatalf("replay of an intact run must succeed, got: %v", err)
	}
}

// Replay is strict by default: a blob whose bytes no longer hash to what
// the manifest recorded is a verification failure, and `readproof replay`
// returns an error (so the process exits non-zero) rather than printing a
// mismatch and carrying on.
func TestReplayCommandFailsOnCorruptedBlob(t *testing.T) {
	dir, blobPath := seedRun(t, "run-a")

	if err := os.WriteFile(blobPath, []byte("tampered content\n"), 0o644); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}

	err := runReplayCmd(t, dir, "run-a")
	if err == nil {
		t.Fatalf("expected a non-nil error (non-zero exit) replaying a corrupted blob")
	}
	if !strings.Contains(err.Error(), "replay verification failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplayCommandFailsOnMissingBlob(t *testing.T) {
	dir, blobPath := seedRun(t, "run-a")

	if err := os.Remove(blobPath); err != nil {
		t.Fatalf("remove blob: %v", err)
	}

	err := runReplayCmd(t, dir, "run-a")
	if err == nil {
		t.Fatalf("expected a non-nil error (non-zero exit) replaying a missing blob")
	}
	if !strings.Contains(err.Error(), "load blob") {
		t.Fatalf("unexpected error: %v", err)
	}
}
