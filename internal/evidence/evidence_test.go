package evidence

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/app"
	"github.com/fbzz/readproof/internal/client"
	"github.com/fbzz/readproof/internal/client/local"
	"github.com/fbzz/readproof/internal/ids"
	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/redact"
	"github.com/fbzz/readproof/internal/replay"
	"github.com/fbzz/readproof/internal/resource"
	"github.com/fbzz/readproof/internal/source"
)

const refundsContent = "Products can be refunded within 30 days.\n"
const shippingContent = "Orders ship within 2 business days.\n"

var fixedNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// newDemoRun stands up an embedded app the same way the refund-agent demo
// does (see internal/e2e/demo_test.go), mounts two filesystem-backed
// resources into one run, and returns a client over it plus the run id.
func newDemoRun(t *testing.T) (client.Client, string) {
	t.Helper()

	fixtureDir := t.TempDir()
	refunds := filepath.Join(fixtureDir, "refunds.md")
	shipping := filepath.Join(fixtureDir, "shipping.md")
	if err := os.WriteFile(refunds, []byte(refundsContent), 0o644); err != nil {
		t.Fatalf("write refunds fixture: %v", err)
	}
	if err := os.WriteFile(shipping, []byte(shippingContent), 0o644); err != nil {
		t.Fatalf("write shipping fixture: %v", err)
	}

	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	c := local.New(a)
	t.Cleanup(func() { c.Close() })

	ctx := context.Background()
	uris := []string{"readproof://demo/policies/refunds", "readproof://demo/policies/shipping"}
	paths := []string{refunds, shipping}
	for i, uri := range uris {
		parsed, err := resource.ParseURI(uri)
		if err != nil {
			t.Fatalf("parse uri: %v", err)
		}
		err = c.RegisterResource(ctx, resource.Resource{
			URI:       uri,
			Namespace: parsed.Namespace,
			Path:      parsed.Path,
			SourceConfig: source.Config{
				Kind:       source.KindFilesystem,
				Filesystem: &source.FilesystemConfig{Path: paths[i]},
			},
			Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
		})
		if err != nil {
			t.Fatalf("register %s: %v", uri, err)
		}
	}

	const runID = "run-evidence"
	if err := c.RunStart(ctx, runID); err != nil {
		t.Fatalf("run start: %v", err)
	}
	for _, uri := range uris {
		if _, _, err := c.RunMount(ctx, runID, uri); err != nil {
			t.Fatalf("mount %s: %v", uri, err)
		}
	}
	if _, err := c.RunCommit(ctx, runID); err != nil {
		t.Fatalf("run commit: %v", err)
	}
	return c, runID
}

