package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"ctx/internal/app"
	"ctx/internal/client"
	"ctx/internal/client/local"
	"ctx/internal/evidence"
	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/source"
)

const (
	demoURI         = "ctx://demo/policies/refunds"
	originalContent = "Products can be refunded within 30 days.\n"
	updatedContent  = "Products can be refunded within 14 days.\n"
)

// newDemoClient stands up the refund-agent demo (see internal/e2e) over an
// embedded app in a temp data directory, and returns a client plus the
// fixture path, so a test can change the document under Ctx's feet.
func newDemoClient(t *testing.T) (client.Client, string) {
	t.Helper()

	fixturePath := filepath.Join(t.TempDir(), "refunds.md")
	if err := os.WriteFile(fixturePath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	c := local.New(a)
	t.Cleanup(func() { c.Close() })

	registerDemoResource(t, c, fixturePath)
	return c, fixturePath
}

func registerDemoResource(t *testing.T, c client.Client, fixturePath string) {
	t.Helper()
	parsed, err := resource.ParseURI(demoURI)
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	err = c.RegisterResource(context.Background(), resource.Resource{
		URI:       demoURI,
		Namespace: parsed.Namespace,
		Path:      parsed.Path,
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixturePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	})
	if err != nil {
		t.Fatalf("register resource: %v", err)
	}
}

// connect runs a real MCP client against a real MCP server over the SDK's
// in-memory transport pair — the whole JSON-RPC round-trip, schema
// validation included, without a subprocess.
func connect(t *testing.T, c client.Client) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	srv := NewServer(c, Options{})
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "ctx-test", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// callTool calls a tool, fails the test on any error result, and decodes
// the structured content into T — asserting on the way through that the
// tool really did return structuredContent, not just text.
func callTool[T any](t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s returned an error result: %s", name, toolText(res))
	}
	if res.StructuredContent == nil {
		t.Fatalf("call %s returned no structured content", name)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal %s structured content: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s structured content: %v", name, err)
	}
	return out
}

