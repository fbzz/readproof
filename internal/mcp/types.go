package mcp

import (
	"github.com/fbzz/readproof/internal/diff"
	"github.com/fbzz/readproof/internal/manifest"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/redact"
	"github.com/fbzz/readproof/internal/replay"
	"github.com/fbzz/readproof/internal/resolver"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/snapshot"
	"github.com/fbzz/readproof/internal/source"
	"github.com/fbzz/readproof/internal/tag"
)

// The types in this file are the JSON payloads the MCP tools return. They
// are deliberately separate from the internal domain types: an MCP result
// is a wire format read by a model, so it uses snake_case JSON names,
// RFC3339 strings instead of time.Time, and never carries a credential.
// Every field is a plain scalar, slice, or nested struct so the SDK can
// derive each tool's output schema by reflection.

// SourceInfo is a resource's origin with credential-bearing fields
// redacted (see internal/redact). HTTP header values are the field that
// carries secrets, and they are masked even in embedded mode: this
// projection is the one that leaves the process.
type SourceInfo struct {
	Kind    string            `json:"kind"`
	Path    string            `json:"path,omitempty"`
	Owner   string            `json:"owner,omitempty"`
	Repo    string            `json:"repo,omitempty"`
	Ref     string            `json:"ref,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func sourceInfo(cfg source.Config) SourceInfo {
	out := SourceInfo{Kind: string(cfg.Kind)}
	switch {
	case cfg.Filesystem != nil:
		out.Path = cfg.Filesystem.Path
	case cfg.GitHub != nil:
		out.Owner = cfg.GitHub.Owner
		out.Repo = cfg.GitHub.Repo
		out.Path = cfg.GitHub.Path
		out.Ref = cfg.GitHub.Ref
	case cfg.HTTP != nil:
		out.URL = cfg.HTTP.URL
		out.Headers = redact.Headers(cfg.HTTP.Headers)
	}
	return out
}

// PolicyInfo is a resource's freshness policy.
type PolicyInfo struct {
	Strategy         string `json:"strategy"`
	MaxAgeSeconds    int64  `json:"max_age_seconds,omitempty"`
	PinnedSnapshotID string `json:"pinned_snapshot_id,omitempty"`
}

func policyInfo(p policy.Policy) PolicyInfo {
	return PolicyInfo{
		Strategy:         string(p.Strategy),
		MaxAgeSeconds:    int64(p.MaxAge.Seconds()),
		PinnedSnapshotID: p.PinnedSnapshotID,
	}
}

// ResourceInfo is one registered resource as readproof_resources_list
// reports it.
type ResourceInfo struct {
	URI               string     `json:"uri"`
	Namespace         string     `json:"namespace"`
	Path              string     `json:"path"`
	Description       string     `json:"description"`
	Source            SourceInfo `json:"source"`
	Policy            PolicyInfo `json:"policy"`
	CurrentSnapshotID string     `json:"current_snapshot_id,omitempty"`
}

func resourceInfo(r resource.Resource) ResourceInfo {
	return ResourceInfo{
		URI:               r.URI,
		Namespace:         r.Namespace,
		Path:              r.Path,
		Description:       describeResource(r),
		Source:            sourceInfo(r.SourceConfig),
		Policy:            policyInfo(r.Policy),
		CurrentSnapshotID: r.CurrentSnapshotID,
	}
}

// ContentPayload carries resolved bytes. Exactly one of Text/Base64 is
// set, per Encoding; Truncated says whether it is a prefix, and TotalBytes
// is always the full length so a caller can tell how much it is missing.
type ContentPayload struct {
	Encoding   string `json:"encoding"`
	Text       string `json:"text,omitempty"`
	Base64     string `json:"base64,omitempty"`
	Truncated  bool   `json:"truncated"`
	TotalBytes int    `json:"total_bytes"`
}

func contentPayload(enc encodedContent) *ContentPayload {
	return &ContentPayload{
		Encoding:   enc.Encoding,
		Text:       enc.Text,
		Base64:     enc.blobBase64(),
		Truncated:  enc.Truncated,
		TotalBytes: enc.TotalBytes,
	}
}

// SnapshotInfo is one immutable observation of a resource.
type SnapshotInfo struct {
	SnapshotID     string            `json:"snapshot_id"`
	ResourceURI    string            `json:"resource_uri"`
	SourceRevision string            `json:"source_revision"`
	ContentHash    string            `json:"content_hash"`
	ObservedAt     string            `json:"observed_at"`
	CreatedAt      string            `json:"created_at"`
	ContentType    string            `json:"content_type"`
	Bytes          int64             `json:"bytes"`
	Provenance     map[string]string `json:"provenance,omitempty"`
	// Tags are the tag names currently pointing at this snapshot — the
	// answer to "which of these can I pin?" without a second call.
	Tags []string `json:"tags,omitempty"`
}

func snapshotInfo(s snapshot.Snapshot, tags []string) SnapshotInfo {
	return SnapshotInfo{
		SnapshotID:     s.SnapshotID,
		ResourceURI:    s.ResourceURI,
		SourceRevision: s.SourceRevision,
		ContentHash:    s.ContentHash,
		ObservedAt:     formatTime(s.ObservedAt),
		CreatedAt:      formatTime(s.CreatedAt),
		ContentType:    s.ContentType,
		Bytes:          s.Bytes,
		Provenance:     s.Provenance,
		Tags:           tags,
	}
}

// ResolveOut is the result of resolving one reference: the snapshot that
// was selected, why it was selected (Decision), and optionally the bytes.
type ResolveOut struct {
	URI               string            `json:"uri"`
	Ref               string            `json:"ref,omitempty"`
	Decision          string            `json:"decision"`
	SnapshotID        string            `json:"snapshot_id"`
	ContentHash       string            `json:"content_hash"`
	SourceRevision    string            `json:"source_revision"`
	ObservedAt        string            `json:"observed_at"`
	ContentType       string            `json:"content_type"`
	Bytes             int64             `json:"bytes"`
	MaterializationID string            `json:"materialization_id"`
	Provenance        map[string]string `json:"provenance,omitempty"`
	Content           *ContentPayload   `json:"content,omitempty"`
}

func resolveOut(r resolver.ResolveResult, content *ContentPayload) ResolveOut {
	return ResolveOut{
		URI:               r.Snapshot.ResourceURI,
		Ref:               r.Ref,
		Decision:          r.Decision.String(),
		SnapshotID:        r.Snapshot.SnapshotID,
		ContentHash:       r.Snapshot.ContentHash,
		SourceRevision:    r.Snapshot.SourceRevision,
		ObservedAt:        formatTime(r.Snapshot.ObservedAt),
		ContentType:       r.Snapshot.ContentType,
		Bytes:             r.Snapshot.Bytes,
		MaterializationID: r.Materialization.MaterializationID,
		Provenance:        r.Snapshot.Provenance,
		Content:           content,
	}
}

// ManifestEntryOut is one position in a committed manifest.
type ManifestEntryOut struct {
	Position          int    `json:"position"`
	URI               string `json:"uri"`
	Ref               string `json:"ref,omitempty"`
	SnapshotID        string `json:"snapshot_id"`
	MaterializationID string `json:"materialization_id"`
	ContentHash       string `json:"content_hash"`
}

// ManifestOut is a committed manifest: the immutable record of everything
// one run resolved, in mount order.
type ManifestOut struct {
	ManifestID string             `json:"manifest_id"`
	RunID      string             `json:"run_id"`
	CreatedAt  string             `json:"created_at"`
	Entries    []ManifestEntryOut `json:"entries"`
}

func manifestOut(m manifest.Manifest) ManifestOut {
	entries := make([]ManifestEntryOut, len(m.Entries))
	for i, e := range m.Entries {
		entries[i] = ManifestEntryOut{
			Position:          e.Position,
			URI:               e.URI,
			Ref:               e.Ref,
			SnapshotID:        e.SnapshotID,
			MaterializationID: e.MaterializationID,
			ContentHash:       e.ContentHash,
		}
	}
	return ManifestOut{
		ManifestID: m.ManifestID,
		RunID:      m.RunID,
		CreatedAt:  formatTime(m.CreatedAt),
		Entries:    entries,
	}
}

// DiffEntryOut is one URI's status between two manifests. The per-side
// source_revision/observed_at/ref fields are the "why did this change?"
// provenance; unified_diff is set only for a changed entry.
type DiffEntryOut struct {
	URI             string `json:"uri"`
	Status          string `json:"status"`
	SnapshotIDA     string `json:"snapshot_id_a,omitempty"`
	SnapshotIDB     string `json:"snapshot_id_b,omitempty"`
	SourceRevisionA string `json:"source_revision_a,omitempty"`
	SourceRevisionB string `json:"source_revision_b,omitempty"`
	ObservedAtA     string `json:"observed_at_a,omitempty"`
	ObservedAtB     string `json:"observed_at_b,omitempty"`
	RefA            string `json:"ref_a,omitempty"`
	RefB            string `json:"ref_b,omitempty"`
	UnifiedDiff     string `json:"unified_diff,omitempty"`
}

// DiffOut compares two manifests entry by entry.
type DiffOut struct {
	ManifestA string `json:"manifest_a"`
	ManifestB string `json:"manifest_b"`
	Changed   int    `json:"changed"`
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Unchanged int    `json:"unchanged"`
	// Entries omits unchanged URIs' diff text but still lists them, so a
	// caller can see the full comparison, not just what moved.
	Entries []DiffEntryOut `json:"entries"`
}

func diffOut(r diff.Result) DiffOut {
	changed, added, removed, unchanged := r.Counts()
	entries := make([]DiffEntryOut, len(r.Entries))
	for i, e := range r.Entries {
		entries[i] = DiffEntryOut{
			URI:             e.URI,
			Status:          string(e.Status),
			SnapshotIDA:     e.SnapshotIDA,
			SnapshotIDB:     e.SnapshotIDB,
			SourceRevisionA: e.SourceRevisionA,
			SourceRevisionB: e.SourceRevisionB,
			ObservedAtA:     formatTime(e.ObservedAtA),
			ObservedAtB:     formatTime(e.ObservedAtB),
			RefA:            e.RefA,
			RefB:            e.RefB,
			UnifiedDiff:     e.UnifiedDiff,
		}
	}
	return DiffOut{
		ManifestA: r.ManifestA.ManifestID,
		ManifestB: r.ManifestB.ManifestID,
		Changed:   changed,
		Added:     added,
		Removed:   removed,
		Unchanged: unchanged,
		Entries:   entries,
	}
}

// ReplayEntryOut verifies one manifest entry: recorded_hash is what the
// manifest committed to, replayed_hash is what re-hashing the bytes read
// back out of the blob store produced.
type ReplayEntryOut struct {
	Position          int             `json:"position"`
	URI               string          `json:"uri"`
	MaterializationID string          `json:"materialization_id"`
	RecordedHash      string          `json:"recorded_hash"`
	ReplayedHash      string          `json:"replayed_hash"`
	Match             bool            `json:"match"`
	Content           *ContentPayload `json:"content,omitempty"`
}

// ReplayOut is a whole manifest reconstructed from storage alone.
type ReplayOut struct {
	ManifestID string           `json:"manifest_id"`
	RunID      string           `json:"run_id"`
	AllMatch   bool             `json:"all_match"`
	Entries    []ReplayEntryOut `json:"entries"`
}

func replayOut(r replay.Result, entries []ReplayEntryOut) ReplayOut {
	return ReplayOut{
		ManifestID: r.Manifest.ManifestID,
		RunID:      r.Manifest.RunID,
		AllMatch:   r.AllMatch(),
		Entries:    entries,
	}
}

// TagInfo is a named, movable pointer from a resource to one of its
// snapshots.
type TagInfo struct {
	URI        string `json:"uri"`
	Tag        string `json:"tag"`
	SnapshotID string `json:"snapshot_id"`
	UpdatedAt  string `json:"updated_at"`
	// Reference is the string to read this tag by, uri@tag — spelling it
	// out saves a model from assembling it and getting the syntax wrong.
	Reference string `json:"reference"`
}

func tagInfo(t tag.Tag) TagInfo {
	return TagInfo{
		URI:        t.ResourceURI,
		Tag:        t.Name,
		SnapshotID: t.SnapshotID,
		UpdatedAt:  formatTime(t.UpdatedAt),
		Reference:  t.ResourceURI + "@" + t.Name,
	}
}
