package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"ctx/internal/manifest"
	"ctx/internal/materialization"
	"ctx/internal/replay"
	"ctx/internal/resolver"
	"ctx/internal/resource"
	"ctx/internal/run"
	"ctx/internal/snapshot"
	"ctx/internal/source"
	fsSource "ctx/internal/source/filesystem"
	ghSource "ctx/internal/source/github"
	"ctx/internal/storage/blob"
	"ctx/internal/storage/sqlite"
)

// App is the composition root wiring every store, the resolver, the run
// builder, and the replayer together over a single local data directory.
type App struct {
	DB               *sql.DB
	Resources        resource.Store
	Snapshots        snapshot.Store
	Materializations materialization.Store
	Manifests        manifest.Store
	Runs             run.RunStore
	Blobs            blob.Store
	Resolver         *resolver.Resolver
	RunBuilder       *run.Builder
	Replayer         *replay.Replayer
}

// DataDir resolves the default local data directory: $CTX_HOME, or ".ctx".
func DataDir() string {
	if dir := os.Getenv("CTX_HOME"); dir != "" {
		return dir
	}
	return ".ctx"
}

// Open sets up (creating and migrating if needed) the local SQLite database
// and blob store under dataDir, and wires the full pipeline over them.
func Open(dataDir string) (*App, error) {
	if dataDir == "" {
		dataDir = DataDir()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("app: create data dir: %w", err)
	}

	db, err := sqlite.Open(filepath.Join(dataDir, "ctx.db"))
	if err != nil {
		return nil, err
	}
	if err := sqlite.Migrate(db); err != nil {
		return nil, err
	}

	blobStore := blob.NewLocalStore(filepath.Join(dataDir, "blobs"))
	resources := sqlite.NewResourceStore(db)
	snapshots := sqlite.NewSnapshotStore(db)
	materializations := sqlite.NewMaterializationStore(db)
	manifests := sqlite.NewManifestStore(db)
	runs := sqlite.NewRunStore(db)

	sources := source.NewRegistry()
	sources.Register(source.KindFilesystem, fsSource.New())
	sources.Register(source.KindGitHub, ghSource.New())

	res := &resolver.Resolver{
		Resources:        resources,
		Snapshots:        snapshots,
		Materializations: materializations,
		Blobs:            blobStore,
		Sources:          sources,
		Materializer:     materialization.RawMaterializer{},
	}

	builder := &run.Builder{
		Runs:      runs,
		Manifests: manifests,
		Resolver:  res,
	}

	replayer := &replay.Replayer{
		Manifests:        manifests,
		Materializations: materializations,
		Blobs:            blobStore,
	}

	return &App{
		DB:               db,
		Resources:        resources,
		Snapshots:        snapshots,
		Materializations: materializations,
		Manifests:        manifests,
		Runs:             runs,
		Blobs:            blobStore,
		Resolver:         res,
		RunBuilder:       builder,
		Replayer:         replayer,
	}, nil
}

func (a *App) Close() error {
	return a.DB.Close()
}
