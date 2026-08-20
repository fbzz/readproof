package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"ctx/internal/evidence"
)

// Tool inputs. Field tags carry the JSON Schema descriptions the SDK
// derives each tool's input schema from, so the schema a model sees and
// the Go struct a handler receives can never drift apart.

type emptyIn struct{}

type uriIn struct {
	URI string `json:"uri" jsonschema:"a Ctx resource reference, ctx://<namespace>/<path>, optionally with a trailing @<tag>"`
}

type resolveIn struct {
	URI string `json:"uri" jsonschema:"the resource to read, ctx://<namespace>/<path>; append @<tag> (e.g. ctx://acme/policies/refunds@prod) to pin exactly the snapshot that tag names"`
}

type runIDIn struct {
	RunID string `json:"run_id" jsonschema:"an identifier you choose for this run; reuse the same value across start, mount, and commit"`
}

type runMountIn struct {
	RunID string `json:"run_id" jsonschema:"the run id passed to ctx_run_start"`
	URI   string `json:"uri" jsonschema:"the resource to mount, ctx://<namespace>/<path>, optionally with @<tag>"`
}

type targetIn struct {
	Target string `json:"target" jsonschema:"a manifest id, or the run id it was committed from"`
}

type diffIn struct {
	A string `json:"a" jsonschema:"the baseline manifest id or run id"`
	B string `json:"b" jsonschema:"the manifest id or run id to compare against the baseline"`
}

type replayIn struct {
	Target         string `json:"target" jsonschema:"a manifest id, or the run id it was committed from"`
	IncludeContent bool   `json:"include_content,omitempty" jsonschema:"also return each entry's reconstructed bytes; off by default because verification only needs the hashes"`
}

type tagSetIn struct {
	URI        string `json:"uri" jsonschema:"the resource the tag belongs to, ctx://<namespace>/<path>"`
	Tag        string `json:"tag" jsonschema:"the tag name, matching ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$"`
	SnapshotID string `json:"snapshot_id" jsonschema:"the snapshot to point the tag at; it must be a snapshot of this same resource (see ctx_history)"`
}

type tagDeleteIn struct {
	URI string `json:"uri" jsonschema:"the resource the tag belongs to"`
	Tag string `json:"tag" jsonschema:"the tag name to delete; the snapshot it pointed at is untouched"`
}

type evidenceIn struct {
	Target      string `json:"target" jsonschema:"a manifest id, or the run id it was committed from"`
	WithContent bool   `json:"with_content,omitempty" jsonschema:"embed each entry's bytes in the bundle as base64; off by default so the bundle proves what was read without disclosing it"`
}

// Tool outputs that wrap a single value.

type ResourceListOut struct {
	Resources []ResourceInfo `json:"resources"`
}

type HistoryOut struct {
	URI       string         `json:"uri"`
	Snapshots []SnapshotInfo `json:"snapshots"`
}

type RunStartOut struct {
	RunID string `json:"run_id"`
	// NextStep spells out the lifecycle so a model that called start in
	// isolation knows a manifest only exists after commit.
	NextStep string `json:"next_step"`
}

type MountOut struct {
	RunID    string     `json:"run_id"`
	Position int        `json:"position"`
	Resolved ResolveOut `json:"resolved"`
}

type TagListOut struct {
	URI  string    `json:"uri"`
	Tags []TagInfo `json:"tags"`
}

type TagDeleteOut struct {
	URI     string `json:"uri"`
	Tag     string `json:"tag"`
	Deleted bool   `json:"deleted"`
}

// readOnly and mutating are the tool annotation hints. Resolving and
// mounting are NOT read-only: both may fetch from the source and record a
// new snapshot, which is the whole point of Ctx observing a read.
var (
	readOnly = &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	mutating = &mcpsdk.ToolAnnotations{IdempotentHint: false}
)

