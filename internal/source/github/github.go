package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"ctx/internal/source"
)

// Fetcher reads content from the GitHub Contents API.
type Fetcher struct {
	HTTPClient *http.Client
	// BaseURL defaults to https://api.github.com; overridable for tests.
	BaseURL string
}

func New() *Fetcher {
	return &Fetcher{HTTPClient: http.DefaultClient, BaseURL: "https://api.github.com"}
}

type contentsResponse struct {
	SHA      string `json:"sha"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func (f *Fetcher) Fetch(ctx context.Context, req source.FetchRequest) (source.FetchResult, error) {
	cfg := req.Config.GitHub
	if cfg == nil {
		return source.FetchResult{}, fmt.Errorf("github: missing github config")
	}
	ref := cfg.Ref
	if ref == "" {
		ref = "main"
	}
	base := f.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", base, cfg.Owner, cfg.Repo, cfg.Path, ref)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("github: build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	client := f.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("github: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return source.FetchResult{}, fmt.Errorf("github: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var parsed contentsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return source.FetchResult{}, fmt.Errorf("github: parse response: %w", err)
	}

	var content []byte
	if parsed.Encoding == "base64" {
		cleaned := strings.ReplaceAll(parsed.Content, "\n", "")
		content, err = base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return source.FetchResult{}, fmt.Errorf("github: decode content: %w", err)
		}
	} else {
		content = []byte(parsed.Content)
	}

	return source.FetchResult{
		Content:        content,
		ContentType:    source.DetectContentType(cfg.Path),
		SourceRevision: parsed.SHA,
		Metadata: map[string]string{
			"source_type": "github",
			"owner":       cfg.Owner,
			"repo":        cfg.Repo,
			"path":        cfg.Path,
			"ref":         ref,
		},
	}, nil
}
