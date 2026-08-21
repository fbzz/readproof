package api_test

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/source"
)

// captureLog redirects the standard logger for the duration of a test and
// returns what was written to it.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var sb strings.Builder
	flags := log.Flags()
	log.SetOutput(&sb)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})
	return sb.String
}

// RP-11: a 500 used to return err.Error() verbatim, which for a failed
// filesystem read is the absolute path on the server's disk. The response now
// carries a request id and nothing else, and the detail goes to the log under
// the same id.
func TestInternalErrorsDoNotLeakInternals(t *testing.T) {
	logged := captureLog(t)

	// An unrestricted (embedded) App, so the refusal under test is the read
	// failing — a genuine 500 — rather than the source policy, which is a 400.
	dir := t.TempDir()
	missing := filepath.Join(dir, "deleted-since-registration.md")

	a, err := app.OpenWithOptions(filepath.Join(dir, "data"), app.Options{})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	defer a.Close()

	uri := "readproof://demo/gone"
	if err := a.Resources.Create(t.Context(), resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "gone",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: missing},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}

	own := newServer(t, a)
	status, message := postJSON(t, own.URL+"/v1/resolve", map[string]any{"uri": uri})
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d (%s), want 500", status, message)
	}
	if strings.Contains(message, missing) || strings.Contains(message, dir) {
		t.Fatalf("the 500 body leaked a host path: %q", message)
	}
	if !strings.Contains(message, "request id req_") {
		t.Fatalf("the 500 body carries no request id: %q", message)
	}

	// The detail is not lost — it is in the log, under the id the caller was
	// given, which is the whole point of returning one.
	id := message[strings.Index(message, "req_"):]
	id = strings.FieldsFunc(id, func(r rune) bool { return r == ')' || r == ' ' })[0]
	entries := logged()
	if !strings.Contains(entries, id) {
		t.Fatalf("log does not mention request id %q: %q", id, entries)
	}
	if !strings.Contains(entries, missing) {
		t.Fatalf("log does not carry the detail that was withheld: %q", entries)
	}
}
