// Command ctxd is the Ctx HTTP server: it wraps the same resolution
// pipeline the CLI uses in embedded mode behind a network API, so the CLI
// (via --server) and future SDKs can talk to a shared, durable backend.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"ctx/internal/api"
	"ctx/internal/app"
)

func main() {
	addr := flag.String("addr", envOr("CTXD_ADDR", ":8080"), "address to listen on")
	dataDir := flag.String("data-dir", os.Getenv("CTXD_DATA_DIR"), "embedded-mode data directory (default: .ctx, or $CTX_HOME); ignored if --postgres-dsn is set")

	postgresDSN := flag.String("postgres-dsn", os.Getenv("CTXD_POSTGRES_DSN"), "PostgreSQL DSN; when set, ctxd runs against Postgres + S3 instead of embedded SQLite + local disk")
	s3Endpoint := flag.String("s3-endpoint", envOr("CTXD_S3_ENDPOINT", "localhost:9000"), "S3-compatible endpoint (postgres mode only)")
	s3AccessKey := flag.String("s3-access-key", os.Getenv("CTXD_S3_ACCESS_KEY"), "S3-compatible access key (postgres mode only)")
	s3SecretKey := flag.String("s3-secret-key", os.Getenv("CTXD_S3_SECRET_KEY"), "S3-compatible secret key (postgres mode only)")
	s3Bucket := flag.String("s3-bucket", envOr("CTXD_S3_BUCKET", "ctx-blobs"), "S3-compatible bucket name (postgres mode only)")
	s3UseSSL := flag.Bool("s3-use-ssl", envBool("CTXD_S3_USE_SSL", false), "use TLS for the S3-compatible endpoint (postgres mode only)")
	flag.Parse()

	a, backend, err := openApp(context.Background(), *dataDir, *postgresDSN, *s3Endpoint, *s3AccessKey, *s3SecretKey, *s3Bucket, *s3UseSSL)
	if err != nil {
		log.Fatalf("ctxd: %v", err)
	}
	defer a.Close()

	handler := api.NewHandler(a)
	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("ctxd listening on %s (backend: %s)", *addr, backend)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("ctxd: %v", err)
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
