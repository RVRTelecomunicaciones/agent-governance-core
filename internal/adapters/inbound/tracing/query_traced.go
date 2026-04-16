package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ inbound.QueryService = (*TracedQueryService)(nil)

// TracedQueryService wraps a QueryService with OpenTelemetry tracing.
type TracedQueryService struct {
	next   inbound.QueryService
	tracer trace.Tracer
}

// NewTracedQueryService creates a new tracing decorator for QueryService.
func NewTracedQueryService(next inbound.QueryService, tracer trace.Tracer) *TracedQueryService {
	return &TracedQueryService{next: next, tracer: tracer}
}

func (t *TracedQueryService) GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	ctx, span := t.tracer.Start(ctx, "QueryService.GetTask",
		trace.WithAttributes(
			attribute.String("governance.action", "GetTask"),
			attribute.String("governance.task_id", string(id)),
		),
	)
	defer span.End()

	result, err := t.next.GetTask(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, nil
}

func (t *TracedQueryService) GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	ctx, span := t.tracer.Start(ctx, "QueryService.GetWorkflowStatus",
		trace.WithAttributes(
			attribute.String("governance.action", "GetWorkflowStatus"),
			attribute.String("governance.workflow_run_id", string(id)),
		),
	)
	defer span.End()

	result, err := t.next.GetWorkflowStatus(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, nil
}

func (t *TracedQueryService) GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	ctx, span := t.tracer.Start(ctx, "QueryService.GetWorkflowByTask",
		trace.WithAttributes(
			attribute.String("governance.action", "GetWorkflowByTask"),
			attribute.String("governance.task_id", string(taskID)),
		),
	)
	defer span.End()

	result, err := t.next.GetWorkflowByTask(ctx, taskID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, nil
}

func (t *TracedQueryService) QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
	ctx, span := t.tracer.Start(ctx, "QueryService.QueryAuditTrail",
		trace.WithAttributes(
			attribute.String("governance.action", "QueryAuditTrail"),
		),
	)
	defer span.End()

	result, count, err := t.next.QueryAuditTrail(ctx, filter)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, 0, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, count, nil
}

func (t *TracedQueryService) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	ctx, span := t.tracer.Start(ctx, "QueryService.ListWorkflows",
		trace.WithAttributes(
			attribute.String("governance.action", "ListWorkflows"),
		),
	)
	defer span.End()

	result, count, err := t.next.ListWorkflows(ctx, filter)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, 0, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, count, nil
}
