package approval_test

import (
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validApprovalRequest(t *testing.T) *approval.ApprovalRequest {
	t.Helper()
	id, _ := shared.NewApprovalRequestID("01JSGX0000000000000000APRV")
	taskID, _ := shared.NewTaskID("01JSGX0000000000000000TASK")
	wfID, _ := shared.NewWorkflowRunID("01JSGX0000000000000000WFLO")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))

	approver := approval.ApproverSpec{Role: "admin"}
	ar, err := approval.NewApprovalRequest(id, taskID, wfID, "needs review", approver, nil, now)
	require.NoError(t, err)
	return ar
}

func TestNewApprovalRequest_ValidFields(t *testing.T) {
	ar := validApprovalRequest(t)
	assert.Equal(t, approval.StatusPending, ar.Status())
	assert.Equal(t, "needs review", ar.Reason())
	assert.Nil(t, ar.Resolution())
	assert.Nil(t, ar.ExpiresAt())
}

func TestNewApprovalRequest_EmptyReasonFails(t *testing.T) {
	id, _ := shared.NewApprovalRequestID("01JSGX0000000000000000APRV")
	taskID, _ := shared.NewTaskID("01JSGX0000000000000000TASK")
	wfID, _ := shared.NewWorkflowRunID("01JSGX0000000000000000WFLO")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))

	_, err := approval.NewApprovalRequest(id, taskID, wfID, "", approval.ApproverSpec{Role: "admin"}, nil, now)
	require.ErrorIs(t, err, approval.ErrEmptyReason)

	_, err = approval.NewApprovalRequest(id, taskID, wfID, "   ", approval.ApproverSpec{Role: "admin"}, nil, now)
	require.ErrorIs(t, err, approval.ErrEmptyReason)
}

func TestApproveFromPending(t *testing.T) {
	ar := validApprovalRequest(t)
	actor, _ := shared.NewActorID("admin-user")
	resolveTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))

	err := ar.Approve(actor, "looks good", resolveTime)
	require.NoError(t, err)

	assert.Equal(t, approval.StatusApproved, ar.Status())
	require.NotNil(t, ar.Resolution())
	assert.Equal(t, actor, ar.Resolution().ResolvedBy)
	assert.Equal(t, resolveTime, ar.Resolution().ResolvedAt)
	assert.Equal(t, "looks good", ar.Resolution().Reason)
	assert.Equal(t, approval.StatusApproved, ar.Resolution().Status)
}

func TestDenyFromPending(t *testing.T) {
	ar := validApprovalRequest(t)
	actor, _ := shared.NewActorID("admin-user")
	resolveTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))

	err := ar.Deny(actor, "not ready", resolveTime)
	require.NoError(t, err)

	assert.Equal(t, approval.StatusDenied, ar.Status())
	require.NotNil(t, ar.Resolution())
	assert.Equal(t, approval.StatusDenied, ar.Resolution().Status)
}

func TestExpireFromPending(t *testing.T) {
	ar := validApprovalRequest(t)
	resolveTime := shared.MustTimestamp(time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC))

	err := ar.Expire(resolveTime)
	require.NoError(t, err)

	assert.Equal(t, approval.StatusExpired, ar.Status())
	require.NotNil(t, ar.Resolution())
	assert.Equal(t, shared.ActorID("system"), ar.Resolution().ResolvedBy)
}

func TestDoubleResolutionFails(t *testing.T) {
	ar := validApprovalRequest(t)
	actor, _ := shared.NewActorID("admin-user")
	resolveTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))

	err := ar.Approve(actor, "approved", resolveTime)
	require.NoError(t, err)

	err = ar.Deny(actor, "deny after approve", resolveTime)
	require.ErrorIs(t, err, approval.ErrNotPending)
}

func TestApproveFromApprovedFails(t *testing.T) {
	ar := validApprovalRequest(t)
	actor, _ := shared.NewActorID("admin-user")
	resolveTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))

	err := ar.Approve(actor, "first", resolveTime)
	require.NoError(t, err)

	err = ar.Approve(actor, "second", resolveTime)
	require.ErrorIs(t, err, approval.ErrNotPending)
}

func TestApprovalStatusEnum(t *testing.T) {
	_, err := approval.NewApprovalStatus("pending")
	require.NoError(t, err)

	_, err = approval.NewApprovalStatus("invalid")
	require.Error(t, err)

	assert.True(t, approval.StatusApproved.IsTerminal())
	assert.True(t, approval.StatusDenied.IsTerminal())
	assert.True(t, approval.StatusExpired.IsTerminal())
	assert.False(t, approval.StatusPending.IsTerminal())
}
