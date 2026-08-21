package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/ids"
	"github.com/fbzz/readproof/internal/materialization"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/run"
	"github.com/fbzz/readproof/internal/snapshot"
	"github.com/fbzz/readproof/internal/source"
	"github.com/fbzz/readproof/internal/storage/sqlite"
	"github.com/fbzz/readproof/internal/tag"
)

// testDB opens a migrated SQLite database in a per-test temp dir — no
// external service, so these run on every `go test ./...`.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "readproof.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func createResource(t *testing.T, ctx context.Context, store *sqlite.ResourceStore, uri string) {
	t.Helper()
	if err := store.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "test",
		Path:      "policies/" + ids.New("p"),
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: "/tmp/x"},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("create resource %s: %v", uri, err)
	}
}

func createSnapshot(t *testing.T, ctx context.Context, store *sqlite.SnapshotStore, uri string) snapshot.Snapshot {
	t.Helper()
	snap := snapshot.Snapshot{
		SnapshotID:     ids.New("snap"),
		ResourceURI:    uri,
		SourceRevision: "rev-" + ids.New("r"),
		ContentHash:    ids.ContentHash([]byte(ids.New("content"))),
		ObservedAt:     time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
		ContentType:    "text/markdown",
		Bytes:          10,
		Provenance:     map[string]string{"source_type": "filesystem"},
	}
	if err := store.Create(ctx, snap); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	return snap
}

func TestTagStore_SetGetListDelete(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	resources := sqlite.NewResourceStore(db)
	snapshots := sqlite.NewSnapshotStore(db)
	tags := sqlite.NewTagStore(db)

	uri := "readproof://test/policies/refunds"
	createResource(t, ctx, resources, uri)
	first := createSnapshot(t, ctx, snapshots, uri)
	second := createSnapshot(t, ctx, snapshots, uri)

	if err := tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: first.SnapshotID}); err != nil {
		t.Fatalf("set tag: %v", err)
	}
	got, err := tags.Get(ctx, uri, "prod")
	if err != nil {
		t.Fatalf("get tag: %v", err)
	}
	if got.SnapshotID != first.SnapshotID || got.ResourceURI != uri || got.Name != "prod" {
		t.Fatalf("tag round-trip mismatch: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("updated_at not populated: %+v", got)
	}

	// Set is an upsert: re-pointing an existing tag must move it, not fail.
	if err := tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: second.SnapshotID}); err != nil {
		t.Fatalf("re-set tag: %v", err)
	}
	moved, err := tags.Get(ctx, uri, "prod")
	if err != nil {
		t.Fatalf("get moved tag: %v", err)
	}
	if moved.SnapshotID != second.SnapshotID {
		t.Fatalf("tag did not move: got %s, want %s", moved.SnapshotID, second.SnapshotID)
	}

	if err := tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "a-baseline", SnapshotID: first.SnapshotID}); err != nil {
		t.Fatalf("set second tag: %v", err)
	}
	list, err := tags.List(ctx, uri)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(list) != 2 || list[0].Name != "a-baseline" || list[1].Name != "prod" {
		t.Fatalf("expected tags sorted by name, got %+v", list)
	}

	if err := tags.Delete(ctx, uri, "prod"); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if _, err := tags.Get(ctx, uri, "prod"); !errors.Is(err, tag.ErrNotFound) {
		t.Fatalf("get deleted tag: got %v, want tag.ErrNotFound", err)
	}
	if err := tags.Delete(ctx, uri, "prod"); !errors.Is(err, tag.ErrNotFound) {
		t.Fatalf("delete missing tag: got %v, want tag.ErrNotFound", err)
	}
}

