package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

// --- test helpers ---

func setupTracer(t *testing.T) (*tracetest.InMemoryExporter, trace.Tracer) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exporter, tp.Tracer("test")
}

func findAttr(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value, true
		}
	}
	return attribute.Value{}, false
}

func mustTimestamp(t *testing.T) shared.Timestamp {
	t.Helper()
	ts, err := shared.NewTimestamp(time.Now())
	require.NoError(t, err)
	return ts
}

// --- mock GovernanceService ---

type mockGovernanceService struct {
	submitTaskFn     func(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error)
	processTaskFn    func(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error)
	routeTaskFn      func(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
	evaluatePolicyFn func(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
	startWorkflowFn  func(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}

func (m *mockGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	return m.submitTaskFn(ctx, input)
}

func (m *mockGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	return m.processTaskFn(ctx, input, action)
}

func (m *mockGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	return m.routeTaskFn(ctx, taskID)
}

func (m *mockGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	return m.evaluatePolicyFn(ctx, taskID, action)
}

func (m *mockGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return m.startWorkflowFn(ctx, taskID)
}

// --- tests ---

func TestTracedGovernanceService_SubmitTask_Success(t *testing.T) {
	exporter, tracer := setupTracer(t)
	now := mustTimestamp(t)

	testTask, err := task.NewTask(
		"task-001", nil, task.TypeDevelopment, "Test Task",
		task.ScopeModule, shared.PriorityNormal, shared.RiskLevelMedium,
		task.TaskMetadata{"key": "value"}, now,
	)
	require.NoError(t, err)

	mock := &mockGovernanceService{
		submitTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput) (*task.Task, error) {
			return testTask, nil
		},
	}

	traced := NewTracedGovernanceService(mock, tracer)
	result, err := traced.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:  task.TypeDevelopment,
		Title: "Test Task",
		Scope: task.ScopeModule,
	})

	require.NoError(t, err)
	assert.Equal(t, testTask, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "GovernanceService.SubmitTask", span.Name)

	// Start attributes
	v, ok := findAttr(span.Attributes, "governance.action")
	assert.True(t, ok)
	assert.Equal(t, "SubmitTask", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.task_type")
	assert.True(t, ok)
	assert.Equal(t, "development", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.task_scope")
	assert.True(t, ok)
	assert.Equal(t, "module", v.AsString())

	// End attributes
	v, ok = findAttr(span.Attributes, "governance.task_id")
	assert.True(t, ok)
	assert.Equal(t, "task-001", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.risk_level")
	assert.True(t, ok)
	assert.Equal(t, "medium", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.outcome")
	assert.True(t, ok)
	assert.Equal(t, "submitted", v.AsString())

	// No error status
	assert.Equal(t, otelcodes.Unset, span.Status.Code)
}

func TestTracedGovernanceService_SubmitTask_Error(t *testing.T) {
	exporter, tracer := setupTracer(t)
	submitErr := errors.New("validation failed")

	mock := &mockGovernanceService{
		submitTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput) (*task.Task, error) {
			return nil, submitErr
		},
	}

	traced := NewTracedGovernanceService(mock, tracer)
	_, err := traced.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:  task.TypeDevelopment,
		Title: "Bad Task",
		Scope: task.ScopeFile,
	})

	require.Error(t, err)
	assert.Equal(t, submitErr, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "GovernanceService.SubmitTask", span.Name)

	// Error recorded
	assert.Equal(t, otelcodes.Error, span.Status.Code)
	assert.Equal(t, "validation failed", span.Status.Description)

	// Error event
	require.NotEmpty(t, span.Events)

	// No outcome attribute on error
	_, ok := findAttr(span.Attributes, "governance.outcome")
	assert.False(t, ok)
}

func TestTracedGovernanceService_RouteTask_Attributes(t *testing.T) {
	exporter, tracer := setupTracer(t)
	now := mustTimestamp(t)

	rd, err := routing.NewRoutingDecision(
		"rd-001", "task-001",
		[]routing.StrategyEvaluation{
			{Strategy: routing.StrategyDirect, Score: 0.8, FactorScores: map[string]float64{"risk": 0.8}, Overridden: false, Reason: "best fit"},
		},
		routing.StrategyDirect,
		routing.RoleImplementer,
		"direct strategy selected",
		nil,
		now,
	)
	require.NoError(t, err)

	mock := &mockGovernanceService{
		routeTaskFn: func(_ context.Context, _ shared.TaskID) (*routing.RoutingDecision, error) {
			return rd, nil
		},
	}

	traced := NewTracedGovernanceService(mock, tracer)
	result, err := traced.RouteTask(context.Background(), "task-001")

	require.NoError(t, err)
	assert.Equal(t, rd, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "GovernanceService.RouteTask", span.Name)

	// Start attribute
	v, ok := findAttr(span.Attributes, "governance.task_id")
	assert.True(t, ok)
	assert.Equal(t, "task-001", v.AsString())

	// End attributes
	v, ok = findAttr(span.Attributes, "governance.strategy")
	assert.True(t, ok)
	assert.Equal(t, "direct", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.strategy_overridden")
	assert.True(t, ok)
	assert.False(t, v.AsBool())

	v, ok = findAttr(span.Attributes, "governance.agent_role")
	assert.True(t, ok)
	assert.Equal(t, "implementer", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.outcome")
	assert.True(t, ok)
	assert.Equal(t, "routed", v.AsString())

	// Adaptive attribute should be false (no adaptive adjustments in this decision)
	v, ok = findAttr(span.Attributes, "governance.adaptive_applied")
	assert.True(t, ok)
	assert.False(t, v.AsBool())

	// No detailed adaptive attributes when adaptive is not applied
	_, ok = findAttr(span.Attributes, "governance.adaptive_final_adjustment")
	assert.False(t, ok)
}

func TestTracedGovernanceService_RouteTask_AdaptiveAttributes(t *testing.T) {
	exporter, tracer := setupTracer(t)
	now := mustTimestamp(t)

	rd, err := routing.NewRoutingDecision(
		"rd-002", "task-002",
		[]routing.StrategyEvaluation{
			{
				Strategy:      routing.StrategyDirect,
				Score:         0.80,
				AdjustedScore: 0.75,
				FactorScores:  map[string]float64{"risk": 0.8},
				Overridden:    false,
				Reason:        "adaptive adjusted",
				AdaptiveAdjustment: &routing.AdaptiveAdjustment{
					RawAdjustment:   -0.05,
					Confidence:      1.0,
					FinalAdjustment: -0.05,
					FailureRate:     0.45,
					SampleSize:      40,
					Granularity:     "strategy",
					Window:          "48h",
				},
			},
		},
		routing.StrategyDirect,
		routing.RoleImplementer,
		"adaptive routing",
		nil,
		now,
	)
	require.NoError(t, err)

	mock := &mockGovernanceService{
		routeTaskFn: func(_ context.Context, _ shared.TaskID) (*routing.RoutingDecision, error) {
			return rd, nil
		},
	}

	traced := NewTracedGovernanceService(mock, tracer)
	result, err := traced.RouteTask(context.Background(), "task-002")

	require.NoError(t, err)
	assert.Equal(t, rd, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]

	// Adaptive applied should be true
	v, ok := findAttr(span.Attributes, "governance.adaptive_applied")
	assert.True(t, ok)
	assert.True(t, v.AsBool())

	// Detailed adaptive attributes for the selected strategy
	v, ok = findAttr(span.Attributes, "governance.adaptive_final_adjustment")
	assert.True(t, ok)
	assert.InDelta(t, -0.05, v.AsFloat64(), 0.0001)

	v, ok = findAttr(span.Attributes, "governance.adaptive_failure_rate")
	assert.True(t, ok)
	assert.InDelta(t, 0.45, v.AsFloat64(), 0.0001)

	v, ok = findAttr(span.Attributes, "governance.adaptive_confidence")
	assert.True(t, ok)
	assert.InDelta(t, 1.0, v.AsFloat64(), 0.0001)

	v, ok = findAttr(span.Attributes, "governance.adaptive_sample_size")
	assert.True(t, ok)
	assert.Equal(t, int64(40), v.AsInt64())

	v, ok = findAttr(span.Attributes, "governance.adaptive_granularity")
	assert.True(t, ok)
	assert.Equal(t, "strategy", v.AsString())
}

func TestTracedGovernanceService_EvaluatePolicy_Attributes(t *testing.T) {
	exporter, tracer := setupTracer(t)
	now := mustTimestamp(t)

	pd, err := policy.NewPolicyDecision(
		"pd-001", "task-001", "deploy",
		policy.OutcomeAllow,
		nil, nil,
		[]policy.RuleEvaluation{{RuleID: "r1", Passed: true, Reason: "ok"}},
		"allowed by policy",
		now,
	)
	require.NoError(t, err)

	mock := &mockGovernanceService{
		evaluatePolicyFn: func(_ context.Context, _ shared.TaskID, _ string) (*policy.PolicyDecision, error) {
			return pd, nil
		},
	}

	traced := NewTracedGovernanceService(mock, tracer)
	result, err := traced.EvaluatePolicy(context.Background(), "task-001", "deploy")

	require.NoError(t, err)
	assert.Equal(t, pd, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "GovernanceService.EvaluatePolicy", span.Name)

	v, ok := findAttr(span.Attributes, "governance.policy_outcome")
	assert.True(t, ok)
	assert.Equal(t, "allow", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.outcome")
	assert.True(t, ok)
	assert.Equal(t, "evaluated", v.AsString())
}
