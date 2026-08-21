package source

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Kind identifies the type of physical origin a Resource is backed by.
type Kind string

const (
	KindFilesystem Kind = "filesystem"
	KindGitHub     Kind = "github"
	KindHTTP       Kind = "http"
)

type FilesystemConfig struct {
	Path string `json:"path"`
}

type GitHubConfig struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref"`
}

type HTTPConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Config is the persisted, source-kind-specific configuration for a Resource.
type Config struct {
	Kind       Kind              `json:"kind"`
	Filesystem *FilesystemConfig `json:"filesystem,omitempty"`
	GitHub     *GitHubConfig     `json:"github,omitempty"`
	HTTP       *HTTPConfig       `json:"http,omitempty"`
}

type FetchRequest struct {
	Config Config
	// PinnedRevision, when set, asks the Fetcher to re-fetch a specific
	// historical revision rather than the current one. Unused in v0.1.
	PinnedRevision string
}

type FetchResult struct {
	Content        []byte
	ContentType    string
	SourceRevision string
	Metadata       map[string]string
}

// Fetcher is the common abstraction every source adapter implements.
type Fetcher interface {
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
}

// Validator is the optional half of a Fetcher: an adapter that can tell,
// without contacting anything, that a source configuration is one it will
// refuse. Registration calls it so an operator gets a 400 naming the policy
// that refused, rather than a resource that only fails the first time
// somebody resolves it.
//
// Validate is never the enforcement point — Fetch is, because a resource row
// can predate the policy that now refuses it. Validate exists to make the
// refusal early and legible, not to make Fetch's check optional.
type Validator interface {
	Validate(cfg Config) error
}

// DeniedError is what an adapter returns when it refuses a source
// configuration on policy grounds: an allow-list root that does not cover the
// path, a target address the server will not connect to, an environment
// variable it will not expand. It is deliberately its own type — nothing is
// broken and nothing leaked, so the HTTP layer can answer 400 with the reason
// instead of burying it in a generic 500.
type DeniedError struct{ Reason string }

func (e *DeniedError) Error() string { return e.Reason }

// Denied builds a DeniedError. The message should say what was refused and,
// where one exists, which flag turns it back on.
func Denied(format string, args ...any) error {
	return &DeniedError{Reason: fmt.Sprintf(format, args...)}
}

// IsDenied reports whether err is, or wraps, a DeniedError.
func IsDenied(err error) bool {
	var denied *DeniedError
	return errors.As(err, &denied)
}

// Registry dispatches a FetchRequest to the Fetcher registered for its Kind.
type Registry struct {
	fetchers map[Kind]Fetcher
}

func NewRegistry() *Registry {
	return &Registry{fetchers: make(map[Kind]Fetcher)}
}

func (r *Registry) Register(k Kind, f Fetcher) {
	r.fetchers[k] = f
}

// Validate asks the Fetcher registered for cfg.Kind whether it would refuse
// this configuration. A kind with no fetcher, or a fetcher implementing no
// Validator, validates clean: this is an early-refusal path, not an admission
// gate, and Fetch still has the last word.
func (r *Registry) Validate(cfg Config) error {
	f, ok := r.fetchers[cfg.Kind]
	if !ok {
		return nil
	}
	v, ok := f.(Validator)
	if !ok {
		return nil
	}
	return v.Validate(cfg)
}

func (r *Registry) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	f, ok := r.fetchers[req.Config.Kind]
	if !ok {
		return FetchResult{}, fmt.Errorf("source: no fetcher registered for kind %q", req.Config.Kind)
	}
	return f.Fetch(ctx, req)
}

// DetectContentType makes a best-effort guess based on file extension.
func DetectContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		return "application/octet-stream"
	}
}
