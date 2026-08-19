// Package wire defines the JSON request/response shapes for the Ctx HTTP
// API, plus conversions to/from the domain types in internal/*. It has no
// server or client logic of its own — internal/api encodes/decodes these
// types on the server side, internal/client/remote does the same on the
// client side, so both sides of the wire agree on one definition.
package wire

import (
	"time"

	"ctx/internal/diff"
	"ctx/internal/manifest"
	"ctx/internal/materialization"
	"ctx/internal/policy"
	"ctx/internal/redact"
	"ctx/internal/replay"
	"ctx/internal/resolver"
	"ctx/internal/resource"
	"ctx/internal/snapshot"
	"ctx/internal/source"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

// --- Source / Policy ---

type SourceWire struct {
	Kind       string                `json:"kind"`
	Filesystem *FilesystemConfigWire `json:"filesystem,omitempty"`
	GitHub     *GitHubConfigWire     `json:"github,omitempty"`
	HTTP       *HTTPConfigWire       `json:"http,omitempty"`
}

type FilesystemConfigWire struct {
	Path string `json:"path"`
}

type GitHubConfigWire struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref"`
}

type HTTPConfigWire struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func SourceToWire(cfg source.Config) SourceWire {
	w := SourceWire{Kind: string(cfg.Kind)}
	if cfg.Filesystem != nil {
		w.Filesystem = &FilesystemConfigWire{Path: cfg.Filesystem.Path}
	}
	if cfg.GitHub != nil {
		w.GitHub = &GitHubConfigWire{Owner: cfg.GitHub.Owner, Repo: cfg.GitHub.Repo, Path: cfg.GitHub.Path, Ref: cfg.GitHub.Ref}
	}
	if cfg.HTTP != nil {
		w.HTTP = &HTTPConfigWire{URL: cfg.HTTP.URL, Headers: cfg.HTTP.Headers}
	}
	return w
}

// SourceToWireRedacted is SourceToWire with sensitive HTTP header values
// masked — use this (never SourceToWire) when building an API *response*.
// SourceToWire itself stays unredacted because it's also used to encode
// *requests* (e.g. RegisterResourceRequest on the client side), which must
// carry the real header values for ctxd to authenticate with.
func SourceToWireRedacted(cfg source.Config) SourceWire {
	w := SourceToWire(cfg)
	if w.HTTP != nil {
		redactedHTTP := *w.HTTP
		redactedHTTP.Headers = redact.Headers(w.HTTP.Headers)
		w.HTTP = &redactedHTTP
	}
	return w
}

func SourceFromWire(w SourceWire) source.Config {
	cfg := source.Config{Kind: source.Kind(w.Kind)}
	if w.Filesystem != nil {
		cfg.Filesystem = &source.FilesystemConfig{Path: w.Filesystem.Path}
	}
	if w.GitHub != nil {
		cfg.GitHub = &source.GitHubConfig{Owner: w.GitHub.Owner, Repo: w.GitHub.Repo, Path: w.GitHub.Path, Ref: w.GitHub.Ref}
	}
	if w.HTTP != nil {
		cfg.HTTP = &source.HTTPConfig{URL: w.HTTP.URL, Headers: w.HTTP.Headers}
	}
	return cfg
}

type PolicyWire struct {
	Strategy         string `json:"strategy"`
	MaxAgeSeconds    int64  `json:"max_age_seconds,omitempty"`
	PinnedSnapshotID string `json:"pinned_snapshot_id,omitempty"`
}

func PolicyToWire(p policy.Policy) PolicyWire {
	w := PolicyWire{Strategy: string(p.Strategy), PinnedSnapshotID: p.PinnedSnapshotID}
	if p.MaxAge > 0 {
		w.MaxAgeSeconds = int64(p.MaxAge.Seconds())
	}
	return w
}

func PolicyFromWire(w PolicyWire) policy.Policy {
	return policy.Policy{
		Strategy:         policy.Strategy(w.Strategy),
		MaxAge:           time.Duration(w.MaxAgeSeconds) * time.Second,
		PinnedSnapshotID: w.PinnedSnapshotID,
	}
}