func buildBundle(t *testing.T, c client.Client, target string, opts Options) Bundle {
	t.Helper()
	if opts.Now == nil {
		opts.Now = func() time.Time { return fixedNow }
	}
	b, err := Build(context.Background(), c, target, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return b
}

func TestBuildProducesInTotoStatementForARun(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{WithContent: true})

	if b.Type != StatementType {
		t.Errorf("_type = %q, want %q", b.Type, StatementType)
	}
	if b.PredicateType != PredicateType {
		t.Errorf("predicateType = %q, want %q", b.PredicateType, PredicateType)
	}
	if len(b.Subject) != 1 {
		t.Fatalf("expected 1 subject, got %d", len(b.Subject))
	}
	if b.Subject[0].Name != b.Predicate.ManifestID {
		t.Errorf("subject name %q != manifest id %q", b.Subject[0].Name, b.Predicate.ManifestID)
	}
	if b.Subject[0].Digest.SHA256 != MerkleRoot(b.Predicate.Entries) {
		t.Errorf("subject digest is not the merkle root over the entries")
	}
	if b.Predicate.RunID != runID {
		t.Errorf("run_id = %q, want %q", b.Predicate.RunID, runID)
	}
	if !b.Predicate.GeneratedAt.Equal(fixedNow) {
		t.Errorf("generated_at = %s, want %s", b.Predicate.GeneratedAt, fixedNow)
	}
	if b.Predicate.Exporter != (Exporter{Name: ExporterName, Version: ExporterVersion}) {
		t.Errorf("exporter = %+v", b.Predicate.Exporter)
	}
	if b.Predicate.Merkle.Algorithm != MerkleAlgorithm || b.Predicate.Merkle.Leaf != MerkleLeafFormula {
		t.Errorf("merkle metadata = %+v", b.Predicate.Merkle)
	}

	if len(b.Predicate.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(b.Predicate.Entries))
	}
	first := b.Predicate.Entries[0]
	if first.Position != 0 || first.URI != "readproof://demo/policies/refunds" {
		t.Errorf("entry 0 = %+v", first)
	}
	// Entries must be hydrated from the snapshot, not just copied from the
	// manifest — the snapshot metadata is what makes the bundle auditable.
	if first.SnapshotID == "" || first.MaterializationID == "" || first.SourceRevision == "" {
		t.Errorf("entry 0 is not hydrated from its snapshot: %+v", first)
	}
	if first.ObservedAt.IsZero() || first.Bytes != int64(len(refundsContent)) {
		t.Errorf("entry 0 snapshot metadata = observed_at %s bytes %d", first.ObservedAt, first.Bytes)
	}
	if first.ContentHash != ids.ContentHash([]byte(refundsContent)) {
		t.Errorf("entry 0 content_hash = %s", first.ContentHash)
	}
	if first.Provenance == nil {
		t.Error("entry 0 provenance should encode as {} rather than null")
	}
	if got, err := base64.StdEncoding.DecodeString(first.ContentB64); err != nil || string(got) != refundsContent {
		t.Errorf("entry 0 content_b64 = %q (err %v), want %q", got, err, refundsContent)
	}

	if len(b.Predicate.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(b.Predicate.Resources))
	}
	res := b.Predicate.Resources[0]
	if res.URI != "readproof://demo/policies/refunds" || res.Namespace != "demo" || res.Path != "policies/refunds" {
		t.Errorf("resource 0 = %+v", res)
	}
	if res.Source.Kind != string(source.KindFilesystem) || res.Source.Config.Filesystem == nil {
		t.Errorf("resource 0 source = %+v", res.Source)
	}
	if res.Policy.Strategy != string(policy.StrategyRequireFresh) {
		t.Errorf("resource 0 policy = %+v", res.Policy)
	}
	if res.Missing {
		t.Error("resource 0 should not be marked missing")
	}

	if !b.Predicate.Replay.AllMatch || len(b.Predicate.Replay.Entries) != 2 {
		t.Fatalf("replay = %+v", b.Predicate.Replay)
	}
	if !b.Predicate.Replay.VerifiedAt.Equal(fixedNow) {
		t.Errorf("replay.verified_at = %s, want %s", b.Predicate.Replay.VerifiedAt, fixedNow)
	}
	for _, e := range b.Predicate.Replay.Entries {
		if !e.Match || e.ExpectedHash != e.ActualHash || e.ExpectedHash == "" {
			t.Errorf("replay entry %d = %+v", e.Position, e)
		}
	}
}

func TestBuildOmitsContentByDefault(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{})

	for _, e := range b.Predicate.Entries {
		if e.ContentB64 != "" {
			t.Fatalf("entry %d embedded content without WithContent", e.Position)
		}
	}
	data, err := Encode(b)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(data), "content_b64") {
		t.Error("metadata-only bundle should not contain a content_b64 key at all")
	}
	if strings.Contains(string(data), refundsContent[:20]) {
		t.Error("metadata-only bundle leaked resource content")
	}
}

func TestBuildByManifestIDMatchesBuildByRunID(t *testing.T) {
	c, runID := newDemoRun(t)
	byRun := buildBundle(t, c, runID, Options{WithContent: true})
	byManifest := buildBundle(t, c, byRun.Predicate.ManifestID, Options{WithContent: true})

	if byRun.Subject[0].Digest.SHA256 != byManifest.Subject[0].Digest.SHA256 {
		t.Fatalf("root differs by target: %s vs %s", byRun.Subject[0].Digest.SHA256, byManifest.Subject[0].Digest.SHA256)
	}

	// Byte-for-byte stability matters: the bundle is what gets signed.
	a, err := Encode(byRun)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	bb, err := Encode(byManifest)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(a) != string(bb) {
		t.Error("bundles built from the run id and the manifest id are not byte-identical")
	}
}

func TestVerifyRoundTripOfflineAndAgainstStore(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{WithContent: true})

	data, err := Encode(b)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	offline, err := Verify(decoded, VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify offline: %v", err)
	}
	if !offline.OK {
		t.Fatalf("offline verification failed: %+v", failed(offline))
	}
	if offline.ContentChecked != 2 || offline.ReplayChecked {
		t.Errorf("offline report = %+v", offline)
	}
	if offline.MerkleRoot != b.Subject[0].Digest.SHA256 {
		t.Errorf("report root = %s, want %s", offline.MerkleRoot, b.Subject[0].Digest.SHA256)
	}

	online, err := Verify(decoded, VerifyOptions{Client: c})
	if err != nil {
		t.Fatalf("Verify online: %v", err)
	}
	if !online.OK {
		t.Fatalf("store verification failed: %+v", failed(online))
	}
	if !online.ReplayChecked || online.ReplayMatched != 2 || online.ReplayTotal != 2 {
		t.Errorf("online report = %+v", online)
	}
}

