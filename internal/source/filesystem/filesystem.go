// Package filesystem reads content directly from the local filesystem.
//
// SECURITY: the path in a resource definition is chosen by whoever registers
// the resource, and this adapter hands the file's bytes back to whoever
// resolves it. Embedded in the `readproof` CLI that is exactly right — the
// files, the process and the person typing the command are one trust domain,
// and a restriction there would only stop a user reading their own documents.
// Behind `readproofd` it is a file-read primitive on the server's host, so the
// server runs this adapter with an allow-list of roots (Roots) and, with no
// root configured, refuses filesystem sources outright. See New vs
// NewRestricted, and docs/security-audit-2026-08.md (RP-01).
package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fbzz/readproof/internal/ids"
	"github.com/fbzz/readproof/internal/source"
)

// Fetcher reads content directly from the local filesystem.
type Fetcher struct {
	// Roots, when non-empty, restricts reads to files inside one of these
	// directories. Each is absolute, cleaned, and symlink-resolved by
	// NewRestricted, because the containment check compares resolved paths.
	Roots []string
	// RequireRoot makes an empty Roots mean "deny every filesystem source"
	// rather than "allow every path". It is what separates the server's
	// default-deny from the CLI's unrestricted read.
	RequireRoot bool
}

// New returns an unrestricted Fetcher: any path the process can read is
// allowed.
//
// This is the embedded `readproof` CLI's mode, and it is deliberate. The CLI
// reads the operator's own files as the operator, so an allow-list would
// restrict a user's access to their own documents and protect nobody. A CLI
// pointed at a server with --server never reaches this code: the server's own
// policy applies there, because the fetch happens on the server.
func New() *Fetcher { return &Fetcher{} }

// NewRestricted returns a Fetcher that reads only files inside roots. An empty
// roots denies every filesystem source — the `readproofd` default, since a
// server has no legitimate reason to serve arbitrary paths on its host, and a
// default-deny is the only version that protects an operator who never reads
// the security notes.
//
// Every root is resolved through filepath.EvalSymlinks here, once, so the
// per-fetch containment check compares like with like. A root that does not
// exist is an error rather than an empty allow-list: a typo in
// --filesystem-root should stop the server, not silently deny everything.
func NewRestricted(roots []string) (*Fetcher, error) {
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("filesystem: resolve root %q: %w", root, err)
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("filesystem: root %q is not a readable directory: %w", root, err)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("filesystem: root %q: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("filesystem: root %q is not a directory", root)
		}
		resolved = append(resolved, filepath.Clean(real))
	}
	return &Fetcher{Roots: resolved, RequireRoot: true}, nil
}

// restricted reports whether any containment policy applies at all.
func (f *Fetcher) restricted() bool { return f.RequireRoot || len(f.Roots) > 0 }

// resolvePath applies the allow-list policy to a configured path and returns
// the path to actually read.
//
// Symlinks are evaluated *before* the containment check, which is the whole
// point: a symlink sitting inside an allowed root and pointing at /etc/shadow
// passes a purely lexical check and fails this one.
func (f *Fetcher) resolvePath(path string) (string, error) {
	if !f.restricted() {
		return path, nil
	}
	if path == "" {
		return "", source.Denied("filesystem: source is missing filesystem.path")
	}
	if len(f.Roots) == 0 {
		return "", source.Denied("filesystem: filesystem sources are disabled on this server: no --filesystem-root is configured, so no path is allowed (start readproofd with --filesystem-root <dir>, or set READPROOFD_FILESYSTEM_ROOTS)")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", source.Denied("filesystem: cannot resolve path %q: %v", path, err)
	}
	checked := resolveDeepest(filepath.Clean(abs))

	for _, root := range f.Roots {
		if within(root, checked) {
			return checked, nil
		}
	}
	return "", source.Denied("filesystem: path %q is outside every configured --filesystem-root (%s)", path, strings.Join(f.Roots, ", "))
}

// resolveDeepest returns abs with every symlink among its existing components
// resolved, re-appending the components that do not exist yet.
//
// Plain EvalSymlinks is all-or-nothing: it fails on a path whose last
// component has not been created, which would refuse a resource registered
// before its file exists — and, on macOS, would compare a /var/... path
// against a root that resolved to /private/var/.... Resolving the deepest
// existing ancestor keeps the check honest either way: whatever does exist is
// resolved, so a symlinked parent still cannot smuggle the path out of a root,
// and a component that does not exist can disclose nothing.
func resolveDeepest(abs string) string {
	rest := ""
	current := abs
	for {
		if real, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Clean(filepath.Join(real, rest))
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}

// within reports whether path is root itself or sits under it. filepath.Rel
// does the work; a relative path that starts with ".." escaped the root.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Validate refuses, at registration time, a path this Fetcher would refuse to
// fetch. It is a no-op for an unrestricted (embedded CLI) Fetcher.
func (f *Fetcher) Validate(cfg source.Config) error {
	if !f.restricted() {
		return nil
	}
	if cfg.Filesystem == nil {
		return source.Denied("filesystem: source is missing filesystem.path")
	}
	_, err := f.resolvePath(cfg.Filesystem.Path)
	return err
}

func (f *Fetcher) Fetch(_ context.Context, req source.FetchRequest) (source.FetchResult, error) {
	cfg := req.Config.Filesystem
	if cfg == nil {
		return source.FetchResult{}, fmt.Errorf("filesystem: missing filesystem config")
	}
	path, err := f.resolvePath(cfg.Path)
	if err != nil {
		return source.FetchResult{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return source.FetchResult{}, fmt.Errorf("filesystem: read %s: %w", cfg.Path, err)
	}
	// Filesystem sources have no native revision concept; mtime is unreliable
	// (e.g. across checkouts), so we fingerprint the content itself.
	return source.FetchResult{
		Content:        content,
		ContentType:    source.DetectContentType(cfg.Path),
		SourceRevision: "sha256:" + ids.SHA256Hex(content)[:12],
		Metadata: map[string]string{
			"source_type": "filesystem",
			"path":        cfg.Path,
		},
	}, nil
}
