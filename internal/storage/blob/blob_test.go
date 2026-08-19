package blob

import (
	"io/fs"
	"os"
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
