package observability_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/russellcxl/agent-governance-core/internal/infrastructure/observability"
)

func TestSetupOTel_Disabled(t *testing.T) {
	ctx := context.Background()

	shutdown, err := observability.SetupOTel(ctx, false, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function, got nil")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("expected no error from shutdown, got %v", err)
	}

	// Global tracer should produce no-op (invalid) spans.
	tracer := otel.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	defer span.End()

	if span.SpanContext().IsValid() {
		t.Fatal("expected invalid (no-op) span when OTel is disabled")
	}
}

func TestSetupOTel_Enabled(t *testing.T) {
	ctx := context.Background()

	shutdown, err := observability.SetupOTel(ctx, true, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function, got nil")
	}

	t.Cleanup(func() {
		_ = shutdown(context.Background()) //nolint:errcheck // shutdown errors in test cleanup are not actionable
		otel.SetTracerProvider(noop.NewTracerProvider())
		otel.SetMeterProvider(otel.GetMeterProvider()) // reset handled by shutdown
	})

	// Global tracer should produce valid spans.
	tracer := otel.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	defer span.End()

	if !span.SpanContext().IsValid() {
		t.Fatal("expected valid span when OTel is enabled")
	}
}