// --- Resource ---

type ResourceWire struct {
	URI               string     `json:"uri"`
	Namespace         string     `json:"namespace"`
	Path              string     `json:"path"`
	Source            SourceWire `json:"source"`
	Policy            PolicyWire `json:"policy"`
	CurrentSnapshotID string     `json:"current_snapshot_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func ResourceToWire(r resource.Resource) ResourceWire {
	return ResourceWire{
		URI: r.URI, Namespace: r.Namespace, Path: r.Path,
		Source: SourceToWire(r.SourceConfig), Policy: PolicyToWire(r.Policy),
		CurrentSnapshotID: r.CurrentSnapshotID,
		CreatedAt:         r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// ResourceToWireRedacted is ResourceToWire with sensitive HTTP header
// values masked — use this for API responses (see SourceToWireRedacted).
func ResourceToWireRedacted(r resource.Resource) ResourceWire {
	w := ResourceToWire(r)
	w.Source = SourceToWireRedacted(r.SourceConfig)
	return w
}

func ResourceFromWire(w ResourceWire) resource.Resource {
	return resource.Resource{
		URI: w.URI, Namespace: w.Namespace, Path: w.Path,
		SourceConfig: SourceFromWire(w.Source), Policy: PolicyFromWire(w.Policy),
		CurrentSnapshotID: w.CurrentSnapshotID,
		CreatedAt:         w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
}

// RegisterResourceRequest is the POST /v1/resources body.
type RegisterResourceRequest struct {
	URI    string     `json:"uri"`
	Source SourceWire `json:"source"`
	Policy PolicyWire `json:"policy"`
}

// --- Snapshot ---

type SnapshotWire struct {
	ID             string            `json:"id"`
	ResourceURI    string            `json:"resource_uri"`
	SourceRevision string            `json:"source_revision"`
	ContentHash    string            `json:"content_hash"`
	ObservedAt     time.Time         `json:"observed_at"`
	CreatedAt      time.Time         `json:"created_at"`
	ContentType    string            `json:"content_type"`
	Bytes          int64             `json:"bytes"`
	Provenance     map[string]string `json:"provenance"`
}

func SnapshotToWire(s snapshot.Snapshot) SnapshotWire {
	return SnapshotWire{
		ID: s.SnapshotID, ResourceURI: s.ResourceURI, SourceRevision: s.SourceRevision,
		ContentHash: s.ContentHash, ObservedAt: s.ObservedAt, CreatedAt: s.CreatedAt,
		ContentType: s.ContentType, Bytes: s.Bytes, Provenance: s.Provenance,
	}
}

func SnapshotFromWire(w SnapshotWire) snapshot.Snapshot {
	return snapshot.Snapshot{
		SnapshotID: w.ID, ResourceURI: w.ResourceURI, SourceRevision: w.SourceRevision,
		ContentHash: w.ContentHash, ObservedAt: w.ObservedAt, CreatedAt: w.CreatedAt,
		ContentType: w.ContentType, Bytes: w.Bytes, Provenance: w.Provenance,
	}
}

// --- Materialization ---

type MaterializationWire struct {
	ID          string    `json:"id"`
	SnapshotID  string    `json:"snapshot_id"`
	Strategy    string    `json:"strategy"`
	ContentHash string    `json:"content_hash"`
	Bytes       int64     `json:"bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

func MaterializationToWire(m materialization.Materialization) MaterializationWire {
	return MaterializationWire{
		ID: m.MaterializationID, SnapshotID: m.SnapshotID, Strategy: string(m.Strategy),
		ContentHash: m.ContentHash, Bytes: m.Bytes, CreatedAt: m.CreatedAt,
	}
}

func MaterializationFromWire(w MaterializationWire) materialization.Materialization {
	return materialization.Materialization{
		MaterializationID: w.ID, SnapshotID: w.SnapshotID, Strategy: materialization.Strategy(w.Strategy),
		ContentHash: w.ContentHash, Bytes: w.Bytes, CreatedAt: w.CreatedAt,
	}
}

// --- Resolve ---

type ResolveRequest struct {
	URI string `json:"uri"`
}

// ResolveResourceWire carries just enough of the Resource for a resolve
// response — the URI and the policy that governed this resolution.
type ResolveResourceWire struct {
	URI    string     `json:"uri"`
	Policy PolicyWire `json:"policy"`
}

type FreshnessWire struct {
	// Status matches policy.Decision.String(): "fetch" | "use_current" | "use_pinned".
	Status     string  `json:"status"`
	AgeSeconds float64 `json:"age_seconds"`
}

type ResolveResponse struct {
	Resource        ResolveResourceWire `json:"resource"`
	Snapshot        SnapshotWire        `json:"snapshot"`
	Materialization MaterializationWire `json:"materialization"`
	Freshness       FreshnessWire       `json:"freshness"`
	// Content is base64-encoded automatically by encoding/json for []byte.
	Content []byte `json:"content"`
}

func ResolveResultToWire(result resolver.ResolveResult, now time.Time) ResolveResponse {
	return ResolveResponse{
		Resource:        ResolveResourceWire{URI: result.Resource.URI, Policy: PolicyToWire(result.Resource.Policy)},
		Snapshot:        SnapshotToWire(result.Snapshot),
		Materialization: MaterializationToWire(result.Materialization),
		Freshness: FreshnessWire{
			Status:     result.Decision.String(),
			AgeSeconds: now.Sub(result.Snapshot.ObservedAt).Seconds(),
		},
		Content: result.Content,
	}
}

func ResolveResponseToResult(w ResolveResponse) resolver.ResolveResult {
	var decision policy.Decision
	switch w.Freshness.Status {
	case policy.DecisionUseCurrent.String():
		decision = policy.DecisionUseCurrent
	case policy.DecisionUsePinned.String():
		decision = policy.DecisionUsePinned
	default:
		decision = policy.DecisionFetch
	}
	return resolver.ResolveResult{
		Resource:        resource.Resource{URI: w.Resource.URI, Policy: PolicyFromWire(w.Resource.Policy)},
		Snapshot:        SnapshotFromWire(w.Snapshot),
		Materialization: MaterializationFromWire(w.Materialization),
		Content:         w.Content,
		Decision:        decision,
	}
}

// --- Manifest ---

type ManifestEntryWire struct {
	Position          int    `json:"position"`
	URI               string `json:"uri"`
	SnapshotID        string `json:"snapshot_id"`
	MaterializationID string `json:"materialization_id"`
	ContentHash       string `json:"content_hash"`
}

type ManifestWire struct {
	ManifestID string              `json:"manifest_id"`
	RunID      string              `json:"run_id"`
	CreatedAt  time.Time           `json:"created_at"`
	Entries    []ManifestEntryWire `json:"entries"`
}

func ManifestToWire(m manifest.Manifest) ManifestWire {
	entries := make([]ManifestEntryWire, len(m.Entries))
	for i, e := range m.Entries {
		entries[i] = ManifestEntryWire{
			Position: e.Position, URI: e.URI, SnapshotID: e.SnapshotID,
			MaterializationID: e.MaterializationID, ContentHash: e.ContentHash,
		}
	}
	return ManifestWire{ManifestID: m.ManifestID, RunID: m.RunID, CreatedAt: m.CreatedAt, Entries: entries}
}

func ManifestFromWire(w ManifestWire) manifest.Manifest {
	entries := make([]manifest.Entry, len(w.Entries))
	for i, e := range w.Entries {
		entries[i] = manifest.Entry{
			Position: e.Position, URI: e.URI, SnapshotID: e.SnapshotID,
			MaterializationID: e.MaterializationID, ContentHash: e.ContentHash,
		}
	}
	return manifest.Manifest{ManifestID: w.ManifestID, RunID: w.RunID, CreatedAt: w.CreatedAt, Entries: entries}
}

// --- Run ---

type RunStartRequest struct {
	RunID string `json:"run_id"`
}

type RunMountRequest struct {
	RunID string `json:"run_id"`
	URI   string `json:"uri"`
}

type RunMountResponse struct {
	Position int             `json:"position"`
	Resolve  ResolveResponse `json:"resolve"`
}

type RunCommitRequest struct {
	RunID string `json:"run_id"`
}

// --- Diff ---

type DiffEntryWire struct {
	URI         string `json:"uri"`
	Status      string `json:"status"`
	SnapshotIDA string `json:"snapshot_id_a,omitempty"`
	SnapshotIDB string `json:"snapshot_id_b,omitempty"`
	UnifiedDiff string `json:"unified_diff,omitempty"`
}

type DiffResponse struct {
	ManifestA ManifestWire    `json:"manifest_a"`
	ManifestB ManifestWire    `json:"manifest_b"`
	Entries   []DiffEntryWire `json:"entries"`
}

func DiffResultToWire(r diff.Result) DiffResponse {
	entries := make([]DiffEntryWire, len(r.Entries))
	for i, e := range r.Entries {
		entries[i] = DiffEntryWire{
			URI: e.URI, Status: string(e.Status),
			SnapshotIDA: e.SnapshotIDA, SnapshotIDB: e.SnapshotIDB, UnifiedDiff: e.UnifiedDiff,
		}
	}
	return DiffResponse{ManifestA: ManifestToWire(r.ManifestA), ManifestB: ManifestToWire(r.ManifestB), Entries: entries}
}

func DiffResponseToResult(w DiffResponse) diff.Result {
	entries := make([]diff.EntryDiff, len(w.Entries))
	for i, e := range w.Entries {
		entries[i] = diff.EntryDiff{
			URI: e.URI, Status: diff.Status(e.Status),
			SnapshotIDA: e.SnapshotIDA, SnapshotIDB: e.SnapshotIDB, UnifiedDiff: e.UnifiedDiff,
		}
	}
	return diff.Result{ManifestA: ManifestFromWire(w.ManifestA), ManifestB: ManifestFromWire(w.ManifestB), Entries: entries}
}

// --- Replay ---

type ReplayEntryWire struct {
	Position          int    `json:"position"`
	URI               string `json:"uri"`
	MaterializationID string `json:"materialization_id"`
	RecordedHash      string `json:"recorded_hash"`
	ReplayedHash      string `json:"replayed_hash"`
	Content           []byte `json:"content"`
	Match             bool   `json:"match"`
}

type ReplayResponse struct {
	Manifest ManifestWire      `json:"manifest"`
	Entries  []ReplayEntryWire `json:"entries"`
}

func ReplayResultToWire(r replay.Result) ReplayResponse {
	entries := make([]ReplayEntryWire, len(r.Entries))
	for i, e := range r.Entries {
		entries[i] = ReplayEntryWire{
			Position: e.Position, URI: e.URI, MaterializationID: e.MaterializationID,
			RecordedHash: e.RecordedHash, ReplayedHash: e.ReplayedHash,
			Content: e.Content, Match: e.Match,
		}
	}
	return ReplayResponse{Manifest: ManifestToWire(r.Manifest), Entries: entries}
}

func ReplayResponseToResult(w ReplayResponse) replay.Result {
	entries := make([]replay.EntryResult, len(w.Entries))
	for i, e := range w.Entries {
		entries[i] = replay.EntryResult{
			Position: e.Position, URI: e.URI, MaterializationID: e.MaterializationID,
			RecordedHash: e.RecordedHash, ReplayedHash: e.ReplayedHash,
			Content: e.Content, Match: e.Match,
		}
	}
	return replay.Result{Manifest: ManifestFromWire(w.Manifest), Entries: entries}
}

// --- Resource list ---

type ResourceListResponse struct {
	Resources []ResourceWire `json:"resources"`
}

type HistoryResponse struct {
	Snapshots []SnapshotWire `json:"snapshots"`
}
