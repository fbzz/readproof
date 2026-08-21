package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/oklog/ulid/v2"
)

// New returns a sortable, prefixed identifier, e.g. New("snap") -> "snap_01J8Y...".
func New(prefix string) string {
	id := ulid.MustNew(ulid.Now(), rand.Reader)
	return prefix + "_" + id.String()
}

// SHA256Hex returns the lowercase hex-encoded sha256 digest of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ContentHash returns the "sha256:<hex>" content hash used throughout
// Readproof.
func ContentHash(b []byte) string {
	return "sha256:" + SHA256Hex(b)
}
