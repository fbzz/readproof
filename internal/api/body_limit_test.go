package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/api"
	"github.com/fbzz/readproof/internal/app"
)

// Every JSON endpoint decodes straight into a struct, so without a cap a
// single request can make readproofd allocate as much memory as the peer
// cares to send — and readproofd is unauthenticated by default.
func TestRequestBodyIsCapped(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()

	// A well-formed JSON document far larger than the cap: the padding is a
	// legal string value, so only the size limit can reject it.
	oversized := `{"uri":"readproof://demo/x","pad":"` + strings.Repeat("a", int(api.MaxRequestBytes)+1024) + `"}`
	resp, err := http.Post(server.URL+"/v1/resolve", "application/json", strings.NewReader(oversized))
	if err != nil {
		t.Fatalf("post oversized body: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body returned %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

// Trailing data after the JSON value means two readers can disagree about
// what the request said, so it is a 400 rather than something to ignore.
func TestRequestBodyRejectsTrailingData(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/runs", "application/json",
		strings.NewReader(`{"run_id":"a"}{"run_id":"b"}`))
	if err != nil {
		t.Fatalf("post trailing data: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing data returned %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
