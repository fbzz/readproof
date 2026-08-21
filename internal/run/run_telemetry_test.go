package run_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"readproof/internal/app"
	"readproof/internal/client/local"
	"readproof/internal/evidence"
	"readproof/internal/merkle"
	"readproof/internal/policy"
	"readproof/internal/resource"
	"readproof/internal/source"
	"readproof/internal/tag"
	"readproof/internal/telemetry"
)

const (
	refundsContent  = "Products can be refunded within 30 days.\n"
	shippingContent = "Orders ship within 2 business days.\n"
	refundsURI      = "readproof://demo/policies/refunds"
	shippingURI     = "readproof://demo/policies/shipping"
)

func recordSpans(t *testing.T) func() tracetest.SpanStubs {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(telemetry.SetTracerProvider(tp))
	return func() tracetest.SpanStubs {
		if err := tp.ForceFlush(context.Background()); err != nil {
			t.Fatalf("flush spans: %v", err)
		}
		return exporter.GetSpans()
	}
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, s.Name)
	}
	return names
}

func findSpan(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, s := range spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no %q span found, spans seen: %v", name, spanNames(spans))
	return tracetest.SpanStub{}
}

func spansNamed(spans tracetest.SpanStubs, name string) tracetest.SpanStubs {
	var out tracetest.SpanStubs
	for _, s := range spans {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func attrs(s tracetest.SpanStub) map[attribute.Key]attribute.Value {
	out := make(map[attribute.Key]attribute.Value, len(s.Attributes))
	for _, kv := range s.Attributes {
		out[kv.Key] = kv.Value
	}
	return out
}

func wantString(t *testing.T, s tracetest.SpanStub, key, want string) {
	t.Helper()
	v, ok := attrs(s)[attribute.Key(key)]
	if !ok {
		t.Errorf("span %q is missing attribute %q", s.Name, key)
		return
	}
	if got := v.AsString(); got != want {
		t.Errorf("span %q attribute %q = %q, want %q", s.Name, key, got, want)
	}
}

// childrenOf returns the spans whose parent is parent, by span id.
func childrenOf(spans tracetest.SpanStubs, parent trace.SpanID) tracetest.SpanStubs {
	var out tracetest.SpanStubs
	for _, s := range spans {
		if s.Parent.SpanID() == parent {
			out = append(out, s)
		}
	}
	return out
}

func hasChild(spans tracetest.SpanStubs, parent trace.SpanID, name string) bool {
	for _, s := range childrenOf(spans, parent) {
		if s.Name == name {
			return true
		}
	}
	return false
}

// newDemoApp stands up the embedded app the refund-agent demo uses, with
// two filesystem-backed resources registered.
func newDemoApp(t *testing.T) *app.App {
	t.Helper()

	fixtureDir := t.TempDir()
	a, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	ctx := context.Background()
	for _, f := range []struct {
		uri, path, content string
	}{
		{refundsURI, filepath.Join(fixtureDir, "refunds.md"), refundsContent},
		{shippingURI, filepath.Join(fixtureDir, "shipping.md"), shippingContent},
	} {
		if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		parsed, err := resource.ParseURI(f.uri)
		if err != nil {
			t.Fatalf("parse uri: %v", err)
		}
		err = a.Resources.Create(ctx, resource.Resource{
			URI:       f.uri,
			Namespace: parsed.Namespace,
			Path:      parsed.Path,
			SourceConfig: source.Config{
				Kind:       source.KindFilesystem,
				Filesystem: &source.FilesystemConfig{Path: f.path},
			},
			Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
		})
		if err != nil {
			t.Fatalf("register %s: %v", f.uri, err)
		}
	}
	return a
}

// TestRunSpans walks the demo's own flow — start, mount by tag, mount by
// URI, commit — and pins the run-level trace shape: one readproof.run.mount
// per mounted resource, parenting that mount's readproof.resolve and
// readproof.manifest.append, and a readproof.run.commit carrying the
// manifest's identity plus the Merkle root of what was committed.
func TestRunSpans(t *testing.T) {
	a := newDemoApp(t)
	ctx := context.Background()

	// Freeze one snapshot behind a tag so the run mounts one resource by
	// ref and one plainly — both shapes must be traced the same way.
	seed, err := a.Resolver.Resolve(ctx, refundsURI)
	if err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	if err := a.Tags.Set(ctx, tag.Tag{ResourceURI: refundsURI, Name: "prod", SnapshotID: seed.Snapshot.SnapshotID}); err != nil {
		t.Fatalf("set tag: %v", err)
	}

	collect := recordSpans(t)

	const runID = "run-telemetry"
	if err := a.RunBuilder.Start(ctx, runID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := a.RunBuilder.Mount(ctx, runID, refundsURI+"@prod"); err != nil {
		t.Fatalf("mount tagged: %v", err)
	}
	if _, err := a.RunBuilder.Mount(ctx, runID, shippingURI); err != nil {
		t.Fatalf("mount plain: %v", err)
	}
	man, err := a.RunBuilder.Commit(ctx, runID)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	spans := collect()

	startSpan := findSpan(t, spans, "readproof.run.start")
	wantString(t, startSpan, "readproof.run.id", runID)

	mounts := spansNamed(spans, "readproof.run.mount")
	if len(mounts) != 2 {
		t.Fatalf("expected 2 readproof.run.mount spans, got %d (spans: %v)", len(mounts), spanNames(spans))
	}
	byURI := make(map[string]tracetest.SpanStub, len(mounts))
	for _, m := range mounts {
		byURI[attrs(m)["readproof.resource.uri"].AsString()] = m
	}

	tagged, ok := byURI[refundsURI]
	if !ok {
		t.Fatalf("no readproof.run.mount span for %s", refundsURI)
	}
	wantString(t, tagged, "readproof.run.id", runID)
	wantString(t, tagged, "readproof.resource.ref", "prod")
	if got := attrs(tagged)["readproof.manifest.position"].AsInt64(); got != 0 {
		t.Errorf("first mount readproof.manifest.position = %d, want 0", got)
	}

	plain, ok := byURI[shippingURI]
	if !ok {
		t.Fatalf("no readproof.run.mount span for %s", shippingURI)
	}
	if _, ok := attrs(plain)["readproof.resource.ref"]; ok {
		t.Error("an untagged mount must not set readproof.resource.ref")
	}
	if got := attrs(plain)["readproof.manifest.position"].AsInt64(); got != 1 {
		t.Errorf("second mount readproof.manifest.position = %d, want 1", got)
	}

	// The point of the mount span: everything a mount did hangs off it.
	for _, m := range mounts {
		uri := attrs(m)["readproof.resource.uri"].AsString()
		if !hasChild(spans, m.SpanContext.SpanID(), "readproof.resolve") {
			t.Errorf("readproof.run.mount(%s) does not parent a readproof.resolve span", uri)
		}
		if !hasChild(spans, m.SpanContext.SpanID(), "readproof.manifest.append") {
			t.Errorf("readproof.run.mount(%s) does not parent a readproof.manifest.append span", uri)
		}
	}

	commit := findSpan(t, spans, "readproof.run.commit")
	wantString(t, commit, "readproof.run.id", runID)
	wantString(t, commit, "readproof.manifest.id", man.ManifestID)
	if got, want := attrs(commit)["readproof.manifest.entries"].AsInt64(), int64(len(man.Entries)); got != want {
		t.Errorf("readproof.manifest.entries = %d, want %d", got, want)
	}
	if len(man.Entries) != 2 {
		t.Fatalf("expected 2 manifest entries, got %d", len(man.Entries))
	}

	leaves := make([]string, len(man.Entries))
	for i, e := range man.Entries {
		leaves[i] = merkle.Leaf(e.Position, e.URI, e.ContentHash)
	}
	wantRoot := merkle.Root(leaves)
	wantString(t, commit, "readproof.manifest.merkle_root", wantRoot)

	// The span's root is the same value `readproof evidence export` signs, so
	// a trace and a bundle can be joined on it.
	bundle, err := evidence.Build(ctx, local.New(a), man.ManifestID, evidence.Options{})
	if err != nil {
		t.Fatalf("build evidence bundle: %v", err)
	}
	if len(bundle.Subject) != 1 {
		t.Fatalf("expected 1 bundle subject, got %d", len(bundle.Subject))
	}
	if got := bundle.Subject[0].Digest.SHA256; got != wantRoot {
		t.Errorf("evidence subject digest %s != readproof.run.commit merkle root %s", got, wantRoot)
	}

	assertNoContent(t, spans, refundsContent, shippingContent)
}

// A commit with nothing mounted still has to produce a legible span: the
// empty-manifest root is a real, documented value, not a missing one.
func TestCommitSpanOnEmptyRun(t *testing.T) {
	a := newDemoApp(t)
	ctx := context.Background()
	collect := recordSpans(t)

	const runID = "run-empty"
	if err := a.RunBuilder.Start(ctx, runID); err != nil {
		t.Fatalf("start: %v", err)
	}
	man, err := a.RunBuilder.Commit(ctx, runID)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	commit := findSpan(t, collect(), "readproof.run.commit")
	wantString(t, commit, "readproof.manifest.id", man.ManifestID)
	if got := attrs(commit)["readproof.manifest.entries"].AsInt64(); got != 0 {
		t.Errorf("readproof.manifest.entries = %d, want 0", got)
	}
	wantString(t, commit, "readproof.manifest.merkle_root", merkle.Root(nil))
}

// assertNoContent scans every attribute value and event of every span for
// the resolved bytes. Content is what Readproof stores, never what it
// exports.
func assertNoContent(t *testing.T, spans tracetest.SpanStubs, contents ...string) {
	t.Helper()
	check := func(span, where, value string) {
		for _, c := range contents {
			needle := strings.TrimSpace(c)
			if needle == "" {
				t.Fatal("empty needle: the scan would be vacuous")
			}
			if strings.Contains(value, needle) {
				t.Errorf("span %q leaked resolved content in %s: %q", span, where, value)
			}
		}
	}
	for _, s := range spans {
		for _, kv := range s.Attributes {
			check(s.Name, "attribute "+string(kv.Key), kv.Value.Emit())
		}
		for _, e := range s.Events {
			check(s.Name, "event "+e.Name, e.Name)
			for _, kv := range e.Attributes {
				check(s.Name, "event "+e.Name+" attribute "+string(kv.Key), kv.Value.Emit())
			}
		}
		check(s.Name, "status description", s.Status.Description)
	}
}