func toolText(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func metaString(t *testing.T, meta map[string]any, key string) string {
	t.Helper()
	v, ok := meta[key]
	if !ok {
		t.Fatalf("_meta is missing %q: %v", key, meta)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("_meta %q = %T, want string", key, v)
	}
	return s
}

// TestServerEndToEnd drives the whole MCP surface over one session, in the
// order an agent would: discover, read, pin, run, commit, compare, replay,
// export.
func TestServerEndToEnd(t *testing.T) {
	c, fixturePath := newDemoClient(t)
	cs := connect(t, c)
	ctx := context.Background()

	// The instructions field is how a model learns what Ctx is; an empty
	// one would leave it guessing.
	if instr := cs.InitializeResult().Instructions; !strings.Contains(instr, "ctx://") || !strings.Contains(instr, "manifest id") {
		t.Fatalf("initialize instructions do not describe Ctx: %q", instr)
	}

	// --- resources/list, before anything has been resolved -------------
	listed, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(listed.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(listed.Resources))
	}
	entry := listed.Resources[0]
	if entry.URI != demoURI {
		t.Fatalf("resource uri = %q, want %q", entry.URI, demoURI)
	}
	if entry.Name != "policies/refunds" {
		t.Fatalf("resource name = %q, want the resource path", entry.Name)
	}
	if !strings.Contains(entry.Description, "filesystem") || !strings.Contains(entry.Description, "require_fresh") {
		t.Fatalf("resource description does not carry source kind + policy: %q", entry.Description)
	}
	if entry.MIMEType != "" {
		t.Fatalf("mime type should be unknown before the first resolve, got %q", entry.MIMEType)
	}

	// The template is what makes ctx://ns/path and ctx://ns/path@tag
	// readable at all, since no static registration can enumerate tags.
	templates, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}
	if len(templates.ResourceTemplates) != 1 || templates.ResourceTemplates[0].URITemplate != uriTemplate {
		t.Fatalf("unexpected resource templates: %+v", templates.ResourceTemplates)
	}

	// --- resources/read -------------------------------------------------
	read, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: demoURI})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(read.Contents))
	}
	contents := read.Contents[0]
	if contents.Text != originalContent {
		t.Fatalf("read text = %q, want %q", contents.Text, originalContent)
	}
	if len(contents.Blob) != 0 {
		t.Fatalf("markdown must come back as text, not a blob")
	}
	if contents.MIMEType != "text/markdown" {
		t.Fatalf("mime type = %q, want text/markdown", contents.MIMEType)
	}
	firstSnapshot := metaString(t, contents.Meta, "snapshot_id")
	if firstSnapshot == "" {
		t.Fatalf("_meta carries no snapshot id: %v", contents.Meta)
	}
	if got := metaString(t, contents.Meta, "decision"); got != "fetch" {
		t.Fatalf("decision = %q, want fetch on the first require_fresh read", got)
	}
	if got := metaString(t, contents.Meta, "content_hash"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("content hash = %q, want a sha256: digest", got)
	}
	for _, key := range []string{"source_revision", "observed_at", "materialization_id"} {
		if metaString(t, contents.Meta, key) == "" {
			t.Fatalf("_meta %q is empty: %v", key, contents.Meta)
		}
	}
	if ref, ok := contents.Meta["ref"]; !ok || ref != "" {
		t.Fatalf("_meta ref = %v, want an empty string for an untagged read", ref)
	}

	// Now that a snapshot exists, the listing can report its media type.
	listed, err = cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("re-list resources: %v", err)
	}
	if listed.Resources[0].MIMEType != "text/markdown" {
		t.Fatalf("mime type after resolve = %q, want text/markdown", listed.Resources[0].MIMEType)
	}
	if listed.Resources[0].Size != int64(len(originalContent)) {
		t.Fatalf("size after resolve = %d, want %d", listed.Resources[0].Size, len(originalContent))
	}

	// --- pin the current bytes behind a tag ------------------------------
	tagged := callTool[TagInfo](t, cs, "ctx_tag_set", map[string]any{
		"uri": demoURI, "tag": "prod", "snapshot_id": firstSnapshot,
	})
	if tagged.SnapshotID != firstSnapshot || tagged.Reference != demoURI+"@prod" {
		t.Fatalf("unexpected tag: %+v", tagged)
	}

	// --- run A: mount + commit -------------------------------------------
	callTool[RunStartOut](t, cs, "ctx_run_start", map[string]any{"run_id": "run-a"})
	mounted := callTool[MountOut](t, cs, "ctx_run_mount", map[string]any{"run_id": "run-a", "uri": demoURI})
	if mounted.Position != 0 {
		t.Fatalf("first mount position = %d, want 0", mounted.Position)
	}
	if mounted.Resolved.Content == nil || mounted.Resolved.Content.Text != originalContent {
		t.Fatalf("mount did not return the mounted bytes: %+v", mounted.Resolved.Content)
	}
	manA := callTool[ManifestOut](t, cs, "ctx_run_commit", map[string]any{"run_id": "run-a"})
	if manA.ManifestID == "" || len(manA.Entries) != 1 {
		t.Fatalf("unexpected manifest for run-a: %+v", manA)
	}

	// --- the document changes under us ------------------------------------
	if err := os.WriteFile(fixturePath, []byte(updatedContent), 0o644); err != nil {
		t.Fatalf("edit fixture: %v", err)
	}

	// A tagged read still delivers the old bytes, without consulting the
	// policy that would otherwise re-fetch.
	pinned, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: demoURI + "@prod"})
	if err != nil {
		t.Fatalf("read tagged resource: %v", err)
	}
	if pinned.Contents[0].Text != originalContent {
		t.Fatalf("tagged read = %q, want the pinned %q", pinned.Contents[0].Text, originalContent)
	}
	if got := metaString(t, pinned.Contents[0].Meta, "decision"); got != "use_tag" {
		t.Fatalf("tagged read decision = %q, want use_tag", got)
	}
	if got := metaString(t, pinned.Contents[0].Meta, "ref"); got != "prod" {
		t.Fatalf("tagged read ref = %q, want prod", got)
	}
	if got := metaString(t, pinned.Contents[0].Meta, "uri"); got != demoURI {
		t.Fatalf("tagged read uri = %q, want the bare %q", got, demoURI)
	}

	// --- run B: the same URI, the new bytes --------------------------------
	callTool[RunStartOut](t, cs, "ctx_run_start", map[string]any{"run_id": "run-b"})
	callTool[MountOut](t, cs, "ctx_run_mount", map[string]any{"run_id": "run-b", "uri": demoURI})
	manB := callTool[ManifestOut](t, cs, "ctx_run_commit", map[string]any{"run_id": "run-b"})
	if manA.Entries[0].ContentHash == manB.Entries[0].ContentHash {
		t.Fatalf("expected the two runs to disagree after the source changed")
	}

	// --- ctx_manifest resolves a run id as well as a manifest id -----------
	fetched := callTool[ManifestOut](t, cs, "ctx_manifest", map[string]any{"target": "run-a"})
	if fetched.ManifestID != manA.ManifestID {
		t.Fatalf("ctx_manifest by run id returned %s, want %s", fetched.ManifestID, manA.ManifestID)
	}

	// --- ctx_diff carries the why-fields and the unified diff --------------
	diffed := callTool[DiffOut](t, cs, "ctx_diff", map[string]any{"a": "run-a", "b": "run-b"})
	if diffed.Changed != 1 || diffed.Added != 0 || diffed.Removed != 0 {
		t.Fatalf("unexpected diff counts: %+v", diffed)
	}
	changed := diffed.Entries[0]
	if changed.Status != "changed" {
		t.Fatalf("diff status = %q, want changed", changed.Status)
	}
	if changed.SourceRevisionA == "" || changed.SourceRevisionB == "" || changed.SourceRevisionA == changed.SourceRevisionB {
		t.Fatalf("diff lost the per-side source revisions: %q vs %q", changed.SourceRevisionA, changed.SourceRevisionB)
	}
	if changed.ObservedAtA == "" || changed.ObservedAtB == "" {
		t.Fatalf("diff lost the per-side observation times: %+v", changed)
	}
	if !strings.Contains(changed.UnifiedDiff, "-Products can be refunded within 30 days.") ||
		!strings.Contains(changed.UnifiedDiff, "+Products can be refunded within 14 days.") {
		t.Fatalf("unified diff does not show the change:\n%s", changed.UnifiedDiff)
	}

	// --- ctx_replay reconstructs run A from storage alone ------------------
	replayed := callTool[ReplayOut](t, cs, "ctx_replay", map[string]any{"target": "run-a", "include_content": true})
	if !replayed.AllMatch || len(replayed.Entries) != 1 {
		t.Fatalf("replay of run-a failed: %+v", replayed)
	}
	if replayed.Entries[0].RecordedHash != replayed.Entries[0].ReplayedHash {
		t.Fatalf("replay hash mismatch: %+v", replayed.Entries[0])
	}
	if replayed.Entries[0].Content == nil || replayed.Entries[0].Content.Text != originalContent {
		t.Fatalf("replayed content = %+v, want the original bytes", replayed.Entries[0].Content)
	}
	// Without include_content the bytes stay out of the result entirely.
	quiet := callTool[ReplayOut](t, cs, "ctx_replay", map[string]any{"target": "run-a"})
	if quiet.Entries[0].Content != nil {
		t.Fatalf("replay returned content without include_content: %+v", quiet.Entries[0].Content)
	}

	// --- ctx_evidence_export matches evidence.Build exactly ----------------
	bundle := callTool[evidence.Bundle](t, cs, "ctx_evidence_export", map[string]any{"target": "run-a"})
	direct, err := evidence.Build(ctx, c, "run-a", evidence.Options{})
	if err != nil {
		t.Fatalf("evidence.Build: %v", err)
	}
	if bundle.Predicate.Merkle.Root != direct.Predicate.Merkle.Root {
		t.Fatalf("merkle root over MCP = %s, want %s", bundle.Predicate.Merkle.Root, direct.Predicate.Merkle.Root)
	}
	if len(bundle.Subject) != 1 || bundle.Subject[0].Digest.SHA256 != direct.Predicate.Merkle.Root {
		t.Fatalf("bundle subject does not carry the merkle root: %+v", bundle.Subject)
	}
	if bundle.Predicate.ManifestID != manA.ManifestID || bundle.Type != evidence.StatementType {
		t.Fatalf("unexpected bundle shape: type=%q manifest=%q", bundle.Type, bundle.Predicate.ManifestID)
	}
	if !bundle.Predicate.Replay.AllMatch {
		t.Fatalf("exported bundle reports a failed replay: %+v", bundle.Predicate.Replay)
	}
	if bundle.Predicate.Entries[0].ContentB64 != "" {
		t.Fatalf("bundle embedded content without with_content")
	}
	withContent := callTool[evidence.Bundle](t, cs, "ctx_evidence_export", map[string]any{"target": "run-a", "with_content": true})
	if withContent.Predicate.Entries[0].ContentB64 == "" {
		t.Fatalf("with_content did not embed the bytes")
	}

	// --- history and tags --------------------------------------------------
	history := callTool[HistoryOut](t, cs, "ctx_history", map[string]any{"uri": demoURI + "@prod"})
	if history.URI != demoURI {
		t.Fatalf("ctx_history did not strip the ref: %q", history.URI)
	}
	if len(history.Snapshots) < 2 {
		t.Fatalf("expected at least 2 snapshots in history, got %d", len(history.Snapshots))
	}
	foundTag := false
	for _, snap := range history.Snapshots {
		if snap.SnapshotID == firstSnapshot {
			for _, name := range snap.Tags {
				if name == "prod" {
					foundTag = true
				}
			}
		}
	}
	if !foundTag {
		t.Fatalf("history does not report the prod tag on %s: %+v", firstSnapshot, history.Snapshots)
	}

	tags := callTool[TagListOut](t, cs, "ctx_tag_list", map[string]any{"uri": demoURI})
	if len(tags.Tags) != 1 || tags.Tags[0].Tag != "prod" {
		t.Fatalf("unexpected tag list: %+v", tags.Tags)
	}
	deleted := callTool[TagDeleteOut](t, cs, "ctx_tag_delete", map[string]any{"uri": demoURI, "tag": "prod"})
	if !deleted.Deleted {
		t.Fatalf("tag delete reported failure: %+v", deleted)
	}
	if after := callTool[TagListOut](t, cs, "ctx_tag_list", map[string]any{"uri": demoURI}); len(after.Tags) != 0 {
		t.Fatalf("tag survived deletion: %+v", after.Tags)
	}
}

