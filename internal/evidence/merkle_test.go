package evidence

import (
	"encoding/hex"
	"testing"
)

// Entries used by the fixed vectors below. The expected roots were computed
// with an independent implementation of the documented rule (a short Python
// script over hashlib), not by running MerkleRoot — otherwise the vectors
// would only assert that the code agrees with itself.
var (
	entryA = Entry{Position: 0, URI: "readproof://demo/policies/refunds", ContentHash: "sha256:aaaa"}
	entryB = Entry{Position: 1, URI: "readproof://demo/policies/shipping", ContentHash: "sha256:bbbb"}
	entryC = Entry{Position: 2, URI: "readproof://demo/faq", ContentHash: "sha256:cccc"}
	entryD = Entry{Position: 3, URI: "readproof://demo/tos", ContentHash: "sha256:dddd"}
)

func TestMerkleRootFixedVectors(t *testing.T) {
	tests := []struct {
		name    string
		entries []Entry
		want    string
	}{
		{
			name:    "no entries hashes the empty input",
			entries: nil,
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:    "single entry root is its leaf",
			entries: []Entry{entryA},
			want:    "70c844e26de1089a9d8386db4c8c4aa51e6a21202a0e857f5d51f92b496c4799",
		},
		{
			name:    "two entries",
			entries: []Entry{entryA, entryB},
			want:    "9f4a65f56078f8bbadbd8c2aaf697699ac14d7ada1e15e45a0af1a8b56c6f87a",
		},
		{
			name:    "three entries duplicate the last leaf",
			entries: []Entry{entryA, entryB, entryC},
			want:    "c56ea8ed87709d94dd208274e1865c78941d16bbbd14f7e09b6eeef96804a9b6",
		},
		{
			name:    "four entries",
			entries: []Entry{entryA, entryB, entryC, entryD},
			want:    "a5b64b93a399a39b411a31b4d5dd47adaef09272a9438649670d8ebe9459c99d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MerkleRoot(tt.entries); got != tt.want {
				t.Fatalf("MerkleRoot = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLeafHashFixedVector(t *testing.T) {
	const want = "70c844e26de1089a9d8386db4c8c4aa51e6a21202a0e857f5d51f92b496c4799"
	if got := hex.EncodeToString(LeafHash(entryA)); got != want {
		t.Fatalf("LeafHash = %s, want %s", got, want)
	}
}

// The leaf commits to position, uri and content_hash and nothing else —
// descriptive metadata must never move the root, or two exports of the
// same manifest would disagree.
func TestMerkleRootIgnoresDescriptiveFields(t *testing.T) {
	decorated := entryA
	decorated.SnapshotID = "snap_1"
	decorated.SourceRevision = "rev-9"
	decorated.ContentType = "text/markdown"
	decorated.Bytes = 4096
	decorated.Provenance = map[string]string{"source": "filesystem"}
	decorated.ContentB64 = "aGVsbG8="

	if got, want := MerkleRoot([]Entry{decorated}), MerkleRoot([]Entry{entryA}); got != want {
		t.Fatalf("descriptive fields changed the root: %s != %s", got, want)
	}
}

func TestMerkleRootIsOrderAndValueSensitive(t *testing.T) {
	base := MerkleRoot([]Entry{entryA, entryB})

	// Order is a hard Readproof invariant: the same two resources mounted the
	// other way round is a different context and must digest differently.
	swapped := MerkleRoot([]Entry{entryB, entryA})
	if swapped == base {
		t.Fatal("swapping entry order did not change the root")
	}
	if want := "22fa08b5ca3cbcb085610c4b620fa8e8f05e1d01bf596bcdc43b175c3d5f4e88"; swapped != want {
		t.Fatalf("swapped root = %s, want %s", swapped, want)
	}

	positionsFlipped := MerkleRoot([]Entry{
		{Position: 1, URI: entryA.URI, ContentHash: entryA.ContentHash},
		{Position: 0, URI: entryB.URI, ContentHash: entryB.ContentHash},
	})
	if want := "0643819135b60291ac113bc4f398c0d848011e0a19433cf8012d6d4bba464ffb"; positionsFlipped != want {
		t.Fatalf("position-flipped root = %s, want %s", positionsFlipped, want)
	}

	tamperedHash := MerkleRoot([]Entry{entryA, {Position: 1, URI: entryB.URI, ContentHash: "sha256:bbbc"}})
	if tamperedHash == base {
		t.Fatal("changing a content hash did not change the root")
	}
	if want := "7f2132f24b570d6130a6aaeff85ce535a631daef70de4e17e58164aa90749e5d"; tamperedHash != want {
		t.Fatalf("hash-tampered root = %s, want %s", tamperedHash, want)
	}
}

func TestMerkleRootIsDeterministic(t *testing.T) {
	entries := []Entry{entryA, entryB, entryC}
	first := MerkleRoot(entries)
	for i := 0; i < 10; i++ {
		if got := MerkleRoot(entries); got != first {
			t.Fatalf("MerkleRoot is not deterministic: %s != %s", got, first)
		}
	}
}
