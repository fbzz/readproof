package merkle_test

import (
	"testing"

	"ctx/internal/merkle"
)

// The exhaustive fixed vectors live in internal/evidence/merkle_test.go —
// they predate this package and are the contract the extraction had to
// preserve. What is tested here is the boundary this package introduced:
// leaves crossing between Leaf and Root as hex strings rather than bytes.

const (
	uriA  = "ctx://demo/policies/refunds"
	uriB  = "ctx://demo/policies/shipping"
	hashA = "sha256:aaaa"
	hashB = "sha256:bbbb"
)

func TestLeafAndRootMatchKnownVectors(t *testing.T) {
	const wantLeafA = "70d55a88f1fbbf31b196930195a7a943b637d58c828ede07b76c444ba9177c43"

	if got := merkle.Leaf(0, uriA, hashA); got != wantLeafA {
		t.Fatalf("Leaf = %s, want %s", got, wantLeafA)
	}
	// A single-leaf tree's root is that leaf: the hex round-trip through
	// Root must be lossless, not merely deterministic.
	if got := merkle.Root([]string{wantLeafA}); got != wantLeafA {
		t.Fatalf("Root of one leaf = %s, want %s", got, wantLeafA)
	}
	if got, want := merkle.Root(nil), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Fatalf("Root of no leaves = %s, want %s (sha256 of the empty input)", got, want)
	}
	if got, want := merkle.Root([]string{
		merkle.Leaf(0, uriA, hashA),
		merkle.Leaf(1, uriB, hashB),
	}), "f11999b8089a3ad1dc8090eca9e3dfe3e2b579354fb739c8ce1c4bf23e245399"; got != want {
		t.Fatalf("Root of two leaves = %s, want %s", got, want)
	}
}

// Order is a hard Ctx invariant — the same two resources mounted the other
// way round is a different context and must digest differently.
func TestRootIsOrderSensitive(t *testing.T) {
	a, b := merkle.Leaf(0, uriA, hashA), merkle.Leaf(1, uriB, hashB)
	if merkle.Root([]string{a, b}) == merkle.Root([]string{b, a}) {
		t.Fatal("swapping leaf order did not change the root")
	}
}

// Position is part of the leaf, so the same URI and hash at a different
// position is a different leaf.
func TestLeafCommitsToPosition(t *testing.T) {
	if merkle.Leaf(0, uriA, hashA) == merkle.Leaf(1, uriA, hashA) {
		t.Fatal("position does not affect the leaf")
	}
}
