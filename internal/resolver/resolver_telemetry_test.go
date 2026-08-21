package resolver_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"readproof/internal/materialization"
	"readproof/internal/policy"
	"readproof/internal/resolver"
	"readproof/internal/resource"
	"readproof/internal/source"
	fsSource "readproof/internal/source/filesystem"
	"readproof/internal/storage/blob"
	"readproof/internal/storage/sqlite"
	"readproof/internal/tag"
	"readproof/internal/telemetry"
)

// fixtureContent is the demo's refund policy. Tests below assert these
// bytes never reach a span — see TestSpansNeverCarryContent.
const fixtureContent = "Products can be refunded within 30 days.\n"

// recordSpans installs an in-memory span exporter for the duration of the
// test and returns a flush-and-collect function. Init is not usable here:
// it needs an OTLP endpoint, which would export off-box from a unit test.
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

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, s.Name)
	}
	return names
}

func hasSpan(spans tracetest.SpanStubs, name string) bool {
	for _, s := range spans {
		if s.Name == name {
			return true
		}
	}
	return false
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

// newFixtureResolver registers one filesystem-backed resource holding
// fixtureContent and returns the resolver, its URI and the fixture path.
func newFixtureResolver(t *testing.T, strategy policy.Strategy) (*resolver.Resolver, string, string) {
	t.Helper()
	dir := t.TempDir()

	db, err := sqlite.Open(filepath.Join(dir, "readproof.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sources := source.NewRegistry()
	sources.Register(source.KindFilesystem, fsSource.New())
	res := &resolver.Resolver{
		Resources:        sqlite.NewResourceStore(db),
		Snapshots:        sqlite.NewSnapshotStore(db),
		Materializations: sqlite.NewMaterializationStore(db),
		Tags:             sqlite.NewTagStore(db),
		Blobs:            blob.NewLocalStore(filepath.Join(dir, "blobs")),
		Sources:          sources,
		Materializer:     materialization.RawMaterializer{},
	}

	fixturePath := filepath.Join(t.TempDir(), "refunds.md")
	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	uri := "readproof://demo/policies/refunds"
	if err := res.Resources.Create(context.Background(), resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: fixturePath},
		},
		Policy: policy.Policy{Strategy: strategy},
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	return res, uri, fixturePath
}

func TestResolveEmitsExpectedSpans(t *testing.T) {
	collect := recordSpans(t)
	res, uri, _ := newFixtureResolver(t, policy.StrategyRequireFresh)

	if _, err := res.Resolve(context.Background(), uri); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	spans := collect()

	for _, want := range []string{
		"readproof.resolve", "readproof.resource.lookup", "readproof.policy.evaluate",
		"readproof.source.fetch", "readproof.snapshot.create", "readproof.materialize",
	} {
		if !hasSpan(spans, want) {
			t.Errorf("expected a %q span, spans seen: %v", want, spanNames(spans))
		}
	}

	// The root readproof.resolve span must be the parent of every child span
	// (proving pipeline stages are correctly nested, not siblings of
	// whatever the caller's ambient span happened to be).
	root := findSpan(t, spans, "readproof.resolve")
	for _, s := range spans {
		if s.Name == "readproof.resolve" {
			continue
		}
		if s.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Errorf("span %q is not a child of the readproof.resolve root span", s.Name)
		}
	}
}

// TestResolveSpanCarriesResultAttributes pins the readproof.resolve
// attribute set an observability backend correlates on — identity of the
// bytes, never the bytes — including the GenAI semconv data-source mapping.
func TestResolveSpanCarriesResultAttributes(t *testing.T) {
	collect := recordSpans(t)
	res, uri, _ := newFixtureResolver(t, policy.StrategyRequireFresh)

	result, err := res.Resolve(context.Background(), uri)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	span := findSpan(t, collect(), "readproof.resolve")

	if result.Snapshot.SourceRevision == "" {
		t.Fatal("fixture produced an empty source revision; the assertion below would be vacuous")
	}

	wantString(t, span, "readproof.resource.uri", uri)
	wantString(t, span, "readproof.snapshot.id", result.Snapshot.SnapshotID)
	wantString(t, span, "readproof.snapshot.content_hash", result.Snapshot.ContentHash)
	wantString(t, span, "readproof.snapshot.source_revision", result.Snapshot.SourceRevision)
	wantString(t, span, "readproof.materialization.id", result.Materialization.MaterializationID)
	wantString(t, span, "readproof.source.type", string(source.KindFilesystem))
	wantString(t, span, "readproof.policy.strategy", string(policy.StrategyRequireFresh))
	wantString(t, span, "readproof.policy.decision", "fetch")
	// Kept alongside decision for the dashboards v0.1 documented.
	wantString(t, span, "readproof.freshness.status", "fetch")
	wantString(t, span, "gen_ai.data_source.id", "readproof://demo")

	a := attrs(span)
	if _, ok := a["readproof.resource.ref"]; ok {
		t.Error("a plain (untagged) resolve must not set readproof.resource.ref")
	}
	if got, want := a["readproof.materialization.bytes"].AsInt64(), result.Materialization.Bytes; got != want {
		t.Errorf("readproof.materialization.bytes = %d, want %d", got, want)
	}
	if got, want := a["readproof.materialization.bytes"].AsInt64(), int64(len(fixtureContent)); got != want {
		t.Errorf("readproof.materialization.bytes = %d, want %d (the fixture's length)", got, want)
	}
	observedAt, ok := a["readproof.snapshot.observed_at"]
	if !ok {
		t.Fatal("readproof.resolve is missing readproof.snapshot.observed_at")
	}
	parsed, err := time.Parse(time.RFC3339, observedAt.AsString())
	if err != nil {
		t.Fatalf("readproof.snapshot.observed_at %q is not RFC3339: %v", observedAt.AsString(), err)
	}
	if !parsed.Equal(result.Snapshot.ObservedAt.Truncate(time.Second)) {
		t.Errorf("readproof.snapshot.observed_at = %s, want %s", parsed, result.Snapshot.ObservedAt)
	}
}

// A tag resolve names one exact snapshot: policy is never consulted, so
// there must be no readproof.policy.evaluate span and the decision is
// use_tag.
func TestTagResolveSpanAttributes(t *testing.T) {
	res, uri, _ := newFixtureResolver(t, policy.StrategyRequireFresh)
	ctx := context.Background()

	seed, err := res.Resolve(ctx, uri)
	if err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	if err := res.Tags.Set(ctx, tag.Tag{ResourceURI: uri, Name: "prod", SnapshotID: seed.Snapshot.SnapshotID}); err != nil {
		t.Fatalf("set tag: %v", err)
	}

	collect := recordSpans(t)
	result, err := res.Resolve(ctx, uri+"@prod")
	if err != nil {
		t.Fatalf("tagged resolve: %v", err)
	}
	spans := collect()
	span := findSpan(t, spans, "readproof.resolve")

	wantString(t, span, "readproof.resource.uri", uri)
	wantString(t, span, "readproof.resource.ref", "prod")
	wantString(t, span, "readproof.policy.decision", "use_tag")
	wantString(t, span, "readproof.freshness.status", "use_tag")
	wantString(t, span, "readproof.snapshot.id", seed.Snapshot.SnapshotID)
	wantString(t, span, "readproof.snapshot.content_hash", result.Snapshot.ContentHash)
	wantString(t, span, "gen_ai.data_source.id", "readproof://demo")

	if hasSpan(spans, "readproof.policy.evaluate") {
		t.Error("a tag resolve must not evaluate policy")
	}
	if hasSpan(spans, "readproof.source.fetch") {
		t.Error("a tag resolve must never contact the source")
	}
	if !hasSpan(spans, "readproof.tag.lookup") {
		t.Errorf("expected a readproof.tag.lookup span, spans seen: %v", spanNames(spans))
	}
	tagSpan := findSpan(t, spans, "readproof.tag.lookup")
	wantString(t, tagSpan, "readproof.resource.ref", "prod")
}

func TestResolveCacheHitEmitsCacheLookupSpan(t *testing.T) {
	res, uri, _ := newFixtureResolver(t, policy.StrategyAllowStale) // no TTL -> always reuse once cached
	ctx := context.Background()

	if _, err := res.Resolve(ctx, uri); err != nil {
		t.Fatalf("resolve 1: %v", err)
	}

	collect := recordSpans(t)
	if _, err := res.Resolve(ctx, uri); err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	spans := collect()

	if !hasSpan(spans, "readproof.cache.lookup") {
		t.Error("expected a readproof.cache.lookup span on the second (cached) resolve")
	}
	if hasSpan(spans, "readproof.source.fetch") {
		t.Error("did not expect a readproof.source.fetch span on a cache hit")
	}
	wantString(t, findSpan(t, spans, "readproof.resolve"), "readproof.policy.decision", "use_current")
}

// A failed resolve must still be a legible span: the error is recorded and
// the status is Error, so a trace shows the failure rather than a stub.
func TestResolveRecordsErrorOnSpan(t *testing.T) {
	collect := recordSpans(t)
	res, _, _ := newFixtureResolver(t, policy.StrategyRequireFresh)

	if _, err := res.Resolve(context.Background(), "readproof://demo/does/not/exist"); err == nil {
		t.Fatal("expected an error resolving an unregistered uri")
	}
	span := findSpan(t, collect(), "readproof.resolve")

	if span.Status.Code.String() != "Error" {
		t.Errorf("readproof.resolve status = %s, want Error", span.Status.Code)
	}
	if len(span.Events) == 0 {
		t.Error("expected the error to be recorded as a span event")
	}
	if _, ok := attrs(span)["readproof.snapshot.id"]; ok {
		t.Error("a failed resolve must not claim a snapshot id")
	}
}

// Content is what Readproof stores, never what it exports: no span
// attribute or event may carry resolved bytes, however convenient that
// would be.
func TestSpansNeverCarryContent(t *testing.T) {
	collect := recordSpans(t)
	res, uri, _ := newFixtureResolver(t, policy.StrategyRequireFresh)

	if _, err := res.Resolve(context.Background(), uri); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertNoContent(t, collect(), fixtureContent)
}

// assertNoContent scans every attribute value and event (including event
// attributes) of every span for a fragment of the resolved bytes.
func assertNoContent(t *testing.T, spans tracetest.SpanStubs, content string) {
	t.Helper()
	needle := strings.TrimSpace(content)
	if needle == "" {
		t.Fatal("empty needle: the scan below would be vacuous")
	}
	check := func(span, where, value string) {
		if strings.Contains(value, needle) {
			t.Errorf("span %q leaked resolved content in %s: %q", span, where, value)
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
