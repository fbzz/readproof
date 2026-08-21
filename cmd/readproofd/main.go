// Command readproofd is the Readproof HTTP server: it wraps the same
// resolution pipeline the CLI uses in embedded mode behind a network API,
// so the CLI (via --server) and future SDKs can talk to a shared, durable
// backend.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/fbzz/readproof/internal/api"
	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/telemetry"
	"github.com/fbzz/readproof/internal/version"
)

func main() {
	addr := flag.String("addr", envOr("READPROOFD_ADDR", ":8080"), "address to listen on")
	dataDir := flag.String("data-dir", os.Getenv("READPROOFD_DATA_DIR"), "embedded-mode data directory (default: .readproof, or $READPROOF_HOME); ignored if --postgres-dsn is set")

	postgresDSN := flag.String("postgres-dsn", os.Getenv("READPROOFD_POSTGRES_DSN"), "PostgreSQL DSN; when set, readproofd runs against Postgres + S3 instead of embedded SQLite + local disk")
	s3Endpoint := flag.String("s3-endpoint", envOr("READPROOFD_S3_ENDPOINT", "localhost:9000"), "S3-compatible endpoint (postgres mode only)")
	s3AccessKey := flag.String("s3-access-key", os.Getenv("READPROOFD_S3_ACCESS_KEY"), "S3-compatible access key (postgres mode only)")
	s3SecretKey := flag.String("s3-secret-key", os.Getenv("READPROOFD_S3_SECRET_KEY"), "S3-compatible secret key (postgres mode only)")
	s3Bucket := flag.String("s3-bucket", envOr("READPROOFD_S3_BUCKET", "readproof-blobs"), "S3-compatible bucket name (postgres mode only)")
	s3UseSSL := flag.Bool("s3-use-ssl", envBool("READPROOFD_S3_USE_SSL", false), "use TLS for the S3-compatible endpoint (postgres mode only)")
	apiKey := flag.String("api-key", os.Getenv("READPROOFD_API_KEY"), "if set, require this value as a Bearer token on every request except /healthz (off by default)")
	showVersion := flag.Bool("version", false, "print the readproofd version and exit")
	flag.Parse()

	// `readproofd version` as well as `readproofd --version`: readproofd takes
	// no positional arguments otherwise, and both spellings get typed.
	if *showVersion || flag.Arg(0) == "version" {
		fmt.Printf("readproofd %s\n", version.String())
		return
	}

	ctx := context.Background()
	shutdownTelemetry, err := telemetry.Init(ctx, "readproofd")
	if err != nil {
		log.Fatalf("readproofd: telemetry: %v", err)
	}
	defer shutdownTelemetry(ctx)

	a, backend, err := openApp(ctx, *dataDir, *postgresDSN, *s3Endpoint, *s3AccessKey, *s3SecretKey, *s3Bucket, *s3UseSSL)
	if err != nil {
		log.Fatalf("readproofd: %v", err)
	}
	defer a.Close()

	handler := api.NewHandler(a, api.Options{APIKey: *apiKey})
	// Every timeout is set, not just the header one: a peer that opens a
	// connection and then dribbles (or never reads the response) otherwise
	// pins a goroutine and a file descriptor indefinitely. WriteTimeout is
	// the loosest because a replay or diff response is assembled from the
	// blob store and can legitimately take a few seconds.
	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	authNote := "no API key set — unauthenticated"
	if *apiKey != "" {
		authNote = "API key required"
	}
	log.Printf("readproofd listening on %s (backend: %s, %s)", *addr, backend, authNote)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("readproofd: %v", err)
	}
}

func openApp(ctx context.Context, dataDir, postgresDSN, s3Endpoint, s3AccessKey, s3SecretKey, s3Bucket string, s3UseSSL bool) (*app.App, string, error) {
	if postgresDSN != "" {
		a, err := app.OpenPostgres(ctx,
			app.PostgresConfig{DSN: postgresDSN},
			app.S3Config{
				Endpoint:        s3Endpoint,
				AccessKeyID:     s3AccessKey,
				SecretAccessKey: s3SecretKey,
				Bucket:          s3Bucket,
				UseSSL:          s3UseSSL,
			},
		)
		if err != nil {
			return nil, "", fmt.Errorf("open postgres backend: %w", err)
		}
		return a, "postgres+s3", nil
	}

	a, err := app.Open(dataDir)
	if err != nil {
		return nil, "", fmt.Errorf("open embedded backend: %w", err)
	}
	return a, "sqlite+local", nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "TRUE"
}