func TestVerifyDetectsTamperedEmbeddedContent(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{WithContent: true})

	// Flip one byte of the delivered bytes, leaving every hash alone.
	raw, err := base64.StdEncoding.DecodeString(b.Predicate.Entries[0].ContentB64)
	if err != nil {
		t.Fatalf("decode content: %v", err)
	}
	raw[0] ^= 0x01
	b.Predicate.Entries[0].ContentB64 = base64.StdEncoding.EncodeToString(raw)

	report, err := Verify(b, VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.OK {
		t.Fatal("verification passed on tampered content")
	}
	if !checkFailed(report, "content[0]") {
		t.Fatalf("expected content[0] to fail, got %+v", failed(report))
	}
	// The root only commits to hashes, so it must still verify — proving
	// the two checks are independent.
	if checkFailed(report, "merkle_root") {
		t.Error("merkle root should be unaffected by a content-only tamper")
	}
}

func TestVerifyDetectsTamperedEntryHash(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{WithContent: true})

	b.Predicate.Entries[1].ContentHash = ids.ContentHash([]byte("a policy that was never delivered"))

	report, err := Verify(b, VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.OK {
		t.Fatal("verification passed on a tampered entry hash")
	}
	if !checkFailed(report, "merkle_root") || !checkFailed(report, "predicate_merkle_root") {
		t.Fatalf("expected both merkle checks to fail, got %+v", failed(report))
	}
	if !checkFailed(report, "content[1]") {
		t.Fatalf("expected the embedded content check to fail too, got %+v", failed(report))
	}
}

// A forger who edits an entry AND recomputes the root produces a bundle
// that is internally consistent — only the store cross-check catches it.
func TestVerifyDetectsInternallyConsistentForgery(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{})

	forged := []byte("Products can be refunded within 365 days.\n")
	b.Predicate.Entries[0].ContentHash = ids.ContentHash(forged)
	root := MerkleRoot(b.Predicate.Entries)
	b.Predicate.Merkle.Root = root
	b.Subject[0].Digest.SHA256 = root

	offline, err := Verify(b, VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify offline: %v", err)
	}
	if !offline.OK {
		t.Fatalf("a re-rooted forgery should still pass offline checks: %+v", failed(offline))
	}

	online, err := Verify(b, VerifyOptions{Client: c})
	if err != nil {
		t.Fatalf("Verify online: %v", err)
	}
	if online.OK {
		t.Fatal("the store cross-check did not catch a re-rooted forgery")
	}
	if !checkFailed(online, "store_replay[0]") {
		t.Fatalf("expected store_replay[0] to fail, got %+v", failed(online))
	}
}

func TestVerifyRejectsForeignStatementAndPredicateTypes(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, c, runID, Options{})
	b.Type = "https://example.com/Statement/v0"
	b.PredicateType = "urn:someone-else:evidence:v1"

	report, err := Verify(b, VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.OK || !checkFailed(report, "statement_type") || !checkFailed(report, "predicate_type") {
		t.Fatalf("expected type checks to fail, got %+v", failed(report))
	}
}

func TestBuildRedactsHTTPSourceHeaders(t *testing.T) {
	const secret = "Bearer super-secret-token-value"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != secret {
			t.Errorf("source adapter did not send the configured header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("remote policy body\n"))
	}))
	defer server.Close()

	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	c := local.New(a)
	defer c.Close()

	ctx := context.Background()
	const uri = "readproof://demo/policies/remote"
	err = c.RegisterResource(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/remote",
		SourceConfig: source.Config{
			Kind: source.KindHTTP,
			HTTP: &source.HTTPConfig{
				URL:     server.URL,
				Headers: map[string]string{"Authorization": secret, "X-Trace-Id": "trace-123"},
			},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	})
	if err != nil {
		t.Fatalf("register resource: %v", err)
	}

	const runID = "run-http"
	if err := c.RunStart(ctx, runID); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, _, err := c.RunMount(ctx, runID, uri); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if _, err := c.RunCommit(ctx, runID); err != nil {
		t.Fatalf("commit: %v", err)
	}

	b := buildBundle(t, c, runID, Options{WithContent: true})
	if len(b.Predicate.Resources) != 1 || b.Predicate.Resources[0].Source.Config.HTTP == nil {
		t.Fatalf("expected one http resource, got %+v", b.Predicate.Resources)
	}
	headers := b.Predicate.Resources[0].Source.Config.HTTP.Headers
	if headers["Authorization"] != redact.Placeholder {
		t.Errorf("Authorization header = %q, want %q", headers["Authorization"], redact.Placeholder)
	}
	if headers["X-Trace-Id"] != "trace-123" {
		t.Errorf("non-sensitive header was altered: %q", headers["X-Trace-Id"])
	}

	// The strongest form of the assertion: the secret must not appear
	// anywhere in the serialized artifact, in any field.
	data, err := Encode(b)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(data), "super-secret-token-value") {
		t.Fatal("the encoded bundle leaked a credential")
	}
}

