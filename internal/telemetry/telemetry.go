// Package telemetry wires OpenTelemetry tracing and metrics for ctx/ctxd.
//
// otel.Tracer/otel.Meter return delegating implementations: code that grabs
// a Tracer/Meter at package-init time (before Init runs) still starts
// exporting correctly once Init installs the real providers. When
// OTEL_EXPORTER_OTLP_ENDPOINT is unset, Init is a no-op and every call
// throughout the codebase is a safe no-op too — instrumentation never
// requires a collector to be running.
package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is used throughout the codebase for all ctx.* spans.
var Tracer trace.Tracer = otel.Tracer("ctx")

// Shutdown flushes and closes any configured exporters. Safe to call even
// when Init ran in no-op mode.
type Shutdown func(context.Context) error

// Init configures the global TracerProvider/MeterProvider from the
// standard OTEL_EXPORTER_OTLP_ENDPOINT (and related OTEL_EXPORTER_OTLP_*)
// environment variables, which the otlptracehttp/otlpmetrichttp exporters
// read automatically. If none is set, Init does nothing and returns a
// no-op Shutdown — every otel.Tracer/otel.Meter call elsewhere remains a
// safe no-op.
func Init(ctx context.Context, serviceName string) (Shutdown, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	Tracer = tp.Tracer("ctx")

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		if err := tp.Shutdown(ctx); err != nil {
			return err
		}
		return mp.Shutdown(ctx)
	}, nil
}
