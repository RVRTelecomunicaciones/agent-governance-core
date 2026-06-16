package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
)

// MetricsGovernanceService is a decorator that emits OTel metrics around a
// GovernanceService implementation.
type MetricsGovernanceService struct {
	next inbound.GovernanceService
	inst *Instruments
}

var _ inbound.GovernanceService = (*MetricsGovernanceService)(nil)

// NewMetricsGovernanceService wraps next with metric instrumentation.
func NewMetricsGovernanceService(next inbound.GovernanceService, inst *Instruments) *MetricsGovernanceService {
	return &MetricsGovernanceService{next: next, inst: inst}
}

// SubmitTask delegates to the next service and increments tasks.submitted on success.
func (m *MetricsGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	t, err := m.next.SubmitTask(ctx, input)
	if err != nil {
		return nil, err
	}

	m.inst.TasksSubmitted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", string(input.Type)),
		attribute.String("scope", string(input.Scope)),
		attribute.String("risk_level", string(t.RiskLevel())),
	))

	return t, nil
}

// RouteTask delegates to the next service and records routing.duration_ms on success.
func (m *MetricsGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	start := time.Now()
	result, err := m.next.RouteTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	elapsed := float64(time.Since(start).Milliseconds())

	attrs := []attribute.KeyValue{
		attribute.String("strategy", string(result.SelectedStrategy())),
	}

	evals := result.EvaluatedStrategies()
	overridden := "false"
	if len(evals) > 0 && evals[0].Overridden {
		overridden = "true"
	}
	attrs = append(attrs, attribute.String("overridden", overridden))

	m.inst.RoutingDuration.Record(ctx, elapsed, metric.WithAttributes(attrs...))

	return result, nil
}

// EvaluatePolicy delegates to the next service and records policy.duration_ms
// and policy.outcomes on success.
func (m *MetricsGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	start := time.Now()
	result, err := m.next.EvaluatePolicy(ctx, taskID, action)
	if err != nil {
		return nil, err
	}

	elapsed := float64(time.Since(start).Milliseconds())

	outcomeLabel := result.Outcome().String()

	m.inst.PolicyDuration.Record(ctx, elapsed, metric.WithAttributes(
		attribute.String("outcome", outcomeLabel),
	))

	m.inst.PolicyOutcomes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcomeLabel),
	))

	return result, nil
}

// ProcessTask delegates directly without emitting decorator metrics.
// Sub-calls (SubmitTask, RouteTask, EvaluatePolicy) emit their own metrics.
func (m *MetricsGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	return m.next.ProcessTask(ctx, input, action)
}

// StartWorkflow delegates directly without emitting decorator metrics.
// Lifecycle metrics are emitted inline in the use case.
func (m *MetricsGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return m.next.StartWorkflow(ctx, taskID)
}
