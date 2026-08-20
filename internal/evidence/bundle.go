// Package evidence builds and verifies tamper-evident evidence bundles for
// a Ctx run: an in-toto Statement whose subject digest is a Merkle root
// over the run's manifest entries.
//
// Everything here is composed purely from client.Client calls (manifest,
// snapshots, resources, replay), so `ctx evidence` behaves identically in
// embedded mode and against a remote ctxd — a bundle is a projection of
// what any Ctx deployment can already answer, never a new storage or wire
// concept.
package evidence

import (
	"encoding/json"
	"fmt"
	"time"

	"ctx/internal/version"
)

const (
	// StatementType is the in-toto Statement v1 type. Bundles are valid
	// in-toto statements so existing supply-chain tooling (cosign, in-toto
	// attestation verifiers) can sign and transport them unmodified.
	StatementType = "https://in-toto.io/Statement/v1"

	// PredicateType is a PLACEHOLDER URN. Ctx has not settled its final
	// name, and the predicate type is the one string external verifiers
	// key off — keeping it in a single exported const makes the rename a
	// one-line change here, mirrored by the same const in
	// sdk/typescript/src/evidence.ts.
	PredicateType = "urn:ctx:evidence:v0.2"

	// ExporterName / ExporterVersion identify the producer of the bundle
	// format, not the Ctx deployment it was exported from — hence the plain
	// version.Version rather than version.String(): two builds of the same
	// source must export byte-identical bundles.
	ExporterName    = "ctx"
	ExporterVersion = version.Version

	// MerkleAlgorithm and MerkleLeafFormula are embedded in every bundle
	// so a verifier can recompute the root without reading this source.
	MerkleAlgorithm   = "sha256"
	MerkleLeafFormula = "sha256(position_be_uint32 || 0x00 || uri || 0x00 || content_hash)"
)

// Bundle is an in-toto Statement v1 describing one Ctx manifest. Field
// order is fixed by the struct definition rather than by map iteration, so
// the JSON encoding is stable across runs and byte-comparable between the
// Go and TypeScript exporters.
type Bundle struct {
	Type          string    `json:"_type"`
	Subject       []Subject `json:"subject"`
	PredicateType string    `json:"predicateType"`
	Predicate     Predicate `json:"predicate"`
}

// Subject names the manifest and digests it with the Merkle root over its
// entries — the single value an external attestation needs to sign.
type Subject struct {
	Name   string `json:"name"`
	Digest Digest `json:"digest"`
}

type Digest struct {
	SHA256 string `json:"sha256"`
}

type Predicate struct {
	RunID             string     `json:"run_id"`
	ManifestID        string     `json:"manifest_id"`
	ManifestCreatedAt time.Time  `json:"manifest_created_at"`
	GeneratedAt       time.Time  `json:"generated_at"`
	Exporter          Exporter   `json:"exporter"`
	Merkle            Merkle     `json:"merkle"`
	Entries           []Entry    `json:"entries"`
	Resources         []Resource `json:"resources"`
	Replay            Replay     `json:"replay"`
}

type Exporter struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Merkle struct {
	Algorithm string `json:"algorithm"`
	Leaf      string `json:"leaf"`
	Root      string `json:"root"`
}

// Entry is one manifest entry hydrated with its snapshot metadata.
// Position, URI and ContentHash are the only fields that feed the Merkle
// leaf; everything else is descriptive.
type Entry struct {
	Position int    `json:"position"`
	URI      string `json:"uri"`
	// Ref is the "@<tag>" the entry was mounted by ("" for a plain URI).
	// Descriptive only — it is deliberately NOT part of the Merkle leaf, so
	// roots stay stable for manifests recorded before tags existed.
	Ref               string            `json:"ref,omitempty"`
	SnapshotID        string            `json:"snapshot_id"`
	MaterializationID string            `json:"materialization_id"`
	ContentHash       string            `json:"content_hash"`
	SourceRevision    string            `json:"source_revision"`
	ObservedAt        time.Time         `json:"observed_at"`
	ContentType       string            `json:"content_type"`
	Bytes             int64             `json:"bytes"`
	Provenance        map[string]string `json:"provenance"`
	// ContentB64 is populated only with Options.WithContent. Without it a
	// bundle is metadata-only: safe to hand to an auditor who is allowed
	// to know what the agent read but not to read it.
	ContentB64 string `json:"content_b64,omitempty"`
}

// Resource records the definition behind an entry's URI at export time.
// Source config is always redacted (see internal/redact): a bundle is an
// artifact meant to leave the building, so it must never carry credentials
// even when it was built in embedded mode from unredacted local state.
type Resource struct {
	URI       string `json:"uri"`
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
	Source    Source `json:"source"`
	Policy    Policy `json:"policy"`
	// Missing marks a URI whose resource definition no longer exists.
	// Recorded rather than fatal: a manifest stays replayable after its
	// resource is deregistered, and the evidence should say exactly that.
	Missing bool `json:"missing,omitempty"`
}

type Source struct {
	Kind   string       `json:"kind"`
	Config SourceConfig `json:"config"`
}

type SourceConfig struct {
	Filesystem *FilesystemConfig `json:"filesystem,omitempty"`
	GitHub     *GitHubConfig     `json:"github,omitempty"`
	HTTP       *HTTPConfig       `json:"http,omitempty"`
}

type FilesystemConfig struct {
	Path string `json:"path"`
}

type GitHubConfig struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref"`
}

type HTTPConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type Policy struct {
	Strategy         string `json:"strategy"`
	MaxAgeSeconds    int64  `json:"max_age_seconds,omitempty"`
	PinnedSnapshotID string `json:"pinned_snapshot_id,omitempty"`
}

// Replay records the reconstruction check performed at export time: every
// entry's bytes re-read from the blob store and re-hashed.
type Replay struct {
	VerifiedAt time.Time     `json:"verified_at"`
	AllMatch   bool          `json:"all_match"`
	Entries    []ReplayEntry `json:"entries"`
	// Error is set when replay could not run at all (e.g. a blob is gone).
	// The export still succeeds — an un-replayable manifest is precisely
	// the situation an auditor needs a durable record of.
	Error string `json:"error,omitempty"`
}

type ReplayEntry struct {
	Position     int    `json:"position"`
	Match        bool   `json:"match"`
	ExpectedHash string `json:"expected_hash"`
	ActualHash   string `json:"actual_hash"`
	Error        string `json:"error,omitempty"`
}

// Encode renders a bundle as indented JSON with a trailing newline, so
// `ctx evidence export > bundle.json` produces a well-formed text file.
func Encode(b Bundle) ([]byte, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("evidence: encode bundle: %w", err)
	}
	return append(data, '\n'), nil
}

// Decode parses a bundle. Unknown fields are deliberately tolerated (no
// DisallowUnknownFields): a bundle written by a newer exporter must still
// verify against an older binary for the checks that binary understands.
func Decode(data []byte) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, fmt.Errorf("evidence: decode bundle: %w", err)
	}
	return b, nil
}
