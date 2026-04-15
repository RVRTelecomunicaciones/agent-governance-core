package metrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

// MetricsWorkflowControl is a decorator that emits OTel metrics around a
// WorkflowControl implementation.
type MetricsWorkflowControl struct {
	next inbound.WorkflowControl
	inst *Instruments
}

var _ inbound.WorkflowControl = (*MetricsWorkflowControl)(nil)

// NewMetricsWorkflowControl wraps next with metric instrumentation.
func NewMetricsWorkflowControl(next inbound.WorkflowControl, inst *Instruments) *MetricsWorkflowControl {
	return &MetricsWorkflowControl{next: next, inst: inst}
}

// RegisterAttempt delegates to the next service and records execution.attempts on success.
// Strategy and agent_role labels are included only when present in the AttemptResult.
func (m *MetricsWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	run, err := m.next.RegisterAttempt(ctx, id, result)
	if err != nil {
		return nil, err
	}

	attrs := make([]attribute.KeyValue, 0, 2)
	if result.StrategyUsed != nil {
		attrs = append(attrs, attribute.String("strategy", *result.StrategyUsed))
	}
	if result.AgentRole != nil {
		attrs = append(attrs, attribute.String("agent_role", *result.AgentRole))
	}

	m.inst.ExecutionAttempts.Record(ctx, 1, metric.WithAttributes(attrs...))

	return run, nil
}

// KillWorkflow delegates to the next service and increments workflow.transitions on success.
func (m *MetricsWorkflowControl) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	err := m.next.KillWorkflow(ctx, id, reason, actor)
	if err != nil {
		return err
	}

	m.inst.WorkflowTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("transition", "killed"),
	))

	return nil
}

// PauseWorkflow delegates to the next service and increments workflow.transitions on success.
func (m *MetricsWorkflowControl) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	err := m.next.PauseWorkflow(ctx, id, reason, actor)
	if err != nil {
		return err
	}

	m.inst.WorkflowTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("transition", "paused"),
	))

	return nil
}

// ResumeWorkflow delegates to the next service and increments workflow.transitions on success.
func (m *MetricsWorkflowControl) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	err := m.next.ResumeWorkflow(ctx, id, reason, actor)
	if err != nil {
		return err
	}

	m.inst.WorkflowTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("transition", "resumed"),
	))

	return nil
}
