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

func TestFetchResolvesEnvVarReferenceHeaders(t *testing.T) {
	t.Setenv("CTX_TEST_SECRET_TOKEN", "the-real-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer the-real-secret"; got != want {
			t.Fatalf("unexpected Authorization header: got %q, want %q", got, want)
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"Authorization": "Bearer ${CTX_TEST_SECRET_TOKEN}"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetchEnvVarReferenceResolvesEmbeddedInLargerValue(t *testing.T) {
	// "${VAR}" resolves wherever it appears in the header value, not just
	// when it's the entire value — this is what makes "Bearer ${TOKEN}"
	// work, the realistic shape for most auth headers.
	t.Setenv("CTX_TEST_SECRET_TOKEN", "the-real-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-Combo"), "prefix-the-real-secret-suffix"; got != want {
			t.Fatalf("unexpected header: got %q, want %q", got, want)
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"X-Combo": "prefix-${CTX_TEST_SECRET_TOKEN}-suffix"},
			},
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetchUnresolvedEnvVarReferenceErrors(t *testing.T) {
	f := New()
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     "http://example.invalid",
				Headers: map[string]string{"Authorization": "${CTX_TEST_DEFINITELY_UNSET_VAR}"},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected an error for an unresolved env var reference")
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