func (s *server) registerTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_resources_list",
		Title: "List Ctx resources",
		Description: "List every document registered in Ctx, with its ctx:// URI, where its bytes come from, and the freshness policy that governs reading it. " +
			"Call this first to discover what you are allowed to read.",
		Annotations: readOnly,
	}, s.toolResourcesList)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_resolve",
		Title: "Read a Ctx resource",
		Description: "Read one document and get its bytes together with the snapshot id, content hash, and source revision that identify exactly what you read. " +
			"Use it whenever you need the current governed version of a policy, spec, or runbook; append @<tag> to the URI to read a pinned snapshot instead. " +
			"Note: depending on the resource's policy this may fetch from the source and record a new snapshot.",
		Annotations: mutating,
	}, s.toolResolve)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_history",
		Title: "List a resource's snapshots",
		Description: "List every snapshot Ctx has recorded for one resource, newest first, with the tags that point at each. " +
			"Use it to find the snapshot id to pin with ctx_tag_set, or to see when and how often a document changed.",
		Annotations: readOnly,
	}, s.toolHistory)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_run_start",
		Title: "Start a Ctx run",
		Description: "Open a run: the container that records everything you are about to read. " +
			"Call it once with an id you choose, then ctx_run_mount for each document, then ctx_run_commit — which returns the manifest id that names the complete, replayable set of bytes this run saw.",
		Annotations: mutating,
	}, s.toolRunStart)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_run_mount",
		Title: "Mount a resource into a run",
		Description: "Read a document and record it in an open run at the next position. " +
			"Use this instead of ctx_resolve whenever the work you are doing should be auditable: mounted reads become manifest entries you can later diff, replay, and export as evidence. " +
			"Mount order is preserved because it can change what a model concludes. Like ctx_resolve, this may fetch from the source and record a new snapshot.",
		Annotations: mutating,
	}, s.toolRunMount)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_run_commit",
		Title: "Commit a run into a manifest",
		Description: "Close a run and freeze everything mounted into it as an immutable manifest. " +
			"Returns the manifest id — cite it in whatever you produce, because it is the single handle for ctx_manifest, ctx_diff, ctx_replay, and ctx_evidence_export.",
		Annotations: mutating,
	}, s.toolRunCommit)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_manifest",
		Title: "Inspect a manifest",
		Description: "Show a committed manifest: every document the run read, in mount order, with the snapshot id and content hash of each. " +
			"Use it to answer 'what exactly did that run see?' from a manifest id or run id alone.",
		Annotations: readOnly,
	}, s.toolManifest)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_diff",
		Title: "Diff two runs",
		Description: "Compare what two runs read. For each document it reports added/removed/changed/unchanged, the unified text diff of any change, and the provenance behind it — each side's source revision, observation time, and pinned tag. " +
			"Use it to explain why two runs of the same task disagreed.",
		Annotations: readOnly,
	}, s.toolDiff)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_replay",
		Title: "Replay a manifest",
		Description: "Reconstruct a manifest's bytes from Ctx's own storage and re-hash them, without contacting any source. " +
			"Use it to prove a past run is still reproducible, or (with include_content) to get back exactly the bytes that run saw even after the sources changed.",
		Annotations: readOnly,
	}, s.toolReplay)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_tag_set",
		Title: "Point a tag at a snapshot",
		Description: "Create or move a named pointer from a resource to one of its snapshots, so it can be read as ctx://<namespace>/<path>@<tag>. " +
			"Use it to freeze a known-good version (e.g. 'prod') that later reads resolve to with no fetch and no policy evaluation.",
		Annotations: mutating,
	}, s.toolTagSet)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "ctx_tag_list",
		Title:       "List a resource's tags",
		Description: "List the tags on one resource and the snapshot each points at, with the exact uri@tag reference to read it by.",
		Annotations: readOnly,
	}, s.toolTagList)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_tag_delete",
		Title: "Delete a tag",
		Description: "Remove a tag. The snapshot it pointed at is untouched and still readable by its snapshot id; only the name goes away. " +
			"Manifests that mounted the tag keep replaying identically, because they recorded the snapshot, not the name.",
		Annotations: mutating,
	}, s.toolTagDelete)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:  "ctx_evidence_export",
		Title: "Export an evidence bundle",
		Description: "Produce a portable, tamper-evident record of one run: an in-toto statement whose digest is a Merkle root over the manifest entries, plus each document's provenance, the resource definitions behind them, and a replay verification. " +
			"Use it when someone needs to audit what the agent read — 'with_content' additionally embeds the bytes.",
		Annotations: readOnly,
	}, s.toolEvidenceExport)
}

