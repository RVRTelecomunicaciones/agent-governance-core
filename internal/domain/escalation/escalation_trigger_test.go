package escalation_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validTrigger(t *testing.T) *escalation.EscalationTrigger {
	t.Helper()
	id, _ := shared.NewEscalationTriggerID("01JSGX0000000000000000ESCT")
	taskID, _ := shared.NewTaskID("01JSGX0000000000000000TASK")
	now := shared.MustTimestamp(time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC))

	condition := escalation.EscalationCondition{
		Type:       "timeout_exceeded",
		Parameters: map[string]any{"threshold_ms": 5000},
	}
	return escalation.NewEscalationTrigger(id, taskID, condition, escalation.TargetHuman, now)
}

func TestCreateAndTrigger(t *testing.T) {
	et := validTrigger(t)
	assert.Equal(t, escalation.StatusPending, et.Status())
	assert.Nil(t, et.TriggeredAt())

	triggerTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))
	err := et.Trigger(triggerTime)
	require.NoError(t, err)

	assert.Equal(t, escalation.StatusTriggered, et.Status())
	require.NotNil(t, et.TriggeredAt())
	assert.Equal(t, triggerTime, *et.TriggeredAt())
}

func TestDoubleTriggerFails(t *testing.T) {
	et := validTrigger(t)
	triggerTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))

	err := et.Trigger(triggerTime)
	require.NoError(t, err)

	err = et.Trigger(triggerTime)
	require.ErrorIs(t, err, escalation.ErrNotPending)
}

func TestResolveAfterTrigger(t *testing.T) {
	et := validTrigger(t)
	triggerTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 0, 0, 0, time.UTC))

	err := et.Trigger(triggerTime)
	require.NoError(t, err)

	err = et.Resolve()
	require.NoError(t, err)
	assert.Equal(t, escalation.StatusResolved, et.Status())
}

func TestResolveWithoutTriggerFails(t *testing.T) {
	et := validTrigger(t)

	err := et.Resolve()
	require.ErrorIs(t, err, escalation.ErrNotTriggered)
}

func TestTriggeredAtSetOnTrigger(t *testing.T) {
	et := validTrigger(t)
	assert.Nil(t, et.TriggeredAt())

	triggerTime := shared.MustTimestamp(time.Date(2026, 4, 14, 11, 30, 0, 0, time.UTC))
	err := et.Trigger(triggerTime)
	require.NoError(t, err)
	require.NotNil(t, et.TriggeredAt())
	assert.Equal(t, triggerTime, *et.TriggeredAt())
}

func TestEscalationTargetEnum(t *testing.T) {
	_, err := escalation.NewEscalationTarget("human")
	require.NoError(t, err)

	_, err = escalation.NewEscalationTarget("senior_agent")
	require.NoError(t, err)

	_, err = escalation.NewEscalationTarget("admin")
	require.NoError(t, err)

	_, err = escalation.NewEscalationTarget("invalid")
	require.Error(t, err)
}
