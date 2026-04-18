// Package observability provides OpenTelemetry initialization for tracing and metrics.
package observability

import (
	"context"
	"errors"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// samplerFromEnv reads OTEL_TRACES_SAMPLER and OTEL_TRACES_SAMPLER_ARG and
// constructs the corresponding sdktrace.Sampler. Returns nil when the env var
// is unset, empty, or unrecognised — the caller then omits WithSampler so the
// SDK default (ParentBased(AlwaysOn)) is preserved.
func samplerFromEnv() sdktrace.Sampler {
	name := os.Getenv("OTEL_TRACES_SAMPLER")
	if name == "" {
		return nil
	}

	ratioArg := func() float64 {
		raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG")
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 1.0
		}
		return v
	}

	switch name {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratioArg())
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratioArg()))
	default:
		return nil
	}
}

// SetupOTel initializes OpenTelemetry tracing and metrics.
// If enabled=false, returns a no-op shutdown — zero impact.
// If enabled=true, configures TracerProvider + MeterProvider with OTLP exporters
// using standard OTel env vars (OTEL_EXPORTER_OTLP_ENDPOINT, etc.).
func SetupOTel(ctx context.Context, enabled bool, serviceName string) (shutdown func(context.Context) error, err error) {
	if !enabled {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, err
	}

	// Trace exporter + provider.
	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	}
	if s := samplerFromEnv(); s != nil {
		tpOpts = append(tpOpts, sdktrace.WithSampler(s))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Metric exporter + provider.
	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
