package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/ids"
	"github.com/fbzz/readproof/internal/manifest"
	"github.com/fbzz/readproof/internal/materialization"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/run"
	"github.com/fbzz/readproof/internal/snapshot"
	"github.com/fbzz/readproof/internal/source"
	"github.com/fbzz/readproof/internal/storage/postgres"
)

// testDB skips the test unless READPROOF_TEST_POSTGRES_DSN is set, so `go
// test ./...` stays green without a live Postgres instance. Every row
// created by these tests uses ULID-based unique IDs/URIs (via
// internal/ids), so repeated runs against the same database never collide
// on unique constraints and tests never need to truncate shared state.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("READPROOF_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("READPROOF_TEST_POSTGRES_DSN not set; skipping postgres integration tests")
	}
	db, err := postgres.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := postgres.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type stores struct {
	Resources        *postgres.ResourceStore
	Snapshots        *postgres.SnapshotStore
	Materializations *postgres.MaterializationStore
	Manifests        *postgres.ManifestStore
	Runs             *postgres.RunStore
}

func newStores(db *sql.DB) stores {
	return stores{
		Resources:        postgres.NewResourceStore(db),
		Snapshots:        postgres.NewSnapshotStore(db),
		Materializations: postgres.NewMaterializationStore(db),
		Manifests:        postgres.NewManifestStore(db),
		Runs:             postgres.NewRunStore(db),
	}
}

// createTestResource inserts a fresh Resource (with its Source+Policy) under
// a unique URI and returns the row as read back from the store.
func createTestResource(t *testing.T, ctx context.Context, st stores) resource.Resource {
	t.Helper()
	suffix := ids.New("res")
	uri := "readproof://test/" + suffix
	r := resource.Resource{
		URI:       uri,
		Namespace: "test",
		Path:      suffix,
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: "/tmp/" + suffix},
		},
		Policy: policy.Policy{Strategy: policy.StrategyAllowStale, MaxAge: time.Hour},
	}
	if err := st.Resources.Create(ctx, r); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	got, err := st.Resources.Get(ctx, uri)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	return got
}

// createSnapshotAndMat inserts a Snapshot for resourceURI observed at
// observedAt, plus its raw Materialization, and returns both.
func createSnapshotAndMat(t *testing.T, ctx context.Context, st stores, resourceURI string, observedAt time.Time) (snapshot.Snapshot, materialization.Materialization) {
	t.Helper()
	snap := snapshot.Snapshot{
		SnapshotID:     ids.New("snap"),
		ResourceURI:    resourceURI,
		SourceRevision: "rev-" + ids.New("r"),
		ContentHash:    ids.ContentHash([]byte(ids.New("content"))),
		ObservedAt:     observedAt,
		CreatedAt:      time.Now().UTC(),
		ContentType:    "text/plain",
		Bytes:          42,
		Provenance:     map[string]string{"source": "test"},
	}
	if err := st.Snapshots.Create(ctx, snap); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	mat := materialization.Materialization{
		MaterializationID: ids.New("mat"),
		SnapshotID:        snap.SnapshotID,
		Strategy:          materialization.StrategyRaw,
		ContentHash:       snap.ContentHash,
		Bytes:             snap.Bytes,
		CreatedAt:         time.Now().UTC(),
	}
	if err := st.Materializations.Create(ctx, mat); err != nil {
		t.Fatalf("create materialization: %v", err)
	}
	return snap, mat
}

func TestResourceStore_CreateGetUpdateList(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)

	if r.SourceConfig.Kind != source.KindFilesystem {
		t.Fatalf("source config kind = %q, want filesystem", r.SourceConfig.Kind)
	}
	if r.SourceConfig.Filesystem == nil || r.SourceConfig.Filesystem.Path == "" {
		t.Fatalf("filesystem config not round-tripped: %+v", r.SourceConfig)
	}
	if r.Policy.Strategy != policy.StrategyAllowStale || r.Policy.MaxAge != time.Hour {
		t.Fatalf("policy not round-tripped: %+v", r.Policy)
	}
	if r.CurrentSnapshotID != "" {
		t.Fatalf("current_snapshot_id = %q, want empty before first resolve", r.CurrentSnapshotID)
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not populated: %+v", r)
	}

	snap, _ := createSnapshotAndMat(t, ctx, st, r.URI, time.Now().UTC())
	if err := st.Resources.UpdateCurrentSnapshot(ctx, r.URI, snap.SnapshotID); err != nil {
		t.Fatalf("update current snapshot: %v", err)
	}
	updated, err := st.Resources.Get(ctx, r.URI)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if updated.CurrentSnapshotID != snap.SnapshotID {
		t.Fatalf("current_snapshot_id = %q, want %q", updated.CurrentSnapshotID, snap.SnapshotID)
	}
	if !updated.UpdatedAt.After(r.UpdatedAt) && !updated.UpdatedAt.Equal(r.UpdatedAt) {
		t.Fatalf("updated_at did not advance: before=%v after=%v", r.UpdatedAt, updated.UpdatedAt)
	}

	all, err := st.Resources.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, x := range all {
		if x.URI == r.URI {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("list did not include %q", r.URI)
	}

	if _, err := st.Resources.Get(ctx, "readproof://test/does-not-exist-"+ids.New("x")); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("get missing resource: got err %v, want resource.ErrNotFound", err)
	}
	if err := st.Resources.UpdateCurrentSnapshot(ctx, "readproof://test/does-not-exist-"+ids.New("x"), snap.SnapshotID); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("update missing resource: got err %v, want resource.ErrNotFound", err)
	}
}

