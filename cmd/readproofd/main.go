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
	"net/url"
	"os"
	"strings"
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

	// Registering a resource names a path, a URL and a set of headers that
	// readproofd will then read, connect to, and expand out of its own
	// environment. On a server that is a file-read and environment-read
	// primitive reachable over the network, so all three default to deny and
	// each is relaxed by an explicit flag. See docs/security-audit-2026-08.md.
	filesystemRoots := stringList(splitPathList(os.Getenv("READPROOFD_FILESYSTEM_ROOTS")))
	flag.Var(&filesystemRoots, "filesystem-root", "directory a filesystem source may read from; repeatable (env READPROOFD_FILESYSTEM_ROOTS, separated by ',' or the OS path separator). No root configured = filesystem sources are refused")
	headerEnvAllow := stringList(splitList(os.Getenv("READPROOFD_HEADER_ENV_ALLOWLIST")))
	flag.Var(&headerEnvAllow, "header-env-allow", "environment variable an http source header may reference as ${VAR}; repeatable (env READPROOFD_HEADER_ENV_ALLOWLIST, comma-separated). Nothing allow-listed = no ${VAR} expands")
	allowPrivateSources := flag.Bool("allow-private-sources", envBool("READPROOFD_ALLOW_PRIVATE_SOURCES", false), "let http sources reach loopback, link-local and private addresses (off by default; turn on only on a trusted network)")

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

	opts := app.ServerOptions()
	opts.FilesystemRoots = filesystemRoots
	opts.HeaderEnvAllowlist = headerEnvAllow
	opts.DenyPrivateHTTPTargets = !*allowPrivateSources

	if *postgresDSN != "" {
		log.Printf("readproofd: connecting to postgres %s", describeDSN(*postgresDSN))
	}
	a, backend, err := openApp(ctx, *dataDir, *postgresDSN, *s3Endpoint, *s3AccessKey, *s3SecretKey, *s3Bucket, *s3UseSSL, opts)
	if err != nil {
		// pgx quotes the connection string in some failures, and the
		// connection string carries the password — so the startup error is
		// scrubbed before it reaches the log.
		log.Fatalf("readproofd: %s", scrubDSN(err.Error(), *postgresDSN))
	}
	defer a.Close()

	logSourcePolicy(opts)

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

func openApp(ctx context.Context, dataDir, postgresDSN, s3Endpoint, s3AccessKey, s3SecretKey, s3Bucket string, s3UseSSL bool, opts app.Options) (*app.App, string, error) {
	if postgresDSN != "" {
		a, err := app.OpenPostgresWithOptions(ctx,
			app.PostgresConfig{DSN: postgresDSN},
			app.S3Config{
				Endpoint:        s3Endpoint,
				AccessKeyID:     s3AccessKey,
				SecretAccessKey: s3SecretKey,
				Bucket:          s3Bucket,
				UseSSL:          s3UseSSL,
			},
			opts,
		)
		if err != nil {
			return nil, "", fmt.Errorf("open postgres backend: %w", err)
		}
		return a, "postgres+s3", nil
	}

	a, err := app.OpenWithOptions(dataDir, opts)
	if err != nil {
		return nil, "", fmt.Errorf("open embedded backend: %w", err)
	}
	return a, "sqlite+local", nil
}

// logSourcePolicy states, at startup, exactly what a registered resource is
// allowed to reach. An operator should never have to resolve a resource to
// find out which of these is on.
func logSourcePolicy(opts app.Options) {
	if len(opts.FilesystemRoots) == 0 {
		log.Printf("readproofd: filesystem sources are refused (no --filesystem-root configured)")
	} else {
		log.Printf("readproofd: filesystem sources restricted to: %s", strings.Join(opts.FilesystemRoots, ", "))
	}
	if len(opts.HeaderEnvAllowlist) == 0 {
		log.Printf("readproofd: ${VAR} expansion in source headers is refused (no --header-env-allow configured)")
	} else {
		log.Printf("readproofd: ${VAR} expansion in source headers allowed for: %s", strings.Join(opts.HeaderEnvAllowlist, ", "))
	}
	if opts.DenyPrivateHTTPTargets {
		log.Printf("readproofd: http sources may not reach loopback, link-local or private addresses")
	} else {
		log.Printf("readproofd: http sources MAY reach private addresses (--allow-private-sources)")
	}
}

// describeDSN renders a Postgres DSN as the only parts worth logging: the host
// it points at and the database name. Never the user, and never the password.
func describeDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Host == "" {
		return "(dsn set; host not parseable)"
	}
	database := strings.TrimPrefix(parsed.Path, "/")
	if database == "" {
		database = "(default)"
	}
	return fmt.Sprintf("host=%s db=%s", parsed.Host, database)
}

// scrubDSN removes a DSN, and the password inside it, from text. Both are
// removed because a driver may quote the whole connection string in one error
// and only the credential in another, and the encoded form in the URL differs
// from the decoded one a driver may print.
func scrubDSN(text, dsn string) string {
	if dsn == "" {
		return text
	}
	text = strings.ReplaceAll(text, dsn, "[REDACTED DSN]")
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil {
		return text
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return text
	}
	for _, form := range []string{password, url.QueryEscape(password), url.PathEscape(password)} {
		text = strings.ReplaceAll(text, form, "[REDACTED]")
	}
	return text
}

// stringList is a repeatable string flag: each --flag occurrence appends.
// Pre-seeding it from an environment variable makes the flag the override,
// which is the direction operators expect.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	for _, part := range splitList(value) {
		*l = append(*l, part)
	}
	return nil
}

// splitList splits a comma-separated environment variable, dropping blanks.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// splitPathList splits a list of directories on either a comma or the OS path
// separator (':' on Unix, ';' on Windows). Splitting on a bare ':' everywhere
// would cut Windows drive letters in half.
func splitPathList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == os.PathListSeparator
	}) {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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