// Handlers. Each returns a typed value; the SDK marshals it into both
// CallToolResult.StructuredContent and a JSON text content block, and
// turns a returned error into an IsError tool result rather than a
// protocol-level failure, so a model can read the message and correct
// itself.

func (s *server) toolResourcesList(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyIn) (*mcpsdk.CallToolResult, ResourceListOut, error) {
	resources, err := s.client.ListResources(ctx)
	if err != nil {
		return nil, ResourceListOut{}, fmt.Errorf("list resources: %w", err)
	}
	out := ResourceListOut{Resources: make([]ResourceInfo, len(resources))}
	for i, r := range resources {
		out.Resources[i] = resourceInfo(r)
	}
	return nil, out, nil
}

func (s *server) toolResolve(ctx context.Context, _ *mcpsdk.CallToolRequest, in resolveIn) (*mcpsdk.CallToolResult, ResolveOut, error) {
	result, err := s.client.Resolve(ctx, in.URI)
	if err != nil {
		return nil, ResolveOut{}, fmt.Errorf("resolve %s: %w", in.URI, err)
	}
	enc := encode(result.Content, result.Snapshot.ContentType, result.Snapshot.ContentHash, s.opts.maxInlineBytes())
	return nil, resolveOut(result, contentPayload(enc)), nil
}

func (s *server) toolHistory(ctx context.Context, _ *mcpsdk.CallToolRequest, in uriIn) (*mcpsdk.CallToolResult, HistoryOut, error) {
	uri, err := bareURI(in.URI)
	if err != nil {
		return nil, HistoryOut{}, err
	}
	snapshots, err := s.client.History(ctx, uri)
	if err != nil {
		return nil, HistoryOut{}, fmt.Errorf("history %s: %w", uri, err)
	}
	tags, err := s.client.ListTags(ctx, uri)
	if err != nil {
		return nil, HistoryOut{}, fmt.Errorf("list tags %s: %w", uri, err)
	}
	// ListTags is sorted by name, so the per-snapshot slices are stable.
	bySnapshot := make(map[string][]string, len(tags))
	for _, t := range tags {
		bySnapshot[t.SnapshotID] = append(bySnapshot[t.SnapshotID], t.Name)
	}

	out := HistoryOut{URI: uri, Snapshots: make([]SnapshotInfo, len(snapshots))}
	for i, snap := range snapshots {
		out.Snapshots[i] = snapshotInfo(snap, bySnapshot[snap.SnapshotID])
	}
	return nil, out, nil
}

func (s *server) toolRunStart(ctx context.Context, _ *mcpsdk.CallToolRequest, in runIDIn) (*mcpsdk.CallToolResult, RunStartOut, error) {
	if err := s.client.RunStart(ctx, in.RunID); err != nil {
		return nil, RunStartOut{}, fmt.Errorf("start run %s: %w", in.RunID, err)
	}
	return nil, RunStartOut{
		RunID:    in.RunID,
		NextStep: "call ctx_run_mount for each resource this run reads, then ctx_run_commit to get the manifest id",
	}, nil
}

func (s *server) toolRunMount(ctx context.Context, _ *mcpsdk.CallToolRequest, in runMountIn) (*mcpsdk.CallToolResult, MountOut, error) {
	result, position, err := s.client.RunMount(ctx, in.RunID, in.URI)
	if err != nil {
		return nil, MountOut{}, fmt.Errorf("mount %s into run %s: %w", in.URI, in.RunID, err)
	}
	enc := encode(result.Content, result.Snapshot.ContentType, result.Snapshot.ContentHash, s.opts.maxInlineBytes())
	return nil, MountOut{
		RunID:    in.RunID,
		Position: position,
		Resolved: resolveOut(result, contentPayload(enc)),
	}, nil
}

func (s *server) toolRunCommit(ctx context.Context, _ *mcpsdk.CallToolRequest, in runIDIn) (*mcpsdk.CallToolResult, ManifestOut, error) {
	man, err := s.client.RunCommit(ctx, in.RunID)
	if err != nil {
		return nil, ManifestOut{}, fmt.Errorf("commit run %s: %w", in.RunID, err)
	}
	return nil, manifestOut(man), nil
}

