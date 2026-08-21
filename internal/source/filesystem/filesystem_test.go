package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/source"
)

func fetch(t *testing.T, f *Fetcher, path string) (source.FetchResult, error) {
	t.Helper()
	return f.Fetch(context.Background(), source.FetchRequest{
		Config: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: path},
		},
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The embedded CLI reads the operator's own files as the operator. Nothing
// about that changes: an allow-list there would restrict a user's access to
// their own documents and protect nobody.
func TestUnrestrictedFetcherReadsAnyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.md")
	writeFile(t, path, "hello")

	result, err := fetch(t, New(), path)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(result.Content) != "hello" {
		t.Fatalf("content = %q, want %q", result.Content, "hello")
	}
	if result.Metadata["path"] != path {
		t.Fatalf("provenance path = %q, want %q", result.Metadata["path"], path)
	}
}

// The server's default: no root configured means no filesystem source
// resolves at all, and the error names the flag that turns it on.
func TestRestrictedWithNoRootsDeniesEverything(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.md")
	writeFile(t, path, "hello")

	f, err := NewRestricted(nil)
	if err != nil {
		t.Fatalf("NewRestricted: %v", err)
	}
	_, err = fetch(t, f, path)
	if err == nil {
		t.Fatalf("fetch succeeded with no roots configured; want a refusal")
	}
	if !source.IsDenied(err) {
		t.Fatalf("error %v is not a source.DeniedError", err)
	}
	if !strings.Contains(err.Error(), "--filesystem-root") {
		t.Fatalf("error %q does not name --filesystem-root", err)
	}
	if err := f.Validate(source.Config{Kind: source.KindFilesystem, Filesystem: &source.FilesystemConfig{Path: path}}); err == nil {
		t.Fatalf("Validate accepted a path with no roots configured")
	}
}

func TestRestrictedAllowsPathsInsideARoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "policies", "refunds.md")
	writeFile(t, path, "30 days")

	f, err := NewRestricted([]string{root})
	if err != nil {
		t.Fatalf("NewRestricted: %v", err)
	}
	result, err := fetch(t, f, path)
	if err != nil {
		t.Fatalf("fetch inside the root: %v", err)
	}
	if string(result.Content) != "30 days" {
		t.Fatalf("content = %q", result.Content)
	}
	if err := f.Validate(source.Config{Kind: source.KindFilesystem, Filesystem: &source.FilesystemConfig{Path: path}}); err != nil {
		t.Fatalf("Validate refused a path inside the root: %v", err)
	}

	// A file that does not exist yet is registerable — the containment check
	// is lexical when there is nothing to resolve — but reading it still
	// fails as a read, not as a policy refusal.
	missing := filepath.Join(root, "not-created-yet.md")
	if err := f.Validate(source.Config{Kind: source.KindFilesystem, Filesystem: &source.FilesystemConfig{Path: missing}}); err != nil {
		t.Fatalf("Validate refused a not-yet-created path inside the root: %v", err)
	}
	if _, err := fetch(t, f, missing); err == nil || source.IsDenied(err) {
		t.Fatalf("fetch of a missing file: got %v, want a read error", err)
	}
}

// The reason RP-01 is a finding at all: "../" out of an allowed root.
func TestRestrictedRejectsTraversalOutOfARoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "allowed")
	secret := filepath.Join(base, "secret.txt")
	writeFile(t, filepath.Join(root, "ok.md"), "fine")
	writeFile(t, secret, "top secret")

	f, err := NewRestricted([]string{root})
	if err != nil {
		t.Fatalf("NewRestricted: %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, "..", "secret.txt"),
		filepath.Join(root, "..", "..", "etc", "hosts"),
		secret,
		"/etc/hosts",
	} {
		result, err := fetch(t, f, path)
		if err == nil {
			t.Fatalf("fetch(%q) succeeded and returned %q; want a refusal", path, result.Content)
		}
		if !source.IsDenied(err) {
			t.Fatalf("fetch(%q) failed with %v; want a source.DeniedError", path, err)
		}
	}
}

// Evaluating symlinks BEFORE the containment check is what stops a link
// inside an allowed root from serving a file outside it. A lexical check
// passes this case; that is the bug it exists to prevent.
func TestRestrictedRejectsSymlinkEscapingARoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	base := t.TempDir()
	root := filepath.Join(base, "allowed")
	secret := filepath.Join(base, "secret.txt")
	writeFile(t, filepath.Join(root, "ok.md"), "fine")
	writeFile(t, secret, "top secret")

	link := filepath.Join(root, "escape.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	f, err := NewRestricted([]string{root})
	if err != nil {
		t.Fatalf("NewRestricted: %v", err)
	}
	result, err := fetch(t, f, link)
	if err == nil {
		t.Fatalf("fetch through the escaping symlink succeeded and returned %q", result.Content)
	}
	if !source.IsDenied(err) {
		t.Fatalf("symlink escape failed with %v; want a source.DeniedError", err)
	}

	// A symlink that stays inside the root is still fine.
	inside := filepath.Join(root, "alias.md")
	if err := os.Symlink(filepath.Join(root, "ok.md"), inside); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := fetch(t, f, inside); err != nil {
		t.Fatalf("fetch through a symlink inside the root: %v", err)
	}
}

// A root nested inside another root is not a conflict: a path only has to be
// under one of them, and being under both is the ordinary case.
func TestRestrictedAcceptsNestedRoots(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "nested")
	deep := filepath.Join(inner, "deep", "policy.md")
	writeFile(t, deep, "nested content")

	f, err := NewRestricted([]string{outer, inner})
	if err != nil {
		t.Fatalf("NewRestricted: %v", err)
	}
	if _, err := fetch(t, f, deep); err != nil {
		t.Fatalf("fetch under nested roots: %v", err)
	}

	// Order must not matter.
	f2, err := NewRestricted([]string{inner, outer})
	if err != nil {
		t.Fatalf("NewRestricted (reversed): %v", err)
	}
	if _, err := fetch(t, f2, filepath.Join(outer, "policy.md")); err == nil {
		t.Fatalf("expected a read error for a missing file, not success")
	} else if source.IsDenied(err) {
		t.Fatalf("a path under the outer root was refused by policy: %v", err)
	}
}

// A root is resolved once, at startup, so a root that is itself a symlink
// still contains the files under it.
func TestRestrictedResolvesSymlinkedRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeFile(t, filepath.Join(real, "policy.md"), "content")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	f, err := NewRestricted([]string{link})
	if err != nil {
		t.Fatalf("NewRestricted: %v", err)
	}
	if _, err := fetch(t, f, filepath.Join(link, "policy.md")); err != nil {
		t.Fatalf("fetch through a symlinked root: %v", err)
	}
}

// A typo in --filesystem-root must stop the server rather than silently
// producing an allow-list that denies everything.
func TestNewRestrictedRejectsAMissingRoot(t *testing.T) {
	if _, err := NewRestricted([]string{filepath.Join(t.TempDir(), "no-such-dir")}); err == nil {
		t.Fatalf("NewRestricted accepted a root that does not exist")
	}
	file := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, file, "x")
	if _, err := NewRestricted([]string{file}); err == nil {
		t.Fatalf("NewRestricted accepted a file as a root")
	}
}
