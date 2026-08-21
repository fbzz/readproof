package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/source"
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

// Snapshot provenance is what `readproof diff`'s "why" line and evidence
// exports read, so ETag/Last-Modified must land in Metadata verbatim
// whenever the server sends them — and must be absent, not empty, when it
// doesn't.
func TestFetchRecordsETagAndLastModifiedProvenance(t *testing.T) {
	const lastModified = "Wed, 19 Aug 2026 16:05:30 GMT"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"v3"`)
		w.Header().Set("Last-Modified", lastModified)
		w.Write([]byte("body"))
	}))
	defer server.Close()

	f := New()
	result, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := result.Metadata["etag"]; got != `W/"v3"` {
		t.Fatalf("provenance etag = %q, want %q", got, `W/"v3"`)
	}
	if got := result.Metadata["last_modified"]; got != lastModified {
		t.Fatalf("provenance last_modified = %q, want %q", got, lastModified)
	}

	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body"))
	}))
	defer bare.Close()

	result, err = f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: bare.URL}},
	})
	if err != nil {
		t.Fatalf("fetch (no revision headers): %v", err)
	}
	if _, ok := result.Metadata["etag"]; ok {
		t.Fatalf("etag recorded when the server sent none: %+v", result.Metadata)
	}
	if _, ok := result.Metadata["last_modified"]; ok {
		t.Fatalf("last_modified recorded when the server sent none: %+v", result.Metadata)
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
	t.Setenv("READPROOF_TEST_SECRET_TOKEN", "the-real-secret")

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
				Headers: map[string]string{"Authorization": "Bearer ${READPROOF_TEST_SECRET_TOKEN}"},
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
	t.Setenv("READPROOF_TEST_SECRET_TOKEN", "the-real-secret")

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
				Headers: map[string]string{"X-Combo": "prefix-${READPROOF_TEST_SECRET_TOKEN}-suffix"},
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
				Headers: map[string]string{"Authorization": "${READPROOF_TEST_DEFINITELY_UNSET_VAR}"},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected an error for an unresolved env var reference")
	}
}

// Whoever can register a resource chooses both the target URL and the
// header names, so "${VAR}" expansion is an environment-read primitive
// pointed at an arbitrary endpoint. readproofd's own credentials are the
// worst case — reading the API key would defeat the control that gates
// registration in the first place — so they are refused by name.
func TestFetchRefusesToSendReadproofdsOwnCredentials(t *testing.T) {
	for _, name := range []string{
		"READPROOFD_API_KEY",
		"READPROOF_API_KEY",
		"READPROOFD_POSTGRES_DSN",
		"READPROOFD_S3_ACCESS_KEY",
		"READPROOFD_S3_SECRET_KEY",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "the-servers-own-secret")

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("attacker endpoint was reached with headers %v", r.Header)
				w.Write([]byte("ok"))
			}))
			defer server.Close()

			f := New()
			_, err := f.Fetch(context.Background(), source.FetchRequest{
				Config: source.Config{
					Kind: source.KindHTTP,
					HTTP: &source.HTTPConfig{
						URL:     server.URL,
						Headers: map[string]string{"Authorization": "Bearer ${" + name + "}"},
					},
				},
			})
			if err == nil {
				t.Fatalf("fetch succeeded; want a refusal to expand $%s", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error %q does not name the refused variable %s", err, name)
			}
		})
	}
}

// The opt-in strict allow-list is the control an operator turns on when
// resource registration is not fully trusted: with it set, only the named
// variables expand and everything else is refused.
func TestFetchEnvAllowlistRestrictsExpansion(t *testing.T) {
	t.Setenv("READPROOF_TEST_ALLOWED", "allowed-value")
	t.Setenv("READPROOF_TEST_DENIED", "denied-value")
	t.Setenv(envAllowlistVar, "READPROOF_TEST_ALLOWED, SOMETHING_ELSE")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Allowed"); got != "allowed-value" {
			t.Errorf("X-Allowed = %q, want %q", got, "allowed-value")
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	f := New()
	if _, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
			URL:     server.URL,
			Headers: map[string]string{"X-Allowed": "${READPROOF_TEST_ALLOWED}"},
		}},
	}); err != nil {
		t.Fatalf("allow-listed variable was refused: %v", err)
	}

	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{
			URL:     server.URL,
			Headers: map[string]string{"X-Denied": "${READPROOF_TEST_DENIED}"},
		}},
	})
	if err == nil {
		t.Fatalf("fetch succeeded; want a refusal for a variable outside the allow-list")
	}
	if !strings.Contains(err.Error(), envAllowlistVar) {
		t.Fatalf("error %q does not mention %s", err, envAllowlistVar)
	}
}

func TestFetchRejectsNonHTTPSchemes(t *testing.T) {
	f := New()
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:11211/_stats",
		"ftp://example.com/x",
		"/etc/passwd",
		"example.com/x",
	} {
		_, err := f.Fetch(context.Background(), source.FetchRequest{
			Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: raw}},
		})
		if err == nil {
			t.Errorf("Fetch(%q) succeeded; want a scheme rejection", raw)
		}
	}
}

// A body is buffered whole because the content hash covers all of it, so an
// oversized response must be refused rather than truncated: a truncated
// body would be hashed and stored as if it were the complete document.
func TestFetchRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer server.Close()

	f := New()
	f.MaxBytes = 1024
	_, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	})
	if err == nil {
		t.Fatalf("fetch succeeded; want a size-limit error")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error %q does not mention the limit", err)
	}

	// A body exactly on the limit is still accepted.
	exact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1024))
	}))
	defer exact.Close()
	if _, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: exact.URL}},
	}); err != nil {
		t.Fatalf("a body exactly on the limit was refused: %v", err)
	}
}

func TestFetchTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	f := New()
	f.Timeout = 100 * time.Millisecond
	f.HTTPClient = &http.Client{}

	start := time.Now()
	if _, err := f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{Kind: source.KindHTTP, HTTP: &source.HTTPConfig{URL: server.URL}},
	}); err == nil {
		t.Fatalf("fetch of a stalled server succeeded; want a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetch took %s; the timeout did not apply", elapsed)
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