func (s *server) toolManifest(ctx context.Context, _ *mcpsdk.CallToolRequest, in targetIn) (*mcpsdk.CallToolResult, ManifestOut, error) {
	man, err := s.client.GetManifest(ctx, in.Target)
	if err != nil {
		return nil, ManifestOut{}, fmt.Errorf("get manifest %s: %w", in.Target, err)
	}
	return nil, manifestOut(man), nil
}

func (s *server) toolDiff(ctx context.Context, _ *mcpsdk.CallToolRequest, in diffIn) (*mcpsdk.CallToolResult, DiffOut, error) {
	result, err := s.client.Diff(ctx, in.A, in.B)
	if err != nil {
		return nil, DiffOut{}, fmt.Errorf("diff %s %s: %w", in.A, in.B, err)
	}
	return nil, diffOut(result), nil
}

func (s *server) toolReplay(ctx context.Context, _ *mcpsdk.CallToolRequest, in replayIn) (*mcpsdk.CallToolResult, ReplayOut, error) {
	result, err := s.client.Replay(ctx, in.Target)
	if err != nil {
		return nil, ReplayOut{}, fmt.Errorf("replay %s: %w", in.Target, err)
	}
	entries := make([]ReplayEntryOut, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = ReplayEntryOut{
			Position:          e.Position,
			URI:               e.URI,
			MaterializationID: e.MaterializationID,
			RecordedHash:      e.RecordedHash,
			ReplayedHash:      e.ReplayedHash,
			Match:             e.Match,
		}
		if in.IncludeContent {
			// Replay carries no content type — the materialization is raw
			// bytes — so encode sniffs them.
			enc := encode(e.Content, "", e.RecordedHash, s.opts.maxInlineBytes())
			entries[i].Content = contentPayload(enc)
		}
	}
	return nil, replayOut(result, entries), nil
}

func (s *server) toolTagSet(ctx context.Context, _ *mcpsdk.CallToolRequest, in tagSetIn) (*mcpsdk.CallToolResult, TagInfo, error) {
	uri, err := bareURI(in.URI)
	if err != nil {
		return nil, TagInfo{}, err
	}
	t, err := s.client.SetTag(ctx, uri, in.Tag, in.SnapshotID)
	if err != nil {
		return nil, TagInfo{}, fmt.Errorf("set tag %s@%s: %w", uri, in.Tag, err)
	}
	return nil, tagInfo(t), nil
}

func (s *server) toolTagList(ctx context.Context, _ *mcpsdk.CallToolRequest, in uriIn) (*mcpsdk.CallToolResult, TagListOut, error) {
	uri, err := bareURI(in.URI)
	if err != nil {
		return nil, TagListOut{}, err
	}
	tags, err := s.client.ListTags(ctx, uri)
	if err != nil {
		return nil, TagListOut{}, fmt.Errorf("list tags %s: %w", uri, err)
	}
	out := TagListOut{URI: uri, Tags: make([]TagInfo, len(tags))}
	for i, t := range tags {
		out.Tags[i] = tagInfo(t)
	}
	return nil, out, nil
}

func (s *server) toolTagDelete(ctx context.Context, _ *mcpsdk.CallToolRequest, in tagDeleteIn) (*mcpsdk.CallToolResult, TagDeleteOut, error) {
	uri, err := bareURI(in.URI)
	if err != nil {
		return nil, TagDeleteOut{}, err
	}
	if err := s.client.DeleteTag(ctx, uri, in.Tag); err != nil {
		return nil, TagDeleteOut{}, fmt.Errorf("delete tag %s@%s: %w", uri, in.Tag, err)
	}
	return nil, TagDeleteOut{URI: uri, Tag: in.Tag, Deleted: true}, nil
}

func (s *server) toolEvidenceExport(ctx context.Context, _ *mcpsdk.CallToolRequest, in evidenceIn) (*mcpsdk.CallToolResult, evidence.Bundle, error) {
	// evidence.Build is composed purely from client.Client calls, so the
	// bundle an MCP caller gets is byte-identical to the one `ctx evidence
	// export` writes for the same target.
	bundle, err := evidence.Build(ctx, s.client, in.Target, evidence.Options{WithContent: in.WithContent})
	if err != nil {
		return nil, evidence.Bundle{}, fmt.Errorf("export evidence for %s: %w", in.Target, err)
	}
	return nil, bundle, nil
}
