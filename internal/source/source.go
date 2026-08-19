package source

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Kind identifies the type of physical origin a Resource is backed by.
type Kind string

const (
	KindFilesystem Kind = "filesystem"
	KindGitHub     Kind = "github"
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

// Config is the persisted, source-kind-specific configuration for a Resource.
type Config struct {
	Kind       Kind              `json:"kind"`
	Filesystem *FilesystemConfig `json:"filesystem,omitempty"`
	GitHub     *GitHubConfig     `json:"github,omitempty"`
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
