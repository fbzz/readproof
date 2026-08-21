package blob

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fbzz/readproof/internal/ids"
)

// Store is the content-addressable blob interface. Payloads are immutable
// and identified purely by content hash — a future S3/MinIO implementation
// satisfies the same interface.
type Store interface {
	Put(content []byte) (contentHash string, err error)
	Get(contentHash string) ([]byte, error)
	Has(contentHash string) (bool, error)
}

// LocalStore stores blobs on local disk, sharded git-object-store style:
// <root>/<first-2-hex>/<full-hex>.
type LocalStore struct {
	root string
}

func NewLocalStore(root string) *LocalStore {
	return &LocalStore{root: root}
}

// hexPart validates contentHash and returns its hex digest. Validation is
// strict — exactly 64 lowercase hex characters — because the digest is
// joined onto the store root to form a filesystem path, and a hash like
// "sha256:../../../etc/passwd" would otherwise read outside it.
func hexPart(contentHash string) (string, error) {
	hexHash, err := ids.ParseContentHash(contentHash)
	if err != nil {
		return "", fmt.Errorf("blob: %w", err)
	}
	return hexHash, nil
}

func (s *LocalStore) path(hexHash string) string {
	return filepath.Join(s.root, hexHash[:2], hexHash)
}

func (s *LocalStore) Put(content []byte) (string, error) {
	hexHash := ids.SHA256Hex(content)
	contentHash := "sha256:" + hexHash
	path := s.path(hexHash)

	if _, err := os.Stat(path); err == nil {
		return contentHash, nil // dedup: identical bytes already stored
	}
	// 0700/0600: a blob is the verbatim content of a document somebody's
	// agent read. Whatever made that document worth pinning is a reason not
	// to leave a copy of it readable by every account on the host.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("blob: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return "", fmt.Errorf("blob: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("blob: rename: %w", err)
	}
	return contentHash, nil
}

func (s *LocalStore) Get(contentHash string) ([]byte, error) {
	hexHash, err := hexPart(contentHash)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(s.path(hexHash))
	if err != nil {
		return nil, fmt.Errorf("blob: read %s: %w", contentHash, err)
	}
	return content, nil
}

func (s *LocalStore) Has(contentHash string) (bool, error) {
	hexHash, err := hexPart(contentHash)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(s.path(hexHash))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
