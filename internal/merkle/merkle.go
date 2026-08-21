// Package merkle implements the Merkle tree Readproof commits manifests
// with.
//
// It exists so that exactly one implementation of the leaf/root rule is
// shipped: internal/evidence puts the root in a bundle's in-toto subject
// digest, and internal/run puts the same value on the readproof.run.commit
// span (readproof.manifest.merkle_root). Those two must agree byte for byte
// or the trace stops being a usable handle on the evidence, so neither
// package keeps its own copy of the algorithm.
//
// The fixed vectors in internal/evidence/merkle_test.go are the contract:
// this package's output must never change for a given input.
package merkle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Leaf returns the hex-encoded Merkle leaf for one manifest entry:
//
//	sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)
//
// The position is length-prefixed as a fixed-width big-endian uint32 and
// the string fields are 0x00-separated so that no two distinct entries can
// serialize to the same byte string (a URI containing the separator would
// otherwise be able to impersonate a different position/hash pair).
// contentHash is hashed as the recorded string, "sha256:<hex>" prefix
// included, so the leaf commits to the exact value stored in the manifest.
//
// Only position, uri and content_hash feed the leaf: descriptive metadata
// (snapshot id, ref, content type, provenance) must never move the root, or
// two exports of the same manifest would disagree.
func Leaf(position int, uri, contentHash string) string {
	h := sha256.New()
	var pos [4]byte
	binary.BigEndian.PutUint32(pos[:], uint32(position))
	h.Write(pos[:])
	h.Write([]byte{0})
	h.Write([]byte(uri))
	h.Write([]byte{0})
	h.Write([]byte(contentHash))
	return hex.EncodeToString(h.Sum(nil))
}

// Root computes the hex-encoded root of a standard binary Merkle tree over
// the given hex-encoded leaves, in the order supplied (manifest entries are
// already in position order — order is a hard Readproof invariant, so it
// must be committed to, not sorted away).
//
// Rules, fixed and mirrored by the TypeScript exporter:
//   - zero leaves      -> sha256 of the empty input
//   - exactly one leaf -> the root is that leaf
//   - odd level        -> the last node is duplicated and paired with
//     itself (the Bitcoin rule), then parent = sha256(left || right)
//
// The duplicate-last rule is known to admit CVE-2012-2459-style collisions
// between differently shaped trees; that is acceptable here because the
// evidence bundle also carries the full entry list, so a verifier
// recomputes the root from a known entry count rather than trusting the
// root alone.
func Root(leaves []string) string {
	if len(leaves) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}

	level := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		decoded, err := hex.DecodeString(leaf)
		if err != nil {
			// Unreachable via Leaf, which always returns hex. Hashing the
			// raw string keeps Root total and deterministic instead of
			// panicking on a leaf that came from somewhere else.
			decoded = []byte(leaf)
		}
		level[i] = decoded
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
