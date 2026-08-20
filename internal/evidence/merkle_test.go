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
	entryA = Entry{Position: 0, URI: "ctx://demo/policies/refunds", ContentHash: "sha256:aaaa"}
	entryB = Entry{Position: 1, URI: "ctx://demo/policies/shipping", ContentHash: "sha256:bbbb"}
	entryC = Entry{Position: 2, URI: "ctx://demo/faq", ContentHash: "sha256:cccc"}
	entryD = Entry{Position: 3, URI: "ctx://demo/tos", ContentHash: "sha256:dddd"}
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
			want:    "70d55a88f1fbbf31b196930195a7a943b637d58c828ede07b76c444ba9177c43",
		},
		{
			name:    "two entries",
			entries: []Entry{entryA, entryB},
			want:    "f11999b8089a3ad1dc8090eca9e3dfe3e2b579354fb739c8ce1c4bf23e245399",
		},
		{
			name:    "three entries duplicate the last leaf",
			entries: []Entry{entryA, entryB, entryC},
			want:    "8ce5f7d3589e166bdf8c13111c7ce1212f21297f4c5707074b15ec33da5c12bb",
		},
		{
			name:    "four entries",
			entries: []Entry{entryA, entryB, entryC, entryD},
			want:    "a41de6697c3d1cf9f416fbf985c1acefacc339b23e6cc061b9c9d60775c96cfc",
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
	const want = "70d55a88f1fbbf31b196930195a7a943b637d58c828ede07b76c444ba9177c43"
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

	// Order is a hard Ctx invariant: the same two resources mounted the
	// other way round is a different context and must digest differently.
	swapped := MerkleRoot([]Entry{entryB, entryA})
	if swapped == base {
		t.Fatal("swapping entry order did not change the root")
	}
	if want := "9f8a8a5aa906949eb82be3a087a7f92af5dabd43dd75fb2f2c355f97a003f03f"; swapped != want {
		t.Fatalf("swapped root = %s, want %s", swapped, want)
	}

	positionsFlipped := MerkleRoot([]Entry{
		{Position: 1, URI: entryA.URI, ContentHash: entryA.ContentHash},
		{Position: 0, URI: entryB.URI, ContentHash: entryB.ContentHash},
	})
	if want := "d99793a5b91de30b813550ea9af7606c866ba09c9410f9c05308fc9c4958a197"; positionsFlipped != want {
		t.Fatalf("position-flipped root = %s, want %s", positionsFlipped, want)
	}

	tamperedHash := MerkleRoot([]Entry{entryA, {Position: 1, URI: entryB.URI, ContentHash: "sha256:bbbc"}})
	if tamperedHash == base {
		t.Fatal("changing a content hash did not change the root")
	}
	if want := "ab225edface320414016f65d9e83ae4d4827fd6734f4f00c470dbd8b2e98855b"; tamperedHash != want {
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
