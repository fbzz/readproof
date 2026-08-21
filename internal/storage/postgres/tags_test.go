package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"readproof/internal/ids"
	"readproof/internal/run"
	"readproof/internal/snapshot"
	"readproof/internal/storage/postgres"
	"readproof/internal/tag"
)

// These mirror internal/storage/sqlite/tags_test.go exactly — same
// assertions, different backend — and skip themselves without
// READPROOF_TEST_POSTGRES_DSN, like every other test in this package.
func TestTagStore_SetGetListDelete(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	tags := postgres.NewTagStore(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)
	first, _ := createSnapshotAndMat(t, ctx, st, r.URI, time.Now().UTC())
	second, _ := createSnapshotAndMat(t, ctx, st, r.URI, time.Now().UTC())

	if err := tags.Set(ctx, tag.Tag{ResourceURI: r.URI, Name: "prod", SnapshotID: first.SnapshotID}); err != nil {
		t.Fatalf("set tag: %v", err)
	}
	got, err := tags.Get(ctx, r.URI, "prod")
	if err != nil {
		t.Fatalf("get tag: %v", err)
	}
	if got.SnapshotID != first.SnapshotID || got.ResourceURI != r.URI || got.Name != "prod" {
		t.Fatalf("tag round-trip mismatch: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("updated_at not populated: %+v", got)
	}

	if err := tags.Set(ctx, tag.Tag{ResourceURI: r.URI, Name: "prod", SnapshotID: second.SnapshotID}); err != nil {
		t.Fatalf("re-set tag: %v", err)
	}
	moved, err := tags.Get(ctx, r.URI, "prod")
	if err != nil {
		t.Fatalf("get moved tag: %v", err)
	}
	if moved.SnapshotID != second.SnapshotID {
		t.Fatalf("tag did not move: got %s, want %s", moved.SnapshotID, second.SnapshotID)
	}

	if err := tags.Set(ctx, tag.Tag{ResourceURI: r.URI, Name: "a-baseline", SnapshotID: first.SnapshotID}); err != nil {
		t.Fatalf("set second tag: %v", err)
	}
	list, err := tags.List(ctx, r.URI)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(list) != 2 || list[0].Name != "a-baseline" || list[1].Name != "prod" {
		t.Fatalf("expected tags sorted by name, got %+v", list)
	}

	if err := tags.Delete(ctx, r.URI, "prod"); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if _, err := tags.Get(ctx, r.URI, "prod"); !errors.Is(err, tag.ErrNotFound) {
		t.Fatalf("get deleted tag: got %v, want tag.ErrNotFound", err)
	}
	if err := tags.Delete(ctx, r.URI, "prod"); !errors.Is(err, tag.ErrNotFound) {
		t.Fatalf("delete missing tag: got %v, want tag.ErrNotFound", err)
	}
}

func TestTagStore_SetRejectsUnknownForeignAndInvalid(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	tags := postgres.NewTagStore(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)
	other := createTestResource(t, ctx, st)
	foreign, _ := createSnapshotAndMat(t, ctx, st, other.URI, time.Now().UTC())
	own, _ := createSnapshotAndMat(t, ctx, st, r.URI, time.Now().UTC())

	err := tags.Set(ctx, tag.Tag{ResourceURI: r.URI, Name: "prod", SnapshotID: "snap_does_not_exist_" + ids.New("x")})
	if !errors.Is(err, snapshot.ErrNotFound) {
		t.Fatalf("set tag on unknown snapshot: got %v, want snapshot.ErrNotFound", err)
	}

	err = tags.Set(ctx, tag.Tag{ResourceURI: r.URI, Name: "prod", SnapshotID: foreign.SnapshotID})
	if !errors.Is(err, tag.ErrSnapshotMismatch) {
		t.Fatalf("set tag on another resource's snapshot: got %v, want tag.ErrSnapshotMismatch", err)
	}

	for _, name := range []string{"", "-leading-dash", "has space", "has/slash", "has@at"} {
		if err := tags.Set(ctx, tag.Tag{ResourceURI: r.URI, Name: name, SnapshotID: own.SnapshotID}); !errors.Is(err, tag.ErrInvalidName) {
			t.Fatalf("set tag %q: got %v, want tag.ErrInvalidName", name, err)
		}
	}
}

func TestRunStore_MountRefRoundTrip(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)
	snap, mat := createSnapshotAndMat(t, ctx, st, r.URI, time.Now().UTC())

	runID := ids.New("run")
	if err := st.Runs.StartRun(ctx, runID); err != nil {
		t.Fatalf("start run: %v", err)
	}
	entries := []run.MountEntry{
		{Position: 0, URI: r.URI, Ref: "prod", SnapshotID: snap.SnapshotID, MaterializationID: mat.MaterializationID, ContentHash: mat.ContentHash},
		{Position: 1, URI: r.URI, SnapshotID: snap.SnapshotID, MaterializationID: mat.MaterializationID, ContentHash: mat.ContentHash},
	}
	for _, e := range entries {
		if err := st.Runs.AppendMount(ctx, runID, e); err != nil {
			t.Fatalf("append mount %d: %v", e.Position, err)
		}
	}

	mounts, err := st.Runs.ListMounts(ctx, runID)
	if err != nil {
		t.Fatalf("list mounts: %v", err)
	}
	if len(mounts) != 2 {
		t.Fatalf("len(mounts) = %d, want 2", len(mounts))
	}
	if mounts[0].Ref != "prod" || mounts[1].Ref != "" {
		t.Fatalf("ref column did not round-trip: %+v", mounts)
	}
}
