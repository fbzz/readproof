package resolver_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"ctx/internal/materialization"
	"ctx/internal/policy"
	"ctx/internal/resolver"
	"ctx/internal/resource"
	"ctx/internal/source"
	fsSource "ctx/internal/source/filesystem"
	"ctx/internal/storage/blob"
	"ctx/internal/storage/sqlite"
	"ctx/internal/telemetry"
)

func TestResolveEmitsExpectedSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	original := telemetry.Tracer
	telemetry.Tracer = tp.Tracer("test")
	t.Cleanup(func() { telemetry.Tracer = original })

	res, _ := newTestResolver(t)
	ctx := context.Background()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "refunds.md")
	if err := os.WriteFile(filePath, []byte("Products can be refunded within 30 days.\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	uri := "ctx://demo/policies/refunds"
	if err := res.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: filePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyRequireFresh},
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if _, err := res.Resolve(ctx, uri); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := tp.ForceFlush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	spans := exporter.GetSpans()
	names := make(map[string]int)
	for _, s := range spans {
		names[s.Name]++
	}

	for _, want := range []string{
		"ctx.resolve", "ctx.resource.lookup", "ctx.policy.evaluate",
		"ctx.source.fetch", "ctx.snapshot.create", "ctx.materialize",
	} {
		if names[want] == 0 {
			t.Errorf("expected a %q span, spans seen: %v", want, names)
		}
	}

	// The root ctx.resolve span must be the parent of every child span
	// (proving pipeline stages are correctly nested, not siblings of
	// whatever the caller's ambient span happened to be).
	var root tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "ctx.resolve" {
			root = s
		}
	}
	if root.Name == "" {
		t.Fatalf("no ctx.resolve root span found")
	}
	for _, s := range spans {
		if s.Name == "ctx.resolve" {
			continue
		}
		if s.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Errorf("span %q is not a child of the ctx.resolve root span", s.Name)
		}
	}
}

func TestResolveCacheHitEmitsCacheLookupSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	original := telemetry.Tracer
	telemetry.Tracer = tp.Tracer("test")
	t.Cleanup(func() { telemetry.Tracer = original })

	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "ctx.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	blobStore := blob.NewLocalStore(filepath.Join(dir, "blobs"))
	sources := source.NewRegistry()
	sources.Register(source.KindFilesystem, fsSource.New())
	res := &resolver.Resolver{
		Resources:        sqlite.NewResourceStore(db),
		Snapshots:        sqlite.NewSnapshotStore(db),
		Materializations: sqlite.NewMaterializationStore(db),
		Blobs:            blobStore,
		Sources:          sources,
		Materializer:     materialization.RawMaterializer{},
	}

	fixtureDir := t.TempDir()
	filePath := filepath.Join(fixtureDir, "refunds.md")
	if err := os.WriteFile(filePath, []byte("Products can be refunded within 30 days.\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ctx := context.Background()
	uri := "ctx://demo/policies/refunds"
	if err := res.Resources.Create(ctx, resource.Resource{
		URI:       uri,
		Namespace: "demo",
		Path:      "policies/refunds",
		SourceConfig: source.Config{
			Kind:       source.KindFilesystem,
			Filesystem: &source.FilesystemConfig{Path: filePath},
		},
		Policy: policy.Policy{Strategy: policy.StrategyAllowStale, MaxAge: 0}, // no TTL -> always reuse once cached
	}); err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if _, err := res.Resolve(ctx, uri); err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	exporter.Reset()

	if _, err := res.Resolve(ctx, uri); err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if err := tp.ForceFlush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	spans := exporter.GetSpans()
	found := false
	for _, s := range spans {
		if s.Name == "ctx.cache.lookup" {
			found = true
		}
		if s.Name == "ctx.source.fetch" {
			t.Errorf("did not expect a ctx.source.fetch span on a cache hit")
		}
	}
	if !found {
		t.Errorf("expected a ctx.cache.lookup span on the second (cached) resolve")
	}
}
