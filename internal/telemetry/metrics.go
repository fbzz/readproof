package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meter delegates to whatever MeterProvider is current at call time (see
// telemetry.go's package doc), so instruments created here at package-init
// time still export correctly once Init installs a real provider.
var meter = otel.Meter("ctx")

func mustCounter(name, description string) metric.Int64Counter {
	c, err := meter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(err)
	}
	return c
}

func mustDurationHistogram(name, description string) metric.Float64Histogram {
	h, err := meter.Float64Histogram(name, metric.WithDescription(description), metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
	return h
}

var (
	resolveTotal                = mustCounter("ctx_resolve_total", "Total resolve calls")
	resolveDuration             = mustDurationHistogram("ctx_resolve_duration_seconds", "Resolve call latency")
	resolveErrorsTotal          = mustCounter("ctx_resolve_errors_total", "Resolve calls that returned an error")
	cacheHitTotal               = mustCounter("ctx_cache_hit_total", "Resolves served from a cached snapshot")
	cacheMissTotal              = mustCounter("ctx_cache_miss_total", "Resolves that required a source fetch")
	sourceFetchTotal            = mustCounter("ctx_source_fetch_total", "Total source fetch attempts")
	sourceFetchDuration         = mustDurationHistogram("ctx_source_fetch_duration_seconds", "Source fetch latency")
	sourceFetchErrors           = mustCounter("ctx_source_fetch_errors_total", "Source fetch attempts that failed")
	snapshotCreatedTotal        = mustCounter("ctx_snapshot_created_total", "Snapshot rows created")
	materializationCreatedTotal = mustCounter("ctx_materialization_created_total", "Materialization rows created")
	manifestCreatedTotal        = mustCounter("ctx_manifest_created_total", "Manifests created")
	runCommittedTotal           = mustCounter("ctx_run_committed_total", "Runs committed to a manifest")
	tagResolveTotal             = mustCounter("ctx_tag_resolve_total", "Resolves pinned to a tag ref")
)

func RecordResolve(ctx context.Context, uri string, durationSeconds float64, err error) {
	attrs := metric.WithAttributes(attribute.String("ctx.resource.uri", uri))
	resolveTotal.Add(ctx, 1, attrs)
	resolveDuration.Record(ctx, durationSeconds, attrs)
	if err != nil {
		resolveErrorsTotal.Add(ctx, 1, attrs)
	}
}

func RecordCacheResult(ctx context.Context, hit bool) {
	if hit {
		cacheHitTotal.Add(ctx, 1)
	} else {
		cacheMissTotal.Add(ctx, 1)
	}
}

func RecordSourceFetch(ctx context.Context, sourceType string, durationSeconds float64, err error) {
	attrs := metric.WithAttributes(attribute.String("ctx.source.type", sourceType))
	sourceFetchTotal.Add(ctx, 1, attrs)
	sourceFetchDuration.Record(ctx, durationSeconds, attrs)
	if err != nil {
		sourceFetchErrors.Add(ctx, 1, attrs)
	}
}

func RecordSnapshotCreated(ctx context.Context) {
	snapshotCreatedTotal.Add(ctx, 1)
}

func RecordMaterializationCreated(ctx context.Context) {
	materializationCreatedTotal.Add(ctx, 1)
}

func RecordManifestCreated(ctx context.Context) {
	manifestCreatedTotal.Add(ctx, 1)
}

// RecordRunCommitted counts one run reaching a committed manifest. Run ids
// are caller-supplied and unbounded, so they stay off the metric and live
// only on the ctx.run.commit span, where cardinality is not a cost.
func RecordRunCommitted(ctx context.Context) {
	runCommittedTotal.Add(ctx, 1)
}

// RecordTagResolve counts one resolve served from a tag ref. The tag name
// is deliberately not a label: tags are user-named and unbounded, and the
// per-tag breakdown belongs in traces.
func RecordTagResolve(ctx context.Context) {
	tagResolveTotal.Add(ctx, 1)
}
