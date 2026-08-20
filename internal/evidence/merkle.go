package evidence

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// LeafHash returns the Merkle leaf for one entry:
//
//	sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)
//
// The position is length-prefixed as a fixed-width big-endian uint32 and
// the string fields are 0x00-separated so that no two distinct entries can
// serialize to the same byte string (a URI containing the separator would
// otherwise be able to impersonate a different position/hash pair).
// content_hash is hashed as the recorded string, "sha256:<hex>" prefix
// included, so the leaf commits to the exact value stored in the manifest.
func LeafHash(e Entry) []byte {
	h := sha256.New()
	var pos [4]byte
	binary.BigEndian.PutUint32(pos[:], uint32(e.Position))
	h.Write(pos[:])
	h.Write([]byte{0})
	h.Write([]byte(e.URI))
	h.Write([]byte{0})
	h.Write([]byte(e.ContentHash))
	return h.Sum(nil)
}

// MerkleRoot computes the hex-encoded root of a standard binary Merkle tree
// over the entries' leaves, in the order given (entries are already in
// manifest position order — order is a hard Ctx invariant, so it must be
// committed to, not sorted away).
//
// Rules, fixed and mirrored by the TypeScript exporter:
//   - zero entries      -> sha256 of the empty input
//   - exactly one entry -> root is that entry's leaf hash
//   - odd level         -> the last node is duplicated and paired with
//     itself (the Bitcoin rule), then parent = sha256(left || right)
//
// The duplicate-last rule is known to admit CVE-2012-2459-style collisions
// between differently shaped trees; that is acceptable here because the
// bundle also carries the full entry list, so a verifier recomputes the
// root from a known entry count rather than trusting the root alone.
func MerkleRoot(entries []Entry) string {
	if len(entries) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}

	level := make([][]byte, len(entries))
	for i, e := range entries {
		level[i] = LeafHash(e)
	}

	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left // odd node out is paired with itself
			if i+1 < len(level) {
				right = level[i+1]
			}
			h := sha256.New()
			h.Write(left)
			h.Write(right)
			next = append(next, h.Sum(nil))
		}
		level = next
	}
	return hex.EncodeToString(level[0])
}
