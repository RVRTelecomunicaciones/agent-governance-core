package tracing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
)

// --- mock WorkflowControl ---

type mockWorkflowControl struct {
	killWorkflowFn    func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	pauseWorkflowFn   func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	resumeWorkflowFn  func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	registerAttemptFn func(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error)
}

func (m *mockWorkflowControl) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return m.killWorkflowFn(ctx, id, reason, actor)
}

func (m *mockWorkflowControl) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return m.pauseWorkflowFn(ctx, id, reason, actor)
}

func (m *mockWorkflowControl) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return m.resumeWorkflowFn(ctx, id, reason, actor)
}

func (m *mockWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	return m.registerAttemptFn(ctx, id, result)
}

var _ inbound.WorkflowControl = (*mockWorkflowControl)(nil)

func setupWorkflowTracer(t *testing.T) (*tracetest.InMemoryExporter, trace.Tracer) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exporter, tp.Tracer("test")
}

func TestTracedWorkflowControl_RegisterAttempt_Success_NoFailureAttrs(t *testing.T) {
	exporter, tracer := setupWorkflowTracer(t)

	now, err := shared.NewTimestamp(time.Now())
	require.NoError(t, err)

	wfRun := workflow.NewWorkflowRun("wf-001", "task-001", now)

	mock := &mockWorkflowControl{
		registerAttemptFn: func(_ context.Context, _ shared.WorkflowRunID, _ execution.AttemptResult) (*workflow.WorkflowRun, error) {
			return wfRun, nil
		},
	}

	traced := NewTracedWorkflowControl(mock, tracer)
	result, err := traced.RegisterAttempt(context.Background(), "wf-001", execution.NewSuccessResult())

	require.NoError(t, err)
	assert.Equal(t, wfRun, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "WorkflowControl.RegisterAttempt", span.Name)

	// Start attributes
	v, ok := findAttr(span.Attributes, "governance.action")
	assert.True(t, ok)
	assert.Equal(t, "RegisterAttempt", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.workflow_run_id")
	assert.True(t, ok)
	assert.Equal(t, "wf-001", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.attempt_status")
	assert.True(t, ok)
	assert.Equal(t, string(execution.AttemptStatusSuccess), v.AsString())

	// Outcome is the workflow status
	v, ok = findAttr(span.Attributes, "governance.outcome")
	assert.True(t, ok)
	assert.Equal(t, string(wfRun.Status), v.AsString())

	// Failure attributes NOT present on success
	_, ok = findAttr(span.Attributes, "governance.failure_stage")
	assert.False(t, ok)
	_, ok = findAttr(span.Attributes, "governance.failure_category")
	assert.False(t, ok)

	// No error status
	assert.Equal(t, otelcodes.Unset, span.Status.Code)
}

func TestTracedWorkflowControl_RegisterAttempt_Failure_HasFailureAttrs(t *testing.T) {
	exporter, tracer := setupWorkflowTracer(t)

	now, err := shared.NewTimestamp(time.Now())
	require.NoError(t, err)

	wfRun := workflow.NewWorkflowRun("wf-001", "task-001", now)

	mock := &mockWorkflowControl{
		registerAttemptFn: func(_ context.Context, _ shared.WorkflowRunID, _ execution.AttemptResult) (*workflow.WorkflowRun, error) {
			return wfRun, nil
		},
	}

	failResult, err := execution.NewFailureResult(shared.FailureStageExecution, "runtime/timeout", true)
	require.NoError(t, err)

	traced := NewTracedWorkflowControl(mock, tracer)
	result, err := traced.RegisterAttempt(context.Background(), "wf-001", failResult)

	require.NoError(t, err)
	assert.Equal(t, wfRun, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]

	// Failure attributes present
	v, ok := findAttr(span.Attributes, "governance.failure_stage")
	assert.True(t, ok)
	assert.Equal(t, "execution", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.failure_category")
	assert.True(t, ok)
	assert.Equal(t, "runtime", v.AsString())

	// Attempt status reflects failure
	v, ok = findAttr(span.Attributes, "governance.attempt_status")
	assert.True(t, ok)
	assert.Equal(t, string(execution.AttemptStatusFailure), v.AsString())
}

func TestTracedWorkflowControl_KillWorkflow(t *testing.T) {
	exporter, tracer := setupWorkflowTracer(t)

	mock := &mockWorkflowControl{
		killWorkflowFn: func(_ context.Context, _ shared.WorkflowRunID, _ string, _ shared.ActorID) error {
			return nil
		},
	}

	traced := NewTracedWorkflowControl(mock, tracer)
	err := traced.KillWorkflow(context.Background(), "wf-002", "emergency stop", "actor-001")

	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "WorkflowControl.KillWorkflow", span.Name)

	v, ok := findAttr(span.Attributes, "governance.action")
	assert.True(t, ok)
	assert.Equal(t, "KillWorkflow", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.workflow_run_id")
	assert.True(t, ok)
	assert.Equal(t, "wf-002", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.kill_reason")
	assert.True(t, ok)
	assert.Equal(t, "emergency stop", v.AsString())

	v, ok = findAttr(span.Attributes, "governance.outcome")
	assert.True(t, ok)
	assert.Equal(t, "killed", v.AsString())

	assert.Equal(t, otelcodes.Unset, span.Status.Code)
}
