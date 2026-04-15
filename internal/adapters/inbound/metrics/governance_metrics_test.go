package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/metrics"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

// --- test helpers ---

func setupMeter(t *testing.T) (metric.Meter, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { mp.Shutdown(context.Background()) })
	return mp.Meter("test"), reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

func newTaskID(t *testing.T) shared.TaskID {
	t.Helper()
	id, err := shared.NewTaskID(ulid.Make().String())
	require.NoError(t, err)
	return id
}

func newRoutingDecisionID(t *testing.T) shared.RoutingDecisionID {
	t.Helper()
	id, err := shared.NewRoutingDecisionID(ulid.Make().String())
	require.NoError(t, err)
	return id
}

func newPolicyDecisionID(t *testing.T) shared.PolicyDecisionID {
	t.Helper()
	id, err := shared.NewPolicyDecisionID(ulid.Make().String())
	require.NoError(t, err)
	return id
}

func mustTimestamp(t *testing.T) shared.Timestamp {
	t.Helper()
	return shared.MustTimestamp(time.Now())
}

// --- mock governance service ---

type mockGovernanceService struct {
	submitTaskFn     func(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error)
	routeTaskFn      func(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
	evaluatePolicyFn func(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
	processTaskFn    func(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error)
	startWorkflowFn  func(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}

func (m *mockGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	if m.submitTaskFn != nil {
		return m.submitTaskFn(ctx, input)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	if m.routeTaskFn != nil {
		return m.routeTaskFn(ctx, taskID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	if m.evaluatePolicyFn != nil {
		return m.evaluatePolicyFn(ctx, taskID, action)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	if m.processTaskFn != nil {
		return m.processTaskFn(ctx, input, action)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	if m.startWorkflowFn != nil {
		return m.startWorkflowFn(ctx, taskID)
	}
	return nil, errors.New("not implemented")
}

// --- tests ---

func TestSubmitTaskIncrementsCounter(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	now := mustTimestamp(t)
	taskID := newTaskID(t)
	testTask, err := task.NewTask(
		taskID, nil,
		task.TypeDevelopment, "test task", task.ScopeModule,
		shared.PriorityNormal, shared.RiskLevelMedium,
		nil, now,
	)
	require.NoError(t, err)

	mock := &mockGovernanceService{
		submitTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput) (*task.Task, error) {
			return testTask, nil
		},
	}

	svc := metrics.NewMetricsGovernanceService(mock, inst)

	_, err = svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:  task.TypeDevelopment,
		Title: "test task",
		Scope: task.ScopeModule,
	})
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	m := findMetric(rm, "governance.tasks.submitted")
	require.NotNil(t, m, "expected governance.tasks.submitted metric")

	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected Sum[int64] aggregation")
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(1), sum.DataPoints[0].Value)

	// Verify labels.
	attrs := sum.DataPoints[0].Attributes
	typeVal, found := attrs.Value(attribute.Key("type"))
	assert.True(t, found)
	assert.Equal(t, "development", typeVal.AsString())

	scopeVal, found := attrs.Value(attribute.Key("scope"))
	assert.True(t, found)
	assert.Equal(t, "module", scopeVal.AsString())

	riskVal, found := attrs.Value(attribute.Key("risk_level"))
	assert.True(t, found)
	assert.Equal(t, "medium", riskVal.AsString())
}

func TestRouteTaskRecordsDuration(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	decision := routing.Reconstruct(
		newRoutingDecisionID(t), newTaskID(t),
		[]routing.StrategyEvaluation{{
			Strategy:   routing.StrategyDirect,
			Score:      0.9,
			Overridden: false,
		}},
		routing.StrategyDirect, routing.RoleImplementer,
		"best match", nil, mustTimestamp(t),
	)

	mock := &mockGovernanceService{
		routeTaskFn: func(_ context.Context, _ shared.TaskID) (*routing.RoutingDecision, error) {
			return decision, nil
		},
	}

	svc := metrics.NewMetricsGovernanceService(mock, inst)

	_, err = svc.RouteTask(context.Background(), newTaskID(t))
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	m := findMetric(rm, "governance.routing.duration_ms")
	require.NotNil(t, m, "expected governance.routing.duration_ms metric")

	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "expected Histogram[float64] aggregation")
	require.Len(t, hist.DataPoints, 1)
	assert.GreaterOrEqual(t, hist.DataPoints[0].Count, uint64(1))

	// Verify labels.
	attrs := hist.DataPoints[0].Attributes
	stratVal, found := attrs.Value(attribute.Key("strategy"))
	assert.True(t, found)
	assert.Equal(t, "direct", stratVal.AsString())

	overVal, found := attrs.Value(attribute.Key("overridden"))
	assert.True(t, found)
	assert.Equal(t, "false", overVal.AsString())
}

func TestEvaluatePolicyRecordsDurationAndOutcome(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	pd := policy.Reconstruct(
		newPolicyDecisionID(t), newTaskID(t),
		"deploy", policy.OutcomeAllow,
		nil, nil, nil, "all good", mustTimestamp(t),
	)

	mock := &mockGovernanceService{
		evaluatePolicyFn: func(_ context.Context, _ shared.TaskID, _ string) (*policy.PolicyDecision, error) {
			return pd, nil
		},
	}

	svc := metrics.NewMetricsGovernanceService(mock, inst)

	_, err = svc.EvaluatePolicy(context.Background(), newTaskID(t), "deploy")
	require.NoError(t, err)

	rm := collectMetrics(t, reader)

	// Check duration histogram.
	durMetric := findMetric(rm, "governance.policy.duration_ms")
	require.NotNil(t, durMetric, "expected governance.policy.duration_ms metric")
	hist, ok := durMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.GreaterOrEqual(t, hist.DataPoints[0].Count, uint64(1))

	// Check outcome counter.
	outMetric := findMetric(rm, "governance.policy.outcomes")
	require.NotNil(t, outMetric, "expected governance.policy.outcomes metric")
	sum, ok := outMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(1), sum.DataPoints[0].Value)

	outcomeVal, found := sum.DataPoints[0].Attributes.Value(attribute.Key("outcome"))
	assert.True(t, found)
	assert.Equal(t, "allow", outcomeVal.AsString())
}

func TestNoMetricsOnError(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	mock := &mockGovernanceService{
		submitTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput) (*task.Task, error) {
			return nil, errors.New("submit failed")
		},
		routeTaskFn: func(_ context.Context, _ shared.TaskID) (*routing.RoutingDecision, error) {
			return nil, errors.New("route failed")
		},
		evaluatePolicyFn: func(_ context.Context, _ shared.TaskID, _ string) (*policy.PolicyDecision, error) {
			return nil, errors.New("policy failed")
		},
	}

	svc := metrics.NewMetricsGovernanceService(mock, inst)

	_, _ = svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{})
	_, _ = svc.RouteTask(context.Background(), newTaskID(t))
	_, _ = svc.EvaluatePolicy(context.Background(), newTaskID(t), "deploy")

	rm := collectMetrics(t, reader)

	// No metrics should have data points.
	assert.Nil(t, findMetric(rm, "governance.tasks.submitted"))
	assert.Nil(t, findMetric(rm, "governance.routing.duration_ms"))
	assert.Nil(t, findMetric(rm, "governance.policy.duration_ms"))
	assert.Nil(t, findMetric(rm, "governance.policy.outcomes"))
}

