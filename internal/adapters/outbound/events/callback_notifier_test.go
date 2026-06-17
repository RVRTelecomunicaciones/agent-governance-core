package events_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/events"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
)

func testWorkflowRun(t *testing.T) *workflow.WorkflowRun {
	t.Helper()
	wfID, err := shared.NewWorkflowRunID("01KP7WTMTBAE67Z5NHCJZHQ5XV")
	require.NoError(t, err)
	taskID, err := shared.NewTaskID("01KP7WTMTBKX5V8WQ8KXDH917J")
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC))
	return workflow.NewWorkflowRun(wfID, taskID, now)
}

func testLease(t *testing.T) *execution.ExecutionLease {
	t.Helper()
	leaseID, err := shared.NewExecutionLeaseID("01KP7WTMTBA84WKP9M7T9BM2EX")
	require.NoError(t, err)
	wfID, err := shared.NewWorkflowRunID("01KP7WTMTBAE67Z5NHCJZHQ5XV")
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC))
	rb, err := execution.NewRetryBudget(3)
	require.NoError(t, err)
	lease, err := execution.NewExecutionLease(leaseID, wfID, 5*time.Minute, rb, now)
	require.NoError(t, err)
	return lease
}

func TestCallbackNotifier_NoCallbacksRegistered_ReturnsNil(t *testing.T) {
	notifier := events.NewCallbackNotifier()
	ctx := context.Background()
	wf := testWorkflowRun(t)

	assert.NoError(t, notifier.OnExecutionReady(ctx, wf, testLease(t)))
	assert.NoError(t, notifier.OnApprovalRequired(ctx, wf, nil))
	assert.NoError(t, notifier.OnWorkflowTerminated(ctx, wf, "test reason"))
}

func TestCallbackNotifier_CallbacksInvoked(t *testing.T) {
	notifier := events.NewCallbackNotifier()
	ctx := context.Background()
	wf := testWorkflowRun(t)
	lease := testLease(t)

	var executionReadyCalled bool
	notifier.SetOnExecutionReady(func(_ context.Context, gotWF *workflow.WorkflowRun, gotLease *execution.ExecutionLease) error {
		executionReadyCalled = true
		assert.Equal(t, wf, gotWF)
		assert.Equal(t, lease, gotLease)
		return nil
	})

	err := notifier.OnExecutionReady(ctx, wf, lease)
	assert.NoError(t, err)
	assert.True(t, executionReadyCalled)
}

func TestCallbackNotifier_CallbackReturnsError(t *testing.T) {
	notifier := events.NewCallbackNotifier()
	ctx := context.Background()
	wf := testWorkflowRun(t)

	expectedErr := errors.New("callback failed")

	notifier.SetOnWorkflowTerminated(func(_ context.Context, _ *workflow.WorkflowRun, _ string) error {
		return expectedErr
	})

	err := notifier.OnWorkflowTerminated(ctx, wf, "termination reason")
	assert.ErrorIs(t, err, expectedErr)
}

func TestCallbackNotifier_ApprovalRequiredReceivesCorrectArgs(t *testing.T) {
	notifier := events.NewCallbackNotifier()
	ctx := context.Background()
	wf := testWorkflowRun(t)

	reqID, err := shared.NewApprovalRequestID("01KP7WTMTBKN9DZNWV9NV456V1")
	require.NoError(t, err)
	taskID, err := shared.NewTaskID("01KP7WTMTBKX5V8WQ8KXDH917J")
	require.NoError(t, err)
	wfID, err := shared.NewWorkflowRunID("01KP7WTMTBAE67Z5NHCJZHQ5XV")
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC))
	req, err := approval.NewApprovalRequest(reqID, taskID, wfID, "policy_violation", approval.ApproverSpec{Role: "human_review"}, nil, now)
	require.NoError(t, err)

	var receivedReq *approval.ApprovalRequest
	notifier.SetOnApprovalRequired(func(_ context.Context, _ *workflow.WorkflowRun, r *approval.ApprovalRequest) error {
		receivedReq = r
		return nil
	})

	err = notifier.OnApprovalRequired(ctx, wf, req)
	assert.NoError(t, err)
	assert.Equal(t, req, receivedReq)
}
