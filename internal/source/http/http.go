// Package http fetches content from a generic HTTP(S) endpoint.
//
// SECURITY NOTE: this adapter enforces a scheme allow-list (http/https), a
// response size cap, and a per-fetch timeout. It performs NO SSRF
// protection: the target address is not checked, so a registered resource
// can reach loopback, link-local, and cloud metadata endpoints
// (169.254.169.254), and redirects to those are followed. That is
// acceptable only while resource registration is an operator-trusted
// action — which is what `readproofd --api-key` is for. An address
// allow-list, applied per-hop so it survives redirects and DNS rebinding,
// is required before readproofd accepts registrations from less-trusted
// callers; see docs/security-audit-2026-08.md and spec §39.
//
// Header values may reference environment variables as "${VAR}". Because
// those are read from readproofd's own environment and sent to whatever
// URL the resource names, whoever can register a resource can read any
// environment variable the server holds. resolveHeaderValue refuses the
// server's own credentials by name and supports an opt-in strict
// allow-list; see denyEnvNames and envAllowlistVar.
package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/fbzz/readproof/internal/ids"
	"github.com/fbzz/readproof/internal/source"
)

// DefaultMaxBytes caps how much of a response body is read into memory.
// The body of every fetch is buffered whole (it has to be: the content hash
// covers all of it), so without a cap a single resource pointed at an
// endless response takes readproofd down with it. 64 MiB is far above any
// plausible policy document and far below a memory problem.
const DefaultMaxBytes int64 = 64 << 20

// DefaultTimeout bounds one whole fetch — connect, headers, and body. The
// resolve call above this has no deadline of its own, so without it a
// source that accepts a connection and then stalls holds the request
// goroutine forever.
const DefaultTimeout = 30 * time.Second

// allowedSchemes is the set of URL schemes this adapter will fetch. Go's
// default transport already refuses everything else, but stating it here
// makes the restriction a property of the adapter rather than of whichever
// http.Client it happens to be handed, and turns an obscure round-tripper
// error into a clear one at the point of configuration.
var allowedSchemes = map[string]bool{"http": true, "https": true}

// envVarRef matches "${VAR_NAME}" references embedded anywhere in a header
// value (e.g. "Bearer ${GITHUB_TOKEN}"). Matched references are resolved
// from readproofd's own environment at fetch time — the referenced secret
// is never persisted in a Resource's stored SourceConfig, only the
// reference to it.
var envVarRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// envAllowlistVar names an optional, comma-separated strict allow-list. When
// it is set, ONLY the variables it names may be referenced from a header —
// the recommended configuration for any readproofd whose resource
// registrations are not fully trusted. When it is unset (the default), any
// variable except those in denyEnvNames resolves, which keeps existing
// deployments working.
const envAllowlistVar = "READPROOF_HTTP_HEADER_ENV_ALLOWLIST"

// denyEnvNames are the variables a header may never reference, whatever the
// allow-list says. They are readproofd's own credentials: the key that
// guards its API, the DSN that reaches its database, and its object-store
// keys. No legitimate source needs to send any of them to a third party,
// and being able to would turn "can register a resource" into "can read the
// credential that gates registration" — and into direct access to the store
// behind it.
var denyEnvNames = map[string]bool{
	"READPROOFD_API_KEY":       true,
	"READPROOF_API_KEY":        true,
	"READPROOFD_POSTGRES_DSN":  true,
	"READPROOFD_S3_ACCESS_KEY": true,
	"READPROOFD_S3_SECRET_KEY": true,
}

// envAllowed reports whether name may be resolved out of the environment.
func envAllowed(name string) error {
	if denyEnvNames[name] {
		return fmt.Errorf("header value references $%s, which is one of readproofd's own credentials and is never sent to a source", name)
	}
	raw, ok := os.LookupEnv(envAllowlistVar)
	if !ok {
		return nil
	}
	for _, allowed := range strings.Split(raw, ",") {
		if strings.TrimSpace(allowed) == name {
			return nil
		}
	}
	return fmt.Errorf("header value references $%s, which is not listed in %s", name, envAllowlistVar)
}

