package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ctx/internal/source"
)

func TestFetch(t *testing.T) {
	const body = "Products can be refunded within 30 days.\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("unexpected Authorization header: got %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer server.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"Authorization": "Bearer test-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if string(result.Content) != body {
		t.Fatalf("unexpected content: got %q, want %q", string(result.Content), body)
	}
	if result.ContentType != "text/markdown" {
		t.Fatalf("unexpected content type: got %q", result.ContentType)
	}
	if result.SourceRevision != `"abc123"` {
		t.Fatalf("unexpected source revision: got %q, want ETag value", result.SourceRevision)
	}
	if result.Metadata["source_type"] != "http" || result.Metadata["url"] != server.URL {
		t.Fatalf("unexpected metadata: %+v", result.Metadata)
	}
}

func TestFetchFallsBackToContentFingerprintWithoutETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no revision headers here"))
	}))
	defer server.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.SourceRevision == "" {
		t.Fatalf("expected a non-empty fallback source revision")
	}
}

func TestFetchNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err == nil {
		t.Fatalf("expected an error for a 404 response")
	}
}
