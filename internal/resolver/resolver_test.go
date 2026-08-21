package resolver_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"readproof/internal/materialization"
	"readproof/internal/policy"
	"readproof/internal/resolver"
	"readproof/internal/resource"
	"readproof/internal/source"
	fsSource "readproof/internal/source/filesystem"
	"readproof/internal/storage/blob"
	"readproof/internal/storage/sqlite"
)

func newTestResolver(t *testing.T) (*resolver.Resolver, string) {
	t.Helper()
	dir := t.TempDir()

	db, err := sqlite.Open(filepath.Join(dir, "readproof.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	blobsDir := filepath.Join(dir, "blobs")
	blobStore := blob.NewLocalStore(blobsDir)
	sources := source.NewRegistry()
	sources.Register(source.KindFilesystem, fsSource.New())

	return &resolver.Resolver{
		Resources:        sqlite.NewResourceStore(db),
		Snapshots:        sqlite.NewSnapshotStore(db),
		Materializations: sqlite.NewMaterializationStore(db),
		Tags:             sqlite.NewTagStore(db),
		Blobs:            blobStore,
		Sources:          sources,
		Materializer:     materialization.RawMaterializer{},
	}, blobsDir
}

func countBlobFiles(t *testing.T, dir string) int {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0
	}
	var files int
	if err := fs.WalkDir(os.DirFS(dir), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk blob dir: %v", err)
	}
	return files
}

func TestResolveDedupAndChangeDetection(t *testing.T) {
	res, blobsDir := newTestResolver(t)
	ctx := context.Background()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "refunds.md")
	if err := os.WriteFile(filePath, []byte("Products can be refunded within 30 days.\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	uri := "readproof://demo/policies/refunds"
	err := res.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: filePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	first, err := res.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	second, err := res.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}

	if first.Snapshot.SnapshotID == second.Snapshot.SnapshotID {
		t.Fatalf("expected a new snapshot id on the second resolve under require_fresh")
	}
	if first.Snapshot.ContentHash != second.Snapshot.ContentHash {
		t.Fatalf("expected identical content hash for unchanged content, got %s vs %s", first.Snapshot.ContentHash, second.Snapshot.ContentHash)
	}
	if first.Materialization.MaterializationID == second.Materialization.MaterializationID {
		t.Fatalf("expected a new materialization row per new snapshot id, got the same id %s", first.Materialization.MaterializationID)
	}
	if first.Materialization.ContentHash != second.Materialization.ContentHash {
		t.Fatalf("expected identical materialization content hash for unchanged content (blob dedup), got %s vs %s",
			first.Materialization.ContentHash, second.Materialization.ContentHash)
	}

	history, err := res.Snapshots.ListByResource(ctx, uri)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 snapshot rows, got %d", len(history))
	}
	if got := countBlobFiles(t, blobsDir); got != 1 {
		t.Fatalf("expected exactly 1 blob file on disk for unchanged content, got %d", got)
	}

	if err := os.WriteFile(filePath, []byte("Products can be refunded within 14 days.\n"), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}
	third, err := res.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("resolve 3: %v", err)
	}
	if third.Snapshot.ContentHash == second.Snapshot.ContentHash {
		t.Fatalf("expected content hash to change after editing the source")
	}

	history, err = res.Snapshots.ListByResource(ctx, uri)
	if err != nil {
		t.Fatalf("list history after edit: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 snapshot rows after edit, got %d", len(history))
	}
	if got := countBlobFiles(t, blobsDir); got != 2 {
		t.Fatalf("expected exactly 2 distinct blob files on disk after the edit, got %d", got)
	}
}
