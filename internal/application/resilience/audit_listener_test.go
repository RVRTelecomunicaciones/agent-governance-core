package resilience

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// captureAuditRecorder is a minimal AuditRecorder that captures the last call.
type captureAuditRecorder struct {
	calls []capturedAudit
}

type capturedAudit struct {
	actor   shared.ActorID
	action  string
	outcome string
	ctx     audit.AuditContext
	taskID  *shared.TaskID
	wfID    *shared.WorkflowRunID
}

func (r *captureAuditRecorder) Record(
	_ context.Context,
	actor shared.ActorID,
	action, outcome string,
	auditCtx audit.AuditContext,
	taskID *shared.TaskID,
	wfID *shared.WorkflowRunID,
) error {
	r.calls = append(r.calls, capturedAudit{
		actor:   actor,
		action:  action,
		outcome: outcome,
		ctx:     auditCtx,
		taskID:  taskID,
		wfID:    wfID,
	})
	return nil
}

func (r *captureAuditRecorder) last() capturedAudit {
	return r.calls[len(r.calls)-1]
}

func testKey() BreakerKey {
	return BreakerKey{ToolName: "shell", AgentRole: "implementer"}
}

func TestAuditListener_ClosedToOpen_ConsecutiveTrip(t *testing.T) {
	rec := &captureAuditRecorder{}
	listener := AuditListener(rec)

	now := time.Now()
	listener(StateTransition{
		Key:                 testKey(),
		From:                StateClosed,
		To:                  StateOpen,
		TripReason:          TripReasonConsecutive,
		ConsecutiveFailures: 3,
		FailureRate:         1.0,
		SampleSize:          3,
		At:                  now,
	})

	require.Len(t, rec.calls, 1)
	got := rec.last()
	assert.Equal(t, shared.ActorID("system"), got.actor)
	assert.Equal(t, "circuit_breaker_opened", got.action)
	assert.Equal(t, string(TripReasonConsecutive), got.outcome)
	assert.Nil(t, got.taskID)
	assert.Nil(t, got.wfID)

	assert.Equal(t, "shell", got.ctx["tool_name"])
	assert.Equal(t, "implementer", got.ctx["agent_role"])
	assert.Equal(t, string(StateClosed), got.ctx["from_state"])
	assert.Equal(t, string(StateOpen), got.ctx["to_state"])
	assert.Equal(t, 3, got.ctx["consecutive_failures"])
	assert.Equal(t, 3, got.ctx["sample_size"])
	assert.Equal(t, 1.0, got.ctx["failure_rate"])
	assert.Equal(t, string(TripReasonConsecutive), got.ctx["trip_reason"])
}

func TestAuditListener_ClosedToOpen_RatioTrip(t *testing.T) {
	rec := &captureAuditRecorder{}
	listener := AuditListener(rec)

	listener(StateTransition{
		Key:                 testKey(),
		From:                StateClosed,
		To:                  StateOpen,
		TripReason:          TripReasonRatio,
		ConsecutiveFailures: 2,
		FailureRate:         0.67,
		SampleSize:          15,
		At:                  time.Now(),
	})

	require.Len(t, rec.calls, 1)
	got := rec.last()
	assert.Equal(t, "circuit_breaker_opened", got.action)
	assert.Equal(t, string(TripReasonRatio), got.outcome)
	assert.Equal(t, string(TripReasonRatio), got.ctx["trip_reason"])
}

func TestAuditListener_OpenToHalfOpen(t *testing.T) {
	rec := &captureAuditRecorder{}
	listener := AuditListener(rec)

	listener(StateTransition{
		Key:  testKey(),
		From: StateOpen,
		To:   StateHalfOpen,
		At:   time.Now(),
	})

	require.Len(t, rec.calls, 1)
	got := rec.last()
	assert.Equal(t, "circuit_breaker_half_opened", got.action)
	assert.Equal(t, "cooldown_elapsed", got.outcome)
	assert.Equal(t, string(StateOpen), got.ctx["from_state"])
	assert.Equal(t, string(StateHalfOpen), got.ctx["to_state"])
	// Should not carry trip fields
	_, hasTripReason := got.ctx["trip_reason"]
	assert.False(t, hasTripReason)
}

func TestAuditListener_HalfOpenToClosed(t *testing.T) {
	rec := &captureAuditRecorder{}
	listener := AuditListener(rec)

	listener(StateTransition{
		Key:                  testKey(),
		From:                 StateHalfOpen,
		To:                   StateClosed,
		ConsecutiveSuccesses: 3,
		At:                   time.Now(),
	})

	require.Len(t, rec.calls, 1)
	got := rec.last()
	assert.Equal(t, "circuit_breaker_closed", got.action)
	assert.Equal(t, "probes_successful", got.outcome)
	assert.Equal(t, 3, got.ctx["consecutive_successes"])
}

func TestAuditListener_HalfOpenToOpen(t *testing.T) {
	rec := &captureAuditRecorder{}
	listener := AuditListener(rec)

	listener(StateTransition{
		Key:  testKey(),
		From: StateHalfOpen,
		To:   StateOpen,
		At:   time.Now(),
	})

	require.Len(t, rec.calls, 1)
	got := rec.last()
	assert.Equal(t, "circuit_breaker_opened", got.action)
	assert.Equal(t, "probe_failed", got.outcome)
	// HALF_OPEN → OPEN is NOT a CLOSED→OPEN trip, so no trip_reason
	_, hasTripReason := got.ctx["trip_reason"]
	assert.False(t, hasTripReason)
}