func TestSnapshotStore_CreateGetListByResourceOrdering(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)

	base := time.Now().UTC().Truncate(time.Second)
	var created []snapshot.Snapshot
	for i := 0; i < 3; i++ {
		snap, _ := createSnapshotAndMat(t, ctx, st, r.URI, base.Add(time.Duration(i)*time.Second))
		created = append(created, snap)
	}

	got, err := st.Snapshots.Get(ctx, created[0].SnapshotID)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if got.ContentHash != created[0].ContentHash || got.ResourceURI != r.URI {
		t.Fatalf("get snapshot mismatch: %+v vs %+v", got, created[0])
	}
	if got.Provenance["source"] != "test" {
		t.Fatalf("provenance not round-tripped: %+v", got.Provenance)
	}
	if !got.ObservedAt.Equal(created[0].ObservedAt) {
		t.Fatalf("observed_at mismatch: got %v want %v", got.ObservedAt, created[0].ObservedAt)
	}

	list, err := st.Snapshots.ListByResource(ctx, r.URI)
	if err != nil {
		t.Fatalf("list by resource: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(list))
	}
	// Newest-first.
	for i, want := range []snapshot.Snapshot{created[2], created[1], created[0]} {
		if list[i].SnapshotID != want.SnapshotID {
			t.Fatalf("list[%d] = %s, want %s (newest-first order)", i, list[i].SnapshotID, want.SnapshotID)
		}
	}
}

func TestMaterializationStore_CreateGetDedup(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)
	snap, mat := createSnapshotAndMat(t, ctx, st, r.URI, time.Now().UTC())

	got, err := st.Materializations.Get(ctx, mat.MaterializationID)
	if err != nil {
		t.Fatalf("get materialization: %v", err)
	}
	if got.SnapshotID != snap.SnapshotID || got.Strategy != materialization.StrategyRaw || got.ContentHash != mat.ContentHash {
		t.Fatalf("get materialization mismatch: %+v vs %+v", got, mat)
	}

	dedup, found, err := st.Materializations.GetBySnapshot(ctx, snap.SnapshotID, materialization.StrategyRaw)
	if err != nil {
		t.Fatalf("get by snapshot: %v", err)
	}
	if !found {
		t.Fatalf("expected dedup lookup to find existing materialization")
	}
	if dedup.MaterializationID != mat.MaterializationID {
		t.Fatalf("dedup lookup returned %s, want %s", dedup.MaterializationID, mat.MaterializationID)
	}

	// A snapshot with no materialization at all: GetBySnapshot must report
	// not-found without error, not raise sql.ErrNoRows.
	otherSnap := snapshot.Snapshot{
		SnapshotID:     ids.New("snap"),
		ResourceURI:    r.URI,
		SourceRevision: "rev-" + ids.New("r"),
		ContentHash:    ids.ContentHash([]byte(ids.New("content"))),
		ObservedAt:     time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
		ContentType:    "text/plain",
		Bytes:          7,
		Provenance:     map[string]string{},
	}
	if err := st.Snapshots.Create(ctx, otherSnap); err != nil {
		t.Fatalf("create other snapshot: %v", err)
	}
	_, found, err = st.Materializations.GetBySnapshot(ctx, otherSnap.SnapshotID, materialization.StrategyRaw)
	if err != nil {
		t.Fatalf("get by snapshot (absent): %v", err)
	}
	if found {
		t.Fatalf("expected dedup lookup to report not-found for a snapshot with no materialization")
	}
}

