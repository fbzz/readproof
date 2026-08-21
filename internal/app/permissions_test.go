package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RP-14. The data directory is the record of everything an agent read, plus
// the resource definitions behind it; on a shared host it has no business
// being readable by other accounts.
func TestOpenCreatesAPrivateDataDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes do not apply on Windows")
	}
	dataDir := filepath.Join(t.TempDir(), "store")

	a, err := Open(dataDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()

	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("stat data dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("data dir mode = %#o, want 0700", got)
	}

	// The database, and the WAL sidecars SQLite derives from its mode.
	for _, name := range []string{"readproof.db", "readproof.db-wal", "readproof.db-shm"} {
		info, err := os.Stat(filepath.Join(dataDir, name))
		if os.IsNotExist(err) {
			continue // -wal/-shm exist only while WAL mode is active
		}
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got&0o077 != 0 {
			t.Fatalf("%s mode = %#o, want no group/other access", name, got)
		}
	}
}

// A blob is the verbatim content of a document — the thing worth pinning is
// usually the thing worth not leaving world-readable.
func TestBlobsAreWrittenPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes do not apply on Windows")
	}
	dataDir := filepath.Join(t.TempDir(), "store")
	a, err := Open(dataDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer a.Close()

	if _, err := a.Blobs.Put([]byte("a document worth pinning")); err != nil {
		t.Fatalf("put: %v", err)
	}

	root := filepath.Join(dataDir, "blobs")
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if got := info.Mode().Perm(); got&0o077 != 0 {
			t.Errorf("%s mode = %#o, want no group/other access", path, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