func resolveHeaderValue(value string) (string, error) {
	var resolveErr error
	resolved := envVarRef.ReplaceAllStringFunc(value, func(match string) string {
		name := envVarRef.FindStringSubmatch(match)[1]
		if err := envAllowed(name); err != nil {
			resolveErr = err
			return match
		}
		v, ok := os.LookupEnv(name)
		if !ok {
			resolveErr = fmt.Errorf("header value references $%s, which is not set in readproofd's environment", name)
			return match
		}
		return v
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return resolved, nil
}

type Fetcher struct {
	HTTPClient *http.Client
	// MaxBytes caps the response body read into memory; <= 0 means
	// DefaultMaxBytes.
	MaxBytes int64
	// Timeout bounds one fetch; <= 0 means DefaultTimeout. Ignored when
	// HTTPClient is supplied with a Timeout of its own.
	Timeout time.Duration
}

func New() *Fetcher {
	return &Fetcher{
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
		MaxBytes:   DefaultMaxBytes,
		Timeout:    DefaultTimeout,
	}
}

func (f *Fetcher) maxBytes() int64 {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return DefaultMaxBytes
}

func (f *Fetcher) timeout() time.Duration {
	if f.Timeout > 0 {
		return f.Timeout
	}
	return DefaultTimeout
}

// checkURL rejects a target this adapter will not fetch. It is deliberately
// only a scheme check: restricting the target *address* (loopback,
// link-local, metadata endpoints) is a separate, larger control that has to
// survive redirects and DNS rebinding, and is tracked as an SSRF allow-list
// on the roadmap. See the package comment.
func checkURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("http: invalid url %q: %w", raw, err)
	}
	if !allowedSchemes[parsed.Scheme] {
		return fmt.Errorf("http: unsupported url scheme %q in %q (want http or https)", parsed.Scheme, raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("http: url %q has no host", raw)
	}
	return nil
}

func (f *Fetcher) Fetch(ctx context.Context, req source.FetchRequest) (source.FetchResult, error) {
	cfg := req.Config.HTTP
	if cfg == nil {
		return source.FetchResult{}, fmt.Errorf("http: missing http config")
	}
	if cfg.URL == "" {
		return source.FetchResult{}, fmt.Errorf("http: missing url")
	}
	if err := checkURL(cfg.URL); err != nil {
		return source.FetchResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout())
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("http: build request: %w", err)
	}
	for k, v := range cfg.Headers {
		resolved, err := resolveHeaderValue(v)
		if err != nil {
			return source.FetchResult{}, fmt.Errorf("http: header %q: %w", k, err)
		}
		httpReq.Header.Set(k, resolved)
	}

	client := f.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("http: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read one byte past the cap so a body sitting exactly on the limit is
	// still accepted and anything larger is refused rather than silently
	// truncated — a truncated body would be hashed and stored as if it were
	// the whole document.
	limit := f.maxBytes()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("http: read response: %w", err)
	}
	if int64(len(body)) > limit {
		return source.FetchResult{}, fmt.Errorf("http: response from %s exceeds the %d-byte limit", cfg.URL, limit)
	}
	if resp.StatusCode != http.StatusOK {
		return source.FetchResult{}, fmt.Errorf("http: unexpected status %d for %s", resp.StatusCode, cfg.URL)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = source.DetectContentType(cfg.URL)
	}

	// Prefer a server-provided revision marker; HTTP has no universal
	// content-revision concept, so fall back to fingerprinting the body
	// itself (same approach the filesystem adapter uses).
	revision := resp.Header.Get("ETag")
	if revision == "" {
		revision = resp.Header.Get("Last-Modified")
	}
	if revision == "" {
		revision = "sha256:" + ids.SHA256Hex(body)[:12]
	}

	metadata := map[string]string{
		"source_type": "http",
		"url":         cfg.URL,
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		metadata["etag"] = etag
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		metadata["last_modified"] = lm
	}

	return source.FetchResult{
		Content:        body,
		ContentType:    contentType,
		SourceRevision: revision,
		Metadata:       metadata,
	}, nil
}
