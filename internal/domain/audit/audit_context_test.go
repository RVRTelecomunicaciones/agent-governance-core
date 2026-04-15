package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AuditContext builder tests ---

func TestNewAuditContext(t *testing.T) {
	ac := audit.NewAuditContext()
	assert.NotNil(t, ac)
	assert.Empty(t, ac)
}

func TestWithFailure(t *testing.T) {
	ac := audit.NewAuditContext().WithFailure("routing", "TIMEOUT", true)
	assert.Equal(t, "routing", ac["failure_stage"])
	assert.Equal(t, "TIMEOUT", ac["failure_code"])
	assert.Equal(t, true, ac["retryable"])
}

func TestWithStrategy(t *testing.T) {
	ac := audit.NewAuditContext().WithStrategy("round_robin")
	assert.Equal(t, "round_robin", ac["strategy_used"])
}

func TestWithAgentRole(t *testing.T) {
	ac := audit.NewAuditContext().WithAgentRole("orchestrator")
	assert.Equal(t, "orchestrator", ac["agent_role"])
}

func TestWithLeaseState(t *testing.T) {
	ac := audit.NewAuditContext().WithLeaseState(3, 5, 12345)
	assert.Equal(t, 3, ac["attempts_used"])
	assert.Equal(t, 5, ac["retry_budget"])
	assert.Equal(t, int64(12345), ac["time_elapsed_ms"])
}

func TestWithToolName(t *testing.T) {
	ac := audit.NewAuditContext().WithToolName("code_search")
	assert.Equal(t, "code_search", ac["tool_name"])
}

func TestSet(t *testing.T) {
	ac := audit.NewAuditContext().Set("custom_key", "custom_value")
	assert.Equal(t, "custom_value", ac["custom_key"])
}

func TestWithTraceInfo_NoSpan(t *testing.T) {
	// Background context has no OTel span — should not add keys.
	ac := audit.NewAuditContext().WithTraceInfo(context.Background())
	_, hasTraceID := ac["trace_id"]
	_, hasSpanID := ac["span_id"]
	assert.False(t, hasTraceID, "trace_id should not be set without a valid span")
	assert.False(t, hasSpanID, "span_id should not be set without a valid span")
}

func TestChainingMultipleBuilders(t *testing.T) {
	ac := audit.NewAuditContext().
		WithFailure("execution", "ERR_500", false).
		WithStrategy("fallback").
		WithAgentRole("worker").
		WithToolName("file_write").
		WithLeaseState(1, 3, 500).
		Set("env", "production")

	assert.Equal(t, "execution", ac["failure_stage"])
	assert.Equal(t, "ERR_500", ac["failure_code"])
	assert.Equal(t, false, ac["retryable"])
	assert.Equal(t, "fallback", ac["strategy_used"])
	assert.Equal(t, "worker", ac["agent_role"])
	assert.Equal(t, "file_write", ac["tool_name"])
	assert.Equal(t, 1, ac["attempts_used"])
	assert.Equal(t, 3, ac["retry_budget"])
	assert.Equal(t, int64(500), ac["time_elapsed_ms"])
	assert.Equal(t, "production", ac["env"])
}

// --- AuditEntry tests ---

func TestNewAuditEntry_ValidFields(t *testing.T) {
	id, _ := shared.NewAuditEntryID("01JSGX0000000000000000AUDT")
	actor, _ := shared.NewActorID("agent-1")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))
	ctx := audit.NewAuditContext().WithStrategy("direct")

	entry, err := audit.NewAuditEntry(id, actor, "task.created", "success", ctx, now)
	require.NoError(t, err)

	assert.Equal(t, id, entry.ID())
	assert.Equal(t, actor, entry.Actor())
	assert.Equal(t, "task.created", entry.Action())
	assert.Equal(t, "success", entry.Outcome())
	assert.Equal(t, "direct", entry.Context()["strategy_used"])
	assert.Nil(t, entry.TaskID())
	assert.Nil(t, entry.WorkflowRunID())
}

func TestNewAuditEntry_EmptyActionFails(t *testing.T) {
	id, _ := shared.NewAuditEntryID("01JSGX0000000000000000AUDT")
	actor, _ := shared.NewActorID("agent-1")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))

	_, err := audit.NewAuditEntry(id, actor, "", "success", nil, now)
	require.ErrorIs(t, err, audit.ErrEmptyAction)

	_, err = audit.NewAuditEntry(id, actor, "   ", "success", nil, now)
	require.ErrorIs(t, err, audit.ErrEmptyAction)
}

func TestNewAuditEntry_EmptyOutcomeFails(t *testing.T) {
	id, _ := shared.NewAuditEntryID("01JSGX0000000000000000AUDT")
	actor, _ := shared.NewActorID("agent-1")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))

	_, err := audit.NewAuditEntry(id, actor, "task.created", "", nil, now)
	require.ErrorIs(t, err, audit.ErrEmptyOutcome)
}

func TestWithTaskID(t *testing.T) {
	id, _ := shared.NewAuditEntryID("01JSGX0000000000000000AUDT")
	actor, _ := shared.NewActorID("agent-1")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))
	taskID, _ := shared.NewTaskID("01JSGX0000000000000000TASK")

	entry, err := audit.NewAuditEntry(id, actor, "action", "outcome", nil, now)
	require.NoError(t, err)

	entry.WithTaskID(taskID)
	require.NotNil(t, entry.TaskID())
	assert.Equal(t, taskID, *entry.TaskID())
}

func TestWithWorkflowRunID(t *testing.T) {
	id, _ := shared.NewAuditEntryID("01JSGX0000000000000000AUDT")
	actor, _ := shared.NewActorID("agent-1")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))
	wfID, _ := shared.NewWorkflowRunID("01JSGX0000000000000000WFLO")

	entry, err := audit.NewAuditEntry(id, actor, "action", "outcome", nil, now)
	require.NoError(t, err)

	entry.WithWorkflowRunID(wfID)
	require.NotNil(t, entry.WorkflowRunID())
	assert.Equal(t, wfID, *entry.WorkflowRunID())
}