func TestTagStore_SetRejectsUnknownAndForeignSnapshots(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	resources := sqlite.NewResourceStore(db)
	snapshots := sqlite.NewSnapshotStore(db)
	tags := sqlite.NewTagStore(db)

	uri := "readproof://test/policies/refunds"
	otherURI := "readproof://test/policies/shipping"
	createResource(t, ctx, resources, uri)
	createResource(t, ctx, resources, otherURI)
	foreign := createSnapshot(t, ctx, snapshots, otherURI)

	err := tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: "snap_does_not_exist"})
	if !errors.Is(err, snapshot.ErrNotFound) {
		t.Fatalf("set tag on unknown snapshot: got %v, want snapshot.ErrNotFound", err)
	}

	err = tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: foreign.SnapshotID})
	if !errors.Is(err, tag.ErrSnapshotMismatch) {
		t.Fatalf("set tag on another resource's snapshot: got %v, want tag.ErrSnapshotMismatch", err)
	}
}

func TestTagStore_SetRejectsInvalidNames(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	resources := sqlite.NewResourceStore(db)
	snapshots := sqlite.NewSnapshotStore(db)
	tags := sqlite.NewTagStore(db)

	uri := "readproof://test/policies/refunds"
	createResource(t, ctx, resources, uri)
	snap := createSnapshot(t, ctx, snapshots, uri)

	for _, name := range []string{"", "-leading-dash", "has space", "has/slash", "has@at"} {
		err := tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: name, SnapshotID: snap.SnapshotID})
		if !errors.Is(err, tag.ErrInvalidName) {
			t.Fatalf("set tag %q: got %v, want tag.ErrInvalidName", name, err)
		}
	}
}

// The 0002 migration adds run_mounts.ref and manifest_entries.ref; this
// asserts the column actually round-trips through the run store.
func TestRunStore_MountRefRoundTrip(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	resources := sqlite.NewResourceStore(db)
	snapshots := sqlite.NewSnapshotStore(db)
	materializations := sqlite.NewMaterializationStore(db)
	runs := sqlite.NewRunStore(db)

	uri := "readproof://test/policies/refunds"
	createResource(t, ctx, resources, uri)
	snap := createSnapshot(t, ctx, snapshots, uri)
	mat := materializationFor(t, ctx, materializations, snap)

	runID := ids.New("run")
	if err := runs.StartRun(ctx, runID); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := runs.AppendMount(ctx, runID, runMount(0, uri, "prod", snap.SnapshotID, mat.MaterializationID, snap.ContentHash)); err != nil {
		t.Fatalf("append tagged mount: %v", err)
	}
	if err := runs.AppendMount(ctx, runID, runMount(1, uri, "", snap.SnapshotID, mat.MaterializationID, snap.ContentHash)); err != nil {
		t.Fatalf("append untagged mount: %v", err)
	}

	mounts, err := runs.ListMounts(ctx, runID)
	if err != nil {
		t.Fatalf("list mounts: %v", err)
	}
	if len(mounts) != 2 {
		t.Fatalf("len(mounts) = %d, want 2", len(mounts))
	}
	if mounts[0].Ref != "prod" {
		t.Fatalf("mount[0].Ref = %q, want %q", mounts[0].Ref, "prod")
	}
	if mounts[1].Ref != "" {
		t.Fatalf("mount[1].Ref = %q, want empty", mounts[1].Ref)
	}
}

func materializationFor(t *testing.T, ctx context.Context, store *sqlite.MaterializationStore, snap snapshot.Snapshot) materialization.Materialization {
	t.Helper()
	mat := materialization.Materialization{
		MaterializationID: ids.New("mat"),
		SnapshotID:        snap.SnapshotID,
		Strategy:          materialization.StrategyRaw,
		ContentHash:       snap.ContentHash,
		Bytes:             snap.Bytes,
		CreatedAt:         time.Now().UTC(),
	}
	if err := store.Create(ctx, mat); err != nil {
		t.Fatalf("create materialization: %v", err)
	}
	return mat
}

func runMount(position int, uri, ref, snapshotID, materializationID, contentHash string) run.MountEntry {
	return run.MountEntry{
		Position:          position,
		URI:               uri,
		Ref:               ref,
		SnapshotID:        snapshotID,
		MaterializationID: materializationID,
		ContentHash:       contentHash,
	}
}
