package execution

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTimestamp() shared.Timestamp {
	return shared.MustTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

func testLeaseID() shared.ExecutionLeaseID {
	id, _ := shared.NewExecutionLeaseID("01JSGBAT6HQXF3Y8PZN4RK0010")
	return id
}

func testWorkflowRunID() shared.WorkflowRunID {
	id, _ := shared.NewWorkflowRunID("01JSGBAT6HQXF3Y8PZN4RK0011")
	return id
}

func newTestLease(maxAttempts int) *ExecutionLease {
	rb, _ := NewRetryBudget(maxAttempts)
	lease, _ := NewExecutionLease(testLeaseID(), testWorkflowRunID(), 5*time.Minute, rb, testTimestamp())
	return lease
}

func TestNewExecutionLease_Active(t *testing.T) {
	rb, err := NewRetryBudget(3)
	require.NoError(t, err)

	lease, err := NewExecutionLease(testLeaseID(), testWorkflowRunID(), 5*time.Minute, rb, testTimestamp())
	require.NoError(t, err)
	assert.Equal(t, LeaseStatusActive, lease.Status)
	assert.Equal(t, 0, lease.AttemptsUsed)
	assert.True(t, lease.HasRetryBudget())
}

func TestNewExecutionLease_ZeroTimeout_Fails(t *testing.T) {
	rb, _ := NewRetryBudget(3)
	_, err := NewExecutionLease(testLeaseID(), testWorkflowRunID(), 0, rb, testTimestamp())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTimeoutBudget)
}

func TestNewExecutionLease_NegativeTimeout_Fails(t *testing.T) {
	rb, _ := NewRetryBudget(3)
	_, err := NewExecutionLease(testLeaseID(), testWorkflowRunID(), -1*time.Second, rb, testTimestamp())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTimeoutBudget)
}

func TestRecordAttempt_IncrementsCount(t *testing.T) {
	lease := newTestLease(3)

	err := lease.RecordAttempt()
	require.NoError(t, err)
	assert.Equal(t, 1, lease.AttemptsUsed)
	assert.Equal(t, LeaseStatusActive, lease.Status)
}

func TestRecordAttempt_ExhaustsWhenMaxReached(t *testing.T) {
	lease := newTestLease(2)

	require.NoError(t, lease.RecordAttempt())
	assert.Equal(t, LeaseStatusActive, lease.Status)

	require.NoError(t, lease.RecordAttempt())
	assert.Equal(t, LeaseStatusExhausted, lease.Status)
	assert.Equal(t, 2, lease.AttemptsUsed)
}

func TestRecordAttempt_OnExhaustedLease_Fails(t *testing.T) {
	lease := newTestLease(1)
	require.NoError(t, lease.RecordAttempt())
	assert.Equal(t, LeaseStatusExhausted, lease.Status)

	err := lease.RecordAttempt()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLeaseNotActive)
}

func TestRecordAttempt_OnRevokedLease_Fails(t *testing.T) {
	lease := newTestLease(3)
	require.NoError(t, lease.Revoke())

	err := lease.RecordAttempt()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLeaseNotActive)
}

func TestRevoke_ActiveLease_Succeeds(t *testing.T) {
	lease := newTestLease(3)

	err := lease.Revoke()
	require.NoError(t, err)
	assert.Equal(t, LeaseStatusRevoked, lease.Status)
}

func TestRevoke_DoubleRevoke_Fails(t *testing.T) {
	lease := newTestLease(3)
	require.NoError(t, lease.Revoke())

	err := lease.Revoke()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLeaseNotActive)
}

func TestRetryBudget_Zero_Fails(t *testing.T) {
	_, err := NewRetryBudget(0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRetryBudget)
}

func TestRetryBudget_Negative_Fails(t *testing.T) {
	_, err := NewRetryBudget(-1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRetryBudget)
}

func TestHasRetryBudget_ReturnsFalseWhenExhausted(t *testing.T) {
	lease := newTestLease(1)
	assert.True(t, lease.HasRetryBudget())

	require.NoError(t, lease.RecordAttempt())
	assert.False(t, lease.HasRetryBudget())
}

func TestHasRetryBudget_ReturnsFalseWhenRevoked(t *testing.T) {
	lease := newTestLease(3)
	require.NoError(t, lease.Revoke())
	assert.False(t, lease.HasRetryBudget())
}

func TestUpdateTimeElapsed(t *testing.T) {
	lease := newTestLease(3)
	lease.UpdateTimeElapsed(2 * time.Minute)
	assert.Equal(t, 2*time.Minute, lease.TimeElapsed)
}

func TestReconstructExecutionLease(t *testing.T) {
	rb := RetryBudget{MaxAttempts: 5}
	lease := ReconstructExecutionLease(
		testLeaseID(), testWorkflowRunID(),
		10*time.Minute, rb, 3, 4*time.Minute,
		LeaseStatusActive, testTimestamp(),
	)

	assert.Equal(t, LeaseStatusActive, lease.Status)
	assert.Equal(t, 3, lease.AttemptsUsed)
	assert.Equal(t, 5, lease.RetryBudget.MaxAttempts)
	assert.Equal(t, 4*time.Minute, lease.TimeElapsed)
}
