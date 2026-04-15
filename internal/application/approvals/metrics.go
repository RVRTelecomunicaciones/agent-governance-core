package approvals

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// LifecycleMetrics for approval-related lifecycle events.
type LifecycleMetrics struct {
	ApprovalWait     metric.Float64Histogram
	TasksCompleted   metric.Int64Counter
	WorkflowDuration metric.Float64Histogram
}

func (m *LifecycleMetrics) emitApprovalWait(ctx context.Context, waitMs float64, resolution string) {
	if m == nil {
		return
	}
	m.ApprovalWait.Record(ctx, waitMs, metric.WithAttributes(
		attribute.String("resolution", resolution),
	))
}

func (m *LifecycleMetrics) emitTerminal(ctx context.Context, finalStatus string, durationMs float64) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("final_status", finalStatus))
	m.TasksCompleted.Add(ctx, 1, attrs)
	m.WorkflowDuration.Record(ctx, durationMs, attrs)
}