// missingResourceClient simulates a resource that was deregistered after
// the run committed: the manifest still resolves, the resource does not.
type missingResourceClient struct{ client.Client }

func (c missingResourceClient) GetResource(context.Context, string) (resource.Resource, error) {
	return resource.Resource{}, resource.ErrNotFound
}

func TestBuildRecordsDeletedResourceAsMissing(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, missingResourceClient{c}, runID, Options{})

	if len(b.Predicate.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(b.Predicate.Resources))
	}
	for _, res := range b.Predicate.Resources {
		if !res.Missing {
			t.Errorf("resource %s should be marked missing", res.URI)
		}
		if res.Namespace != "demo" {
			t.Errorf("missing resource %s lost the identity carried by its URI: %+v", res.URI, res)
		}
	}
	// A missing definition must not affect the digest — the entries, not
	// the resource definitions, are what the root commits to.
	if b.Subject[0].Digest.SHA256 != MerkleRoot(b.Predicate.Entries) {
		t.Error("root changed when a resource was missing")
	}
}

// replayBrokenClient simulates a store whose blobs are gone.
type replayBrokenClient struct{ client.Client }

func (c replayBrokenClient) Replay(context.Context, string) (replay.Result, error) {
	return replay.Result{}, errors.New("replay: load blob sha256:deadbeef: blob: not found")
}

func TestBuildRecordsReplayFailureInsteadOfFailing(t *testing.T) {
	c, runID := newDemoRun(t)
	b := buildBundle(t, replayBrokenClient{c}, runID, Options{WithContent: true})

	if b.Predicate.Replay.AllMatch {
		t.Error("all_match should be false when replay could not run")
	}
	if !strings.Contains(b.Predicate.Replay.Error, "blob: not found") {
		t.Errorf("replay.error = %q, want the underlying failure", b.Predicate.Replay.Error)
	}
	if len(b.Predicate.Entries) != 2 {
		t.Fatalf("entries should still be exported from the manifest, got %d", len(b.Predicate.Entries))
	}
	for _, e := range b.Predicate.Entries {
		if e.ContentB64 != "" {
			t.Errorf("entry %d has content although replay failed", e.Position)
		}
	}
}

func TestIsNotFoundAcrossClientErrorShapes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"typed sentinel from the local client", resource.ErrNotFound, true},
		{"wrapped sentinel", errors.New("x: " + resource.ErrNotFound.Error()), true},
		// The remote client flattens readproofd's 404 body into a plain error.
		{"flattened remote 404", errors.New("readproofd: resource: not found: readproof://demo/x"), true},
		{"unrelated failure", errors.New("readproofd: request failed: connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFound(tt.err); got != tt.want {
				t.Fatalf("isNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDecodeToleratesUnknownFields(t *testing.T) {
	const forwardCompatible = `{
	  "_type": "https://in-toto.io/Statement/v1",
	  "subject": [{"name": "man_1", "digest": {"sha256": "abc"}, "annotations": {"x": 1}}],
	  "predicateType": "urn:readproof:evidence:v0.3",
	  "predicate": {"manifest_id": "man_1", "entries": [], "future_field": {"nested": true}}
	}`
	b, err := Decode([]byte(forwardCompatible))
	if err != nil {
		t.Fatalf("Decode rejected a forward-compatible bundle: %v", err)
	}
	if b.Predicate.ManifestID != "man_1" || b.Subject[0].Digest.SHA256 != "abc" {
		t.Fatalf("decoded bundle = %+v", b)
	}
}

func failed(r Report) []Check {
	var out []Check
	for _, c := range r.Checks {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

func checkFailed(r Report, name string) bool {
	for _, c := range r.Checks {
		if c.Name == name && !c.OK {
			return true
		}
	}
	return false
}
