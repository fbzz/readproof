package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fbzz/readproof/internal/manifest"
	"github.com/fbzz/readproof/internal/materialization"
	"github.com/fbzz/readproof/internal/replay"
	"github.com/fbzz/readproof/internal/resolver"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/run"
	"github.com/fbzz/readproof/internal/snapshot"
	"github.com/fbzz/readproof/internal/source"
	fsSource "github.com/fbzz/readproof/internal/source/filesystem"
	ghSource "github.com/fbzz/readproof/internal/source/github"
	httpSource "github.com/fbzz/readproof/internal/source/http"
	"github.com/fbzz/readproof/internal/storage/blob"
	"github.com/fbzz/readproof/internal/storage/postgres"
	"github.com/fbzz/readproof/internal/storage/s3blob"
	"github.com/fbzz/readproof/internal/storage/sqlite"
	"github.com/fbzz/readproof/internal/tag"
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
	Tags             tag.Store
	Blobs            blob.Store
	Resolver         *resolver.Resolver
	RunBuilder       *run.Builder
	Replayer         *replay.Replayer
}

// DataDir resolves the default local data directory: $READPROOF_HOME, or
// ".readproof".
func DataDir() string {
	if dir := os.Getenv("READPROOF_HOME"); dir != "" {
		return dir
	}
	return ".readproof"
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

	db, err := sqlite.Open(filepath.Join(dataDir, "readproof.db"))
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
	tags := sqlite.NewTagStore(db)

	return wire(db, resources, snapshots, materializations, manifests, runs, tags, blobStore), nil
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
	tags := postgres.NewTagStore(db)

	return wire(db, resources, snapshots, materializations, manifests, runs, tags, blobStore), nil
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
	tags tag.Store,
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
		Tags:             tags,
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
		Tags:             tags,
		Blobs:            blobStore,
		Resolver:         res,
		RunBuilder:       builder,
		Replayer:         replayer,
	}
}

func (a *App) Close() error {
	return a.DB.Close()
}
