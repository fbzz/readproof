package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
)

// ContentHashPrefix is the algorithm prefix every Readproof content hash
// carries. Only sha256 is defined.
const ContentHashPrefix = "sha256:"

// contentHashHexLen is the length of a hex-encoded sha256 digest.
const contentHashHexLen = sha256.Size * 2

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
	return ContentHashPrefix + SHA256Hex(b)
}

// ParseContentHash validates a "sha256:<hex>" content hash and returns its
// hex digest.
//
// Blob stores build a storage key out of the digest — a filesystem path for
// blob.LocalStore, an object key for s3blob.Store — so the digest must be
// proven to be exactly 64 lowercase hex characters *before* it reaches
// filepath.Join or a bucket key. Without that check a crafted hash such as
// "sha256:../../../../etc/passwd" would escape the blob root. Content
// hashes normally originate inside Readproof, but they also arrive from
// stored rows and from client-supplied identifiers, so validation belongs
// at the boundary rather than in the callers' good intentions.
func ParseContentHash(contentHash string) (string, error) {
	hexDigest, ok := strings.CutPrefix(contentHash, ContentHashPrefix)
	if !ok {
		return "", fmt.Errorf("ids: invalid content hash %q: want %q prefix", contentHash, ContentHashPrefix)
	}
	if len(hexDigest) != contentHashHexLen {
		return "", fmt.Errorf("ids: invalid content hash %q: want %d hex characters, got %d", contentHash, contentHashHexLen, len(hexDigest))
	}
	for i := 0; i < len(hexDigest); i++ {
		c := hexDigest[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("ids: invalid content hash %q: %q is not a lowercase hex character", contentHash, string(c))
		}
	}
	return hexDigest, nil
}
