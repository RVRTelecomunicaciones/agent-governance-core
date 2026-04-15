package tracing

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

var _ inbound.GovernanceService = (*TracedGovernanceService)(nil)

// TracedGovernanceService wraps a GovernanceService with OpenTelemetry tracing.
type TracedGovernanceService struct {
	next   inbound.GovernanceService
	tracer trace.Tracer
}

// NewTracedGovernanceService creates a new tracing decorator for GovernanceService.
func NewTracedGovernanceService(next inbound.GovernanceService, tracer trace.Tracer) *TracedGovernanceService {
	return &TracedGovernanceService{next: next, tracer: tracer}
}

func (t *TracedGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.SubmitTask",
		trace.WithAttributes(
			attribute.String("governance.action", "SubmitTask"),
			attribute.String("governance.task_type", string(input.Type)),
			attribute.String("governance.task_scope", string(input.Scope)),
		),
	)
	defer span.End()

	result, err := t.next.SubmitTask(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.task_id", string(result.ID())),
		attribute.String("governance.risk_level", string(result.RiskLevel())),
		attribute.String("governance.outcome", "submitted"),
	)

	return result, nil
}

func (t *TracedGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.ProcessTask",
		trace.WithAttributes(
			attribute.String("governance.action", "ProcessTask"),
			attribute.String("governance.task_type", string(input.Type)),
			attribute.String("governance.task_scope", string(input.Scope)),
		),
	)
	defer span.End()

	result, err := t.next.ProcessTask(ctx, input, action)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if result.Task != nil {
		span.SetAttributes(
			attribute.String("governance.task_id", string(result.Task.ID())),
		)
	}
	span.SetAttributes(attribute.String("governance.outcome", "processed"))

	return result, nil
}

func (t *TracedGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.RouteTask",
		trace.WithAttributes(
			attribute.String("governance.action", "RouteTask"),
			attribute.String("governance.task_id", string(taskID)),
		),
	)
	defer span.End()

	result, err := t.next.RouteTask(ctx, taskID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	overridden := wasOverridden(result)
	span.SetAttributes(
		attribute.String("governance.strategy", string(result.SelectedStrategy())),
		attribute.Bool("governance.strategy_overridden", overridden),
		attribute.String("governance.agent_role", string(result.SelectedAgentRole())),
		attribute.String("governance.outcome", "routed"),
	)

	return result, nil
}

func (t *TracedGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.EvaluatePolicy",
		trace.WithAttributes(
			attribute.String("governance.action", "EvaluatePolicy"),
			attribute.String("governance.task_id", string(taskID)),
		),
	)
	defer span.End()

	result, err := t.next.EvaluatePolicy(ctx, taskID, action)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.policy_outcome", result.Outcome().String()),
		attribute.String("governance.outcome", "evaluated"),
	)

	return result, nil
}

func (t *TracedGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.StartWorkflow",
		trace.WithAttributes(
			attribute.String("governance.action", "StartWorkflow"),
			attribute.String("governance.task_id", string(taskID)),
		),
	)
	defer span.End()

	result, err := t.next.StartWorkflow(ctx, taskID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.workflow_run_id", string(result.ID)),
		attribute.String("governance.workflow_status", string(result.Status)),
		attribute.String("governance.outcome", "started"),
	)

	return result, nil
}

// wasOverridden checks if the selected strategy in a routing decision was overridden.
func wasOverridden(d *routing.RoutingDecision) bool {
	for _, eval := range d.EvaluatedStrategies() {
		if eval.Strategy == d.SelectedStrategy() && eval.Overridden {
			return true
		}
	}
	return false
}

// extractCategory extracts the category prefix from a failure code (e.g. "runtime/timeout" → "runtime").
func extractCategory(code string) string {
	if i := strings.Index(code, "/"); i >= 0 {
		return code[:i]
	}
	return code
}