func TestManifestStore_CreateGetByIDOrRun(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)

	var entries []manifest.Entry
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		snap, mat := createSnapshotAndMat(t, ctx, st, r.URI, base.Add(time.Duration(i)*time.Second))
		entries = append(entries, manifest.Entry{
			Position:          i,
			URI:               r.URI,
			SnapshotID:        snap.SnapshotID,
			MaterializationID: mat.MaterializationID,
			ContentHash:       mat.ContentHash,
		})
	}

	runID := ids.New("run")
	man := manifest.Manifest{
		ManifestID: ids.New("manifest"),
		RunID:      runID,
		CreatedAt:  time.Now().UTC(),
		Entries:    entries,
	}
	if err := st.Manifests.Create(ctx, man); err != nil {
		t.Fatalf("create manifest: %v", err)
	}

	assertEntryOrder := func(t *testing.T, got manifest.Manifest) {
		t.Helper()
		if len(got.Entries) != len(entries) {
			t.Fatalf("len(entries) = %d, want %d", len(got.Entries), len(entries))
		}
		for i, e := range got.Entries {
			if e.Position != entries[i].Position || e.SnapshotID != entries[i].SnapshotID ||
				e.MaterializationID != entries[i].MaterializationID || e.ContentHash != entries[i].ContentHash {
				t.Fatalf("entry[%d] = %+v, want %+v", i, e, entries[i])
			}
		}
	}

	byID, err := st.Manifests.Get(ctx, man.ManifestID)
	if err != nil {
		t.Fatalf("get by manifest id: %v", err)
	}
	assertEntryOrder(t, byID)

	byIDFallback, err := st.Manifests.GetByIDOrRun(ctx, man.ManifestID)
	if err != nil {
		t.Fatalf("get by id or run (manifest id branch): %v", err)
	}
	assertEntryOrder(t, byIDFallback)

	byRun, err := st.Manifests.GetByIDOrRun(ctx, runID)
	if err != nil {
		t.Fatalf("get by id or run (run id fallback branch): %v", err)
	}
	if byRun.ManifestID != man.ManifestID {
		t.Fatalf("resolved manifest id = %s, want %s", byRun.ManifestID, man.ManifestID)
	}
	assertEntryOrder(t, byRun)

	if _, err := st.Manifests.GetByIDOrRun(ctx, "no-such-id-or-run-"+ids.New("x")); !errors.Is(err, postgres.ErrManifestNotFound) {
		t.Fatalf("get by id or run (not found): got err %v, want ErrManifestNotFound", err)
	}
}

func TestRunStore_StartAppendListCommit(t *testing.T) {
	db := testDB(t)
	st := newStores(db)
	ctx := context.Background()

	r := createTestResource(t, ctx, st)
	runID := ids.New("run")

	if err := st.Runs.StartRun(ctx, runID); err != nil {
		t.Fatalf("start run: %v", err)
	}

	open, err := st.Runs.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run (open): %v", err)
	}
	if open.Status != run.StatusOpen {
		t.Fatalf("status = %q, want open", open.Status)
	}
	if open.CommittedAt != nil {
		t.Fatalf("committed_at set on open run: %v", open.CommittedAt)
	}
	if open.ManifestID != "" {
		t.Fatalf("manifest_id set on open run: %q", open.ManifestID)
	}

	var mounts []run.MountEntry
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 2; i++ {
		snap, mat := createSnapshotAndMat(t, ctx, st, r.URI, base.Add(time.Duration(i)*time.Second))
		e := run.MountEntry{
			Position:          i,
			URI:               r.URI,
			SnapshotID:        snap.SnapshotID,
			MaterializationID: mat.MaterializationID,
			ContentHash:       mat.ContentHash,
		}
		if err := st.Runs.AppendMount(ctx, runID, e); err != nil {
			t.Fatalf("append mount %d: %v", i, err)
		}
		mounts = append(mounts, e)
	}

	listed, err := st.Runs.ListMounts(ctx, runID)
	if err != nil {
		t.Fatalf("list mounts: %v", err)
	}
	if len(listed) != len(mounts) {
		t.Fatalf("len(listed) = %d, want %d", len(listed), len(mounts))
	}
	for i, e := range listed {
		if e.Position != mounts[i].Position || e.SnapshotID != mounts[i].SnapshotID || e.MaterializationID != mounts[i].MaterializationID {
			t.Fatalf("mount[%d] = %+v, want %+v", i, e, mounts[i])
		}
	}

	manEntries := make([]manifest.Entry, len(mounts))
	for i, m := range mounts {
		manEntries[i] = manifest.Entry{
			Position:          m.Position,
			URI:               m.URI,
			SnapshotID:        m.SnapshotID,
			MaterializationID: m.MaterializationID,
			ContentHash:       m.ContentHash,
		}
	}
	man := manifest.Manifest{
		ManifestID: ids.New("manifest"),
		RunID:      runID,
		CreatedAt:  time.Now().UTC(),
		Entries:    manEntries,
	}
	if err := st.Manifests.Create(ctx, man); err != nil {
		t.Fatalf("create manifest for run: %v", err)
	}

	if err := st.Runs.MarkCommitted(ctx, runID, man.ManifestID); err != nil {
		t.Fatalf("mark committed: %v", err)
	}

	committed, err := st.Runs.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run (committed): %v", err)
	}
	if committed.Status != run.StatusCommitted {
		t.Fatalf("status = %q, want committed", committed.Status)
	}
	if committed.CommittedAt == nil {
		t.Fatalf("committed_at not set after MarkCommitted")
	}
	if committed.ManifestID != man.ManifestID {
		t.Fatalf("manifest_id = %q, want %q", committed.ManifestID, man.ManifestID)
	}

	if _, err := st.Runs.GetRun(ctx, "no-such-run-"+ids.New("x")); !errors.Is(err, postgres.ErrRunNotFound) {
		t.Fatalf("get missing run: got err %v, want ErrRunNotFound", err)
	}
}
