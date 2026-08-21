// Package http fetches content from a generic HTTP(S) endpoint.
//
// SECURITY NOTE: this adapter performs no SSRF protection (no restriction
// on target IP ranges). That's acceptable while URLs are only ever
// configured by the operator running the CLI against their own data. It
// becomes a real requirement once a network-facing service (the future
// readproofd HTTP API) accepts resource registration from less-trusted
// callers — see spec §39.
package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"

	"readproof/internal/ids"
	"readproof/internal/source"
)

// envVarRef matches "${VAR_NAME}" references embedded anywhere in a header
// value (e.g. "Bearer ${GITHUB_TOKEN}"). Matched references are resolved
// from readproofd's own environment at fetch time — the referenced secret
// is never persisted in a Resource's stored SourceConfig, only the
// reference to it.
var envVarRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func resolveHeaderValue(value string) (string, error) {
	var resolveErr error
	resolved := envVarRef.ReplaceAllStringFunc(value, func(match string) string {
		name := envVarRef.FindStringSubmatch(match)[1]
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
}

func New() *Fetcher {
	return &Fetcher{HTTPClient: http.DefaultClient}
}

func (f *Fetcher) Fetch(ctx context.Context, req source.FetchRequest) (source.FetchResult, error) {
	cfg := req.Config.HTTP
	if cfg == nil {
		return source.FetchResult{}, fmt.Errorf("http: missing http config")
	}
	if cfg.URL == "" {
		return source.FetchResult{}, fmt.Errorf("http: missing url")
	}

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("http: read response: %w", err)
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