// TestUnknownReferences checks that every way of naming something that
// isn't there produces an orderly error a model can read, rather than a
// panic or a silent empty result.
func TestUnknownReferences(t *testing.T) {
	c, _ := newDemoClient(t)
	cs := connect(t, c)
	ctx := context.Background()

	// resources/read of an unregistered URI: a protocol error, but a
	// well-formed one — and the session survives it.
	if _, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "ctx://demo/policies/missing"}); err == nil {
		t.Fatalf("expected an error reading an unregistered resource")
	}

	// A URI that isn't a ctx:// reference at all matches no template, so
	// the SDK rejects it before any handler runs.
	if _, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "file:///etc/passwd"}); err == nil {
		t.Fatalf("expected an error reading a non-ctx URI")
	}

	// Tool failures come back as error *results*, so the model sees the
	// message and can correct itself.
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"unknown resource", "ctx_resolve", map[string]any{"uri": "ctx://demo/policies/missing"}, "not found"},
		{"unknown tag", "ctx_resolve", map[string]any{"uri": demoURI + "@nope"}, "tag"},
		{"malformed uri", "ctx_resolve", map[string]any{"uri": "not-a-uri"}, "ctx://"},
		{"unknown manifest", "ctx_manifest", map[string]any{"target": "run-nope"}, "not found"},
		{"unknown replay target", "ctx_replay", map[string]any{"target": "run-nope"}, "not found"},
		{"unknown snapshot for tag", "ctx_tag_set", map[string]any{"uri": demoURI, "tag": "prod", "snapshot_id": "snap_nope"}, "not found"},
		{"unknown evidence target", "ctx_evidence_export", map[string]any{"target": "run-nope"}, "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatalf("call %s returned a protocol error, want an error result: %v", tc.tool, err)
			}
			if !res.IsError {
				t.Fatalf("call %s succeeded, want an error result", tc.tool)
			}
			if text := toolText(res); !strings.Contains(strings.ToLower(text), tc.want) {
				t.Fatalf("error result %q does not mention %q", text, tc.want)
			}
		})
	}

	// The session is still usable after all of that.
	if _, err := cs.ListResources(ctx, nil); err != nil {
		t.Fatalf("session broken after error results: %v", err)
	}
}

// TestToolsAreDiscoverable guards the tool surface itself: an agent can
// only use what tools/list advertises, and every tool needs a description
// written for a model plus an input schema it can fill in.
func TestToolsAreDiscoverable(t *testing.T) {
	c, _ := newDemoClient(t)
	cs := connect(t, c)

	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := make(map[string]*mcpsdk.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}

	want := []string{
		"ctx_resources_list", "ctx_resolve", "ctx_history",
		"ctx_run_start", "ctx_run_mount", "ctx_run_commit",
		"ctx_manifest", "ctx_diff", "ctx_replay",
		"ctx_tag_set", "ctx_tag_list", "ctx_tag_delete",
		"ctx_evidence_export",
	}
	for _, name := range want {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("tool %s is not advertised", name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %s has no description", name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %s has no input schema", name)
		}
	}
	if len(byName) != len(want) {
		t.Fatalf("advertised %d tools, expected exactly %d: %v", len(byName), len(want), byName)
	}
}
