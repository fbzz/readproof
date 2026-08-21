package blob

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorePutDedup(t *testing.T) {
	dir := t.TempDir()
	store := NewLocalStore(dir)

	content := []byte("Products can be refunded within 30 days.\n")

	hash1, err := store.Put(content)
	if err != nil {
		t.Fatalf("put 1: %v", err)
	}
	hash2, err := store.Put(content)
	if err != nil {
		t.Fatalf("put 2: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("expected identical hash, got %s vs %s", hash1, hash2)
	}

	var files int
	if err := fs.WalkDir(os.DirFS(dir), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk blob dir: %v", err)
	}
	if files != 1 {
		t.Fatalf("expected exactly 1 blob file on disk, got %d", files)
	}

	got, err := store.Get(hash1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q", string(got))
	}

	has, err := store.Has(hash1)
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has {
		t.Fatalf("expected Has to report true for a stored blob")
	}
}

// A content hash becomes a filesystem path under the blob root, so anything
// that is not exactly "sha256:" + 64 lowercase hex characters must be
// rejected before filepath.Join ever sees it. Without the check, the
// traversal entries below read an arbitrary file off the host.
func TestLocalStoreRejectsMalformedContentHash(t *testing.T) {
	root := t.TempDir()
	// Nested so the traversal below lands inside the temp dir rather than
	// somewhere real: LocalStore.path joins root, hash[:2] and hash, so a
	// hash of "../../secret.txt" resolves three levels above the store root.
	store := NewLocalStore(filepath.Join(root, "a", "b", "blobs"))

	const bait = "top secret"
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(bait), 0o600); err != nil {
		t.Fatalf("write bait file: %v", err)
	}

	// The concrete escape: before content hashes were validated, this Get
	// returned the bait file's contents.
	if got, err := store.Get("sha256:../../secret.txt"); err == nil {
		t.Errorf("traversing Get succeeded and returned %q; want an error", string(got))
	}

	malformed := []string{
		"sha256:../secret.txt",
		"sha256:../../../../etc/passwd",
		"sha256:" + strings.Repeat("a", 63), // too short
		"sha256:" + strings.Repeat("a", 65), // too long
		"sha256:" + strings.Repeat("A", 64), // uppercase
		"sha256:" + strings.Repeat("z", 64), // not hex
		"sha1:" + strings.Repeat("a", 64),   // wrong algorithm
		strings.Repeat("a", 64),             // no prefix
		"sha256:",
		"",
	}
	for _, contentHash := range malformed {
		if got, err := store.Get(contentHash); err == nil {
			t.Errorf("Get(%q) succeeded and returned %q; want an error", contentHash, string(got))
		}
		if _, err := store.Has(contentHash); err == nil {
			t.Errorf("Has(%q) succeeded; want an error", contentHash)
		}
	}
}
