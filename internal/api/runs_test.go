package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/api"
	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/client/remote"
	"github.com/fbzz/readproof/internal/wire"
)

// POST /v1/runs/commit used to answer 200 with an empty manifest for a run
// id nobody ever started, and 200 again for a run already committed. Both
// are client mistakes about which run exists, so both get a 4xx naming the
// run — not a fabricated manifest.
func TestRunCommitRejectsUnknownAndCommittedRuns(t *testing.T) {
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	defer server.Close()

	commit := func(t *testing.T, runID string) (int, string) {
		t.Helper()
		resp, err := http.Post(server.URL+"/v1/runs/commit", "application/json",
			strings.NewReader(`{"run_id":"`+runID+`"}`))
		if err != nil {
			t.Fatalf("post commit: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			wire.ErrorResponse
			ManifestID string `json:"manifest_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.StatusCode == http.StatusOK {
			return resp.StatusCode, body.ManifestID
		}
		return resp.StatusCode, body.Error
	}

	status, msg := commit(t, "run-never-started")
	if status != http.StatusNotFound {
		t.Fatalf("commit of an unknown run: status = %d (%s), want 404", status, msg)
	}
	if !strings.Contains(msg, "run-never-started") {
		t.Errorf("404 body %q does not name the run id", msg)
	}

	c := remote.New(server.URL, "")
	defer c.Close()
	ctx := t.Context()
	if err := c.RunStart(ctx, "run-a"); err != nil {
		t.Fatalf("start run: %v", err)
	}
	man, err := c.RunCommit(ctx, "run-a")
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}

	status, msg = commit(t, "run-a")
	if status != http.StatusConflict {
		t.Fatalf("second commit of run-a: status = %d (%s), want 409", status, msg)
	}
	if !strings.Contains(msg, man.ManifestID) {
		t.Errorf("409 body %q does not name the existing manifest %q", msg, man.ManifestID)
	}
}
