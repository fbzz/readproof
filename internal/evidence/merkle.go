package evidence

import (
	"encoding/hex"

	"readproof/internal/merkle"
)

// LeafHash returns the Merkle leaf for one entry. The rule lives in
// internal/merkle so that the readproof.run.commit span can compute the
// same root from manifest entries without importing the bundle types; this
// is a thin projection of Entry onto the three fields the leaf commits to.
func LeafHash(e Entry) []byte {
	// merkle.Leaf always returns hex, so the error is unreachable.
	b, _ := hex.DecodeString(merkle.Leaf(e.Position, e.URI, e.ContentHash))
	return b
}

// MerkleRoot computes the hex-encoded root over the entries' leaves, in the
// order given. See internal/merkle for the tree rules and the reasons
// behind them; the fixed vectors in merkle_test.go pin the output.
func MerkleRoot(entries []Entry) string {
	leaves := make([]string, len(entries))
	for i, e := range entries {
		leaves[i] = merkle.Leaf(e.Position, e.URI, e.ContentHash)
	}
	return merkle.Root(leaves)
}
