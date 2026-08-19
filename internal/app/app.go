package app

import (
	"context"
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
	httpSource "ctx/internal/source/http"
	"ctx/internal/storage/blob"
	"ctx/internal/storage/postgres"
	"ctx/internal/storage/s3blob"
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

	return wire(db, resources, snapshots, materializations, manifests, runs, blobStore), nil
}

// PostgresConfig selects the Postgres metadata backend for OpenPostgres.
type PostgresConfig struct {
	DSN string
}

// S3Config selects the S3-compatible blob backend for OpenPostgres.
type S3Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	UseSSL          bool
}

// OpenPostgres wires the same pipeline as Open, but backed by PostgreSQL
// metadata storage and an S3-compatible (e.g. MinIO) blob store, instead of
// embedded SQLite and local disk. Every store is behind the same domain
// interfaces, so this is a drop-in swap — callers see the same *App shape.
func OpenPostgres(ctx context.Context, pg PostgresConfig, s3 S3Config) (*App, error) {
	db, err := postgres.Open(pg.DSN)
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(db); err != nil {
		return nil, err
	}

	blobStore, err := s3blob.New(ctx, s3blob.Config{
		Endpoint:        s3.Endpoint,
		AccessKeyID:     s3.AccessKeyID,
		SecretAccessKey: s3.SecretAccessKey,
		Bucket:          s3.Bucket,
		UseSSL:          s3.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	resources := postgres.NewResourceStore(db)
	snapshots := postgres.NewSnapshotStore(db)
	materializations := postgres.NewMaterializationStore(db)
	manifests := postgres.NewManifestStore(db)
	runs := postgres.NewRunStore(db)

	return wire(db, resources, snapshots, materializations, manifests, runs, blobStore), nil
}

// wire assembles the resolver, run builder, and replayer over an arbitrary
// set of store implementations — the same wiring regardless of backend.
func wire(
	db *sql.DB,
	resources resource.Store,
	snapshots snapshot.Store,
	materializations materialization.Store,
	manifests manifest.Store,
	runs run.RunStore,
	blobStore blob.Store,
) *App {
	sources := source.NewRegistry()
	sources.Register(source.KindFilesystem, fsSource.New())
	sources.Register(source.KindGitHub, ghSource.New())
	sources.Register(source.KindHTTP, httpSource.New())

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
	}
}

func (a *App) Close() error {
	return a.DB.Close()
}
