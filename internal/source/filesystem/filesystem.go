package filesystem

import (
	"context"
	"fmt"
	"os"

	"github.com/fbzz/readproof/internal/ids"
	"github.com/fbzz/readproof/internal/source"
)

// Fetcher reads content directly from the local filesystem.
type Fetcher struct{}

func New() *Fetcher { return &Fetcher{} }

func (Fetcher) Fetch(_ context.Context, req source.FetchRequest) (source.FetchResult, error) {
	cfg := req.Config.Filesystem
	if cfg == nil {
		return source.FetchResult{}, fmt.Errorf("filesystem: missing filesystem config")
	}
	content, err := os.ReadFile(cfg.Path)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("filesystem: read %s: %w", cfg.Path, err)
	}
	// Filesystem sources have no native revision concept; mtime is unreliable
	// (e.g. across checkouts), so we fingerprint the content itself.
	return source.FetchResult{
		Content:        content,
		ContentType:    source.DetectContentType(cfg.Path),
		SourceRevision: "sha256:" + ids.SHA256Hex(content)[:12],
		Metadata: map[string]string{
			"source_type": "filesystem",
			"path":        cfg.Path,
		},
	}, nil
}
