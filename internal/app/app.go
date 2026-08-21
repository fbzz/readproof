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
	Sources          *source.Registry
	Resolver         *resolver.Resolver
	RunBuilder       *run.Builder
	Replayer         *replay.Replayer
}

// Options carries the policy the source adapters are built with — the part of
// the pipeline whose inputs come from whoever registers a resource, and
// therefore the part whose defaults decide what "can register a resource"
// means.
//
// The zero value is the embedded `readproof` CLI's policy: unrestricted, which
// is correct there because the files, the environment and the person typing the
// command are one trust domain. `readproofd` passes ServerOptions plus whatever
// the operator explicitly opted into, because on a server the same code is a
// file-read and environment-read primitive reachable over the network.
// See docs/security-audit-2026-08.md (RP-01, RP-02, RP-04).
type Options struct {
	// RestrictFilesystem denies every filesystem source that is not inside
	// FilesystemRoots — and, with no roots at all, denies them outright.
	RestrictFilesystem bool
	// FilesystemRoots is the allow-list of directories a filesystem source
	// may read from. Only consulted when RestrictFilesystem is set.
	FilesystemRoots []string

	// DenyPrivateHTTPTargets refuses HTTP sources whose target resolves to a
	// loopback, link-local, private, or otherwise non-public address —
	// checked on every redirect hop and at dial time, so neither a redirect
	// nor a DNS rebind gets around it.
	DenyPrivateHTTPTargets bool

	// RestrictHeaderEnv expands "${VAR}" in an HTTP source header only for
	// names in HeaderEnvAllowlist. With an empty allow-list, no "${VAR}"
	// reference expands at all.
	RestrictHeaderEnv bool
	// HeaderEnvAllowlist names the environment variables an HTTP source
	// header may reference. Only consulted when RestrictHeaderEnv is set.
	HeaderEnvAllowlist []string
}

// ServerOptions is the default-deny policy `readproofd` starts from: no
// filesystem root, no environment variable expandable in a source header, no
// private network target. Each is relaxed by an explicit flag.
func ServerOptions() Options {
	return Options{
		RestrictFilesystem:     true,
		DenyPrivateHTTPTargets: true,
		RestrictHeaderEnv:      true,
	}
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
// and blob store under dataDir, and wires the full pipeline over them with the
// unrestricted, embedded-CLI source policy. Use OpenWithOptions to pass one.
func Open(dataDir string) (*App, error) {
	return OpenWithOptions(dataDir, Options{})
}

// OpenWithOptions is Open with an explicit source policy.
func OpenWithOptions(dataDir string, opts Options) (*App, error) {
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

	return wire(db, resources, snapshots, materializations, manifests, runs, tags, blobStore, opts)
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
	return OpenPostgresWithOptions(ctx, pg, s3, Options{})
}

// OpenPostgresWithOptions is OpenPostgres with an explicit source policy.
func OpenPostgresWithOptions(ctx context.Context, pg PostgresConfig, s3 S3Config, opts Options) (*App, error) {
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

	return wire(db, resources, snapshots, materializations, manifests, runs, tags, blobStore, opts)
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
	opts Options,
) (*App, error) {
	fsFetcher, err := newFilesystemFetcher(opts)
	if err != nil {
		return nil, err
	}

	sources := source.NewRegistry()
	sources.Register(source.KindFilesystem, fsFetcher)
	sources.Register(source.KindGitHub, ghSource.New())
	sources.Register(source.KindHTTP, httpSource.NewWithOptions(httpSource.Options{
		RestrictEnv:        opts.RestrictHeaderEnv,
		EnvAllowlist:       opts.HeaderEnvAllowlist,
		DenyPrivateTargets: opts.DenyPrivateHTTPTargets,
	}))

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
		Sources:          sources,
		Resolver:         res,
		RunBuilder:       builder,
		Replayer:         replayer,
	}, nil
}

// newFilesystemFetcher builds the filesystem adapter for a policy: restricted
// to an allow-list of roots for a server, unrestricted for the embedded CLI.
func newFilesystemFetcher(opts Options) (source.Fetcher, error) {
	if !opts.RestrictFilesystem {
		return fsSource.New(), nil
	}
	f, err := fsSource.NewRestricted(opts.FilesystemRoots)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return f, nil
}

func (a *App) Close() error {
	return a.DB.Close()
}
