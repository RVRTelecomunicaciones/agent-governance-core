package workflowrun

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// LifecycleMetrics holds the metric instruments needed for lifecycle emission.
// nil means no metrics are emitted (backwards compatible with Phase 1).
type LifecycleMetrics struct {
	TasksCompleted    metric.Int64Counter
	WorkflowDuration  metric.Float64Histogram
	ExecutionFailures metric.Int64Counter
}

// emitTerminal emits tasks.completed + workflow.duration_ms for a workflow that reached terminal state.
func (m *LifecycleMetrics) emitTerminal(ctx context.Context, finalStatus string, durationMs float64) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("final_status", finalStatus))
	m.TasksCompleted.Add(ctx, 1, attrs)
	m.WorkflowDuration.Record(ctx, durationMs, attrs)
}

// emitFailure emits execution.failures counter.
func (m *LifecycleMetrics) emitFailure(ctx context.Context, stage, category string, retryable bool) {
	if m == nil {
		return
	}
	m.ExecutionFailures.Add(ctx, 1, metric.WithAttributes(
		attribute.String("failure_stage", stage),
		attribute.String("failure_category", category),
		attribute.Bool("retryable", retryable),
	))
}

// extractCategory extracts the category prefix from a failure code (e.g. "tool/timeout" → "tool").
func extractCategory(code string) string {
	if i := strings.Index(code, "/"); i >= 0 {
		return code[:i]
	}
	return code
}
