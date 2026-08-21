package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"readproof/internal/source"
)

func TestFetch(t *testing.T) {
	const body = "Products can be refunded within 30 days.\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/acme/company-docs/contents/policies/refunds.md"; got != want {
			t.Fatalf("unexpected path: got %s, want %s", got, want)
		}
		if got, want := r.URL.Query().Get("ref"), "main"; got != want {
			t.Fatalf("unexpected ref: got %s, want %s", got, want)
		}
		resp := contentsResponse{
			SHA:      "8af92d1",
			Content:  base64.StdEncoding.EncodeToString([]byte(body)),
			Encoding: "base64",
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	f := &Fetcher{HTTPClient: server.Client(), BaseURL: server.URL}
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindGitHub,
			GitHub: &source.GitHubConfig{
				Owner: "acme",
				Repo:  "company-docs",
				Path:  "policies/refunds.md",
				Ref:   "main",
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if string(result.Content) != body {
		t.Fatalf("unexpected content: got %q, want %q", string(result.Content), body)
	}
	if result.SourceRevision != "8af92d1" {
		t.Fatalf("unexpected source revision: got %q", result.SourceRevision)
	}
	if result.ContentType != "text/markdown" {
		t.Fatalf("unexpected content type: got %q", result.ContentType)
	}
	if result.Metadata["source_type"] != "github" || result.Metadata["owner"] != "acme" || result.Metadata["repo"] != "company-docs" {
		t.Fatalf("unexpected metadata: %+v", result.Metadata)
	}
}
