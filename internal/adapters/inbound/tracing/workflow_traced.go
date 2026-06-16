package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
)

var _ inbound.WorkflowControl = (*TracedWorkflowControl)(nil)

// TracedWorkflowControl wraps a WorkflowControl with OpenTelemetry tracing.
type TracedWorkflowControl struct {
	next   inbound.WorkflowControl
	tracer trace.Tracer
}

// NewTracedWorkflowControl creates a new tracing decorator for WorkflowControl.
func NewTracedWorkflowControl(next inbound.WorkflowControl, tracer trace.Tracer) *TracedWorkflowControl {
	return &TracedWorkflowControl{next: next, tracer: tracer}
}

func (t *TracedWorkflowControl) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	ctx, span := t.tracer.Start(ctx, "WorkflowControl.KillWorkflow",
		trace.WithAttributes(
			attribute.String("governance.action", "KillWorkflow"),
			attribute.String("governance.workflow_run_id", string(id)),
			attribute.String("governance.kill_reason", reason),
		),
	)
	defer span.End()

	err := t.next.KillWorkflow(ctx, id, reason, actor)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.String("governance.outcome", "killed"))

	return nil
}

func (t *TracedWorkflowControl) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	ctx, span := t.tracer.Start(ctx, "WorkflowControl.PauseWorkflow",
		trace.WithAttributes(
			attribute.String("governance.action", "PauseWorkflow"),
			attribute.String("governance.workflow_run_id", string(id)),
		),
	)
	defer span.End()

	err := t.next.PauseWorkflow(ctx, id, reason, actor)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return nil
}

func (t *TracedWorkflowControl) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	ctx, span := t.tracer.Start(ctx, "WorkflowControl.ResumeWorkflow",
		trace.WithAttributes(
			attribute.String("governance.action", "ResumeWorkflow"),
			attribute.String("governance.workflow_run_id", string(id)),
		),
	)
	defer span.End()

	err := t.next.ResumeWorkflow(ctx, id, reason, actor)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return nil
}

func (t *TracedWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	attrs := []attribute.KeyValue{
		attribute.String("governance.action", "RegisterAttempt"),
		attribute.String("governance.workflow_run_id", string(id)),
		attribute.String("governance.attempt_status", string(result.Status)),
	}

	ctx, span := t.tracer.Start(ctx, "WorkflowControl.RegisterAttempt",
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	run, err := t.next.RegisterAttempt(ctx, id, result)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", string(run.Status)))

	if result.FailureStage != nil {
		span.SetAttributes(attribute.String("governance.failure_stage", string(*result.FailureStage)))
	}
	if result.FailureCode != nil && *result.FailureCode != "" {
		span.SetAttributes(attribute.String("governance.failure_category", extractCategory(*result.FailureCode)))
	}

	return run, nil
}
