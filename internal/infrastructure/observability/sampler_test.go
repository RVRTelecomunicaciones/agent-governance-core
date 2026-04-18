package observability

import (
	"strings"
	"testing"
)

func TestSamplerFromEnv(t *testing.T) {
	t.Run("unset returns nil", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "")
		t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
		if samplerFromEnv() != nil {
			t.Fatal("expected nil for unset OTEL_TRACES_SAMPLER")
		}
	})

	t.Run("unrecognized returns nil", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "custom_sampler")
		if samplerFromEnv() != nil {
			t.Fatal("expected nil for unrecognized sampler name")
		}
	})

	t.Run("always_on returns AlwaysSample", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
		s := samplerFromEnv()
		if s == nil {
			t.Fatal("expected non-nil sampler")
		}
		if !strings.Contains(s.Description(), "AlwaysOnSampler") {
			t.Fatalf("unexpected description: %s", s.Description())
		}
	})

	t.Run("always_off returns NeverSample", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
		s := samplerFromEnv()
		if s == nil {
			t.Fatal("expected non-nil sampler")
		}
		if !strings.Contains(s.Description(), "AlwaysOffSampler") {
			t.Fatalf("unexpected description: %s", s.Description())
		}
	})

	t.Run("traceidratio with valid arg", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
		t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
		s := samplerFromEnv()
		if s == nil {
			t.Fatal("expected non-nil sampler")
		}
		if !strings.Contains(s.Description(), "TraceIDRatioBased") {
			t.Fatalf("unexpected description: %s", s.Description())
		}
	})

	t.Run("traceidratio with invalid arg defaults to 1.0", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
		t.Setenv("OTEL_TRACES_SAMPLER_ARG", "abc")
		s := samplerFromEnv()
		if s == nil {
			t.Fatal("expected non-nil sampler (not nil) even with invalid arg")
		}
		if !strings.Contains(s.Description(), "TraceIDRatioBased") {
			t.Fatalf("unexpected description: %s", s.Description())
		}
	})

	t.Run("parentbased_traceidratio with arg 0.1", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
		t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.1")
		s := samplerFromEnv()
		if s == nil {
			t.Fatal("expected non-nil sampler")
		}
		if !strings.Contains(s.Description(), "ParentBased") {
			t.Fatalf("unexpected description: %s", s.Description())
		}
	})
}