func TestProcessTaskDelegatesWithoutMetrics(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	called := false
	mock := &mockGovernanceService{
		processTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput, _ string) (*inbound.ProcessTaskResult, error) {
			called = true
			return &inbound.ProcessTaskResult{}, nil
		},
	}

	svc := metrics.NewMetricsGovernanceService(mock, inst)

	_, err = svc.ProcessTask(context.Background(), inbound.SubmitTaskInput{}, "deploy")
	require.NoError(t, err)
	assert.True(t, called)

	rm := collectMetrics(t, reader)
	// ProcessTask itself should NOT emit any metrics — verify no non-zero data.
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					assert.Equal(t, int64(0), dp.Value, "unexpected metric %s", m.Name)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					assert.Equal(t, uint64(0), dp.Count, "unexpected metric %s", m.Name)
				}
			}
		}
	}
}

func TestStartWorkflowDelegatesWithoutMetrics(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	called := false
	mock := &mockGovernanceService{
		startWorkflowFn: func(_ context.Context, _ shared.TaskID) (*workflow.WorkflowRun, error) {
			called = true
			return &workflow.WorkflowRun{}, nil
		},
	}

	svc := metrics.NewMetricsGovernanceService(mock, inst)

	_, err = svc.StartWorkflow(context.Background(), newTaskID(t))
	require.NoError(t, err)
	assert.True(t, called)

	rm := collectMetrics(t, reader)
	// StartWorkflow itself should NOT emit any metrics.
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					assert.Equal(t, int64(0), dp.Value, "unexpected metric %s", m.Name)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					assert.Equal(t, uint64(0), dp.Count, "unexpected metric %s", m.Name)
				}
			}
		}
	}
}
