package metrics_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/metrics"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

// --- mock workflow control ---

type mockWorkflowControl struct {
	registerAttemptFn func(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error)
	killWorkflowFn    func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	pauseWorkflowFn   func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	resumeWorkflowFn  func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
}

func (m *mockWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	if m.registerAttemptFn != nil {
		return m.registerAttemptFn(ctx, id, result)
	}
	return nil, errors.New("not implemented")
}

func (m *mockWorkflowControl) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	if m.killWorkflowFn != nil {
		return m.killWorkflowFn(ctx, id, reason, actor)
	}
	return errors.New("not implemented")
}

func (m *mockWorkflowControl) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	if m.pauseWorkflowFn != nil {
		return m.pauseWorkflowFn(ctx, id, reason, actor)
	}
	return errors.New("not implemented")
}

func (m *mockWorkflowControl) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	if m.resumeWorkflowFn != nil {
		return m.resumeWorkflowFn(ctx, id, reason, actor)
	}
	return errors.New("not implemented")
}

func newWorkflowRunID(t *testing.T) shared.WorkflowRunID {
	t.Helper()
	id, err := shared.NewWorkflowRunID(ulid.Make().String())
	require.NoError(t, err)
	return id
}

// --- tests ---

func TestRegisterAttemptWithStrategyAndRole(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	mock := &mockWorkflowControl{
		registerAttemptFn: func(_ context.Context, _ shared.WorkflowRunID, _ execution.AttemptResult) (*workflow.WorkflowRun, error) {
			return &workflow.WorkflowRun{}, nil
		},
	}

	svc := metrics.NewMetricsWorkflowControl(mock, inst)

	result := execution.NewSuccessResult().
		WithStrategy("direct").
		WithAgentRole("implementer")

	_, err = svc.RegisterAttempt(context.Background(), newWorkflowRunID(t), result)
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	m := findMetric(rm, "governance.execution.attempts")
	require.NotNil(t, m, "expected governance.execution.attempts metric")

	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.GreaterOrEqual(t, hist.DataPoints[0].Count, uint64(1))

	attrs := hist.DataPoints[0].Attributes
	stratVal, found := attrs.Value(attribute.Key("strategy"))
	assert.True(t, found)
	assert.Equal(t, "direct", stratVal.AsString())

	roleVal, found := attrs.Value(attribute.Key("agent_role"))
	assert.True(t, found)
	assert.Equal(t, "implementer", roleVal.AsString())
}

func TestRegisterAttemptWithoutStrategyOrRole(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	mock := &mockWorkflowControl{
		registerAttemptFn: func(_ context.Context, _ shared.WorkflowRunID, _ execution.AttemptResult) (*workflow.WorkflowRun, error) {
			return &workflow.WorkflowRun{}, nil
		},
	}

	svc := metrics.NewMetricsWorkflowControl(mock, inst)

	result := execution.NewSuccessResult() // no strategy, no agent role

	_, err = svc.RegisterAttempt(context.Background(), newWorkflowRunID(t), result)
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	m := findMetric(rm, "governance.execution.attempts")
	require.NotNil(t, m, "expected governance.execution.attempts metric")

	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.GreaterOrEqual(t, hist.DataPoints[0].Count, uint64(1))

	attrs := hist.DataPoints[0].Attributes
	_, found := attrs.Value(attribute.Key("strategy"))
	assert.False(t, found, "strategy label should not be present")

	_, found = attrs.Value(attribute.Key("agent_role"))
	assert.False(t, found, "agent_role label should not be present")
}

func TestKillWorkflowEmitsTransition(t *testing.T) {
	meter, reader := setupMeter(t)
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	mock := &mockWorkflowControl{
		killWorkflowFn: func(_ context.Context, _ shared.WorkflowRunID, _ string, _ shared.ActorID) error {
			return nil
		},
	}

	svc := metrics.NewMetricsWorkflowControl(mock, inst)

	err = svc.KillWorkflow(context.Background(), newWorkflowRunID(t), "test", shared.ActorID("admin"))
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	m := findMetric(rm, "governance.workflow.transitions")
	require.NotNil(t, m, "expected governance.workflow.transitions metric")

	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(1), sum.DataPoints[0].Value)

	transVal, found := sum.DataPoints[0].Attributes.Value(attribute.Key("transition"))
	assert.True(t, found)
	assert.Equal(t, "killed", transVal.AsString())
}
