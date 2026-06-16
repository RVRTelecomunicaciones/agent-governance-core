//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/escalation"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscalationTriggerRepo_SaveAndFindByTaskID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgEscalationTriggerRepository(db.Pool)
	ctx := context.Background()

	trigger := fixtures.NewTestEscalationTrigger()
	saveTaskForFK(t, db, trigger.TaskID())
	require.NoError(t, repo.Save(ctx, trigger))

	found, err := repo.FindByTaskID(ctx, trigger.TaskID())
	require.NoError(t, err)
	require.Len(t, found, 1)

	assert.Equal(t, trigger.ID(), found[0].ID())
	assert.Equal(t, trigger.TaskID(), found[0].TaskID())
	assert.Equal(t, trigger.Target(), found[0].Target())
	assert.Equal(t, escalation.StatusPending, found[0].Status())
	assert.Nil(t, found[0].TriggeredAt())
}

func TestEscalationTriggerRepo_ConditionJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgEscalationTriggerRepository(db.Pool)
	ctx := context.Background()

	cond := escalation.EscalationCondition{
		Type:       "timeout_exceeded",
		Parameters: map[string]any{"threshold_ms": float64(60000)},
	}
	trigger := fixtures.NewTestEscalationTrigger(fixtures.WithEscalationCondition(cond))
	saveTaskForFK(t, db, trigger.TaskID())
	require.NoError(t, repo.Save(ctx, trigger))

	found, err := repo.FindByTaskID(ctx, trigger.TaskID())
	require.NoError(t, err)
	require.Len(t, found, 1)

	assert.Equal(t, "timeout_exceeded", found[0].Condition().Type)
	assert.Equal(t, float64(60000), found[0].Condition().Parameters["threshold_ms"])
}

func TestEscalationTriggerRepo_UpdateWithTriggeredAt(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgEscalationTriggerRepository(db.Pool)
	ctx := context.Background()

	trigger := fixtures.NewTestEscalationTrigger()
	saveTaskForFK(t, db, trigger.TaskID())
	require.NoError(t, repo.Save(ctx, trigger))

	require.NoError(t, trigger.Trigger(fixtures.Now()))
	require.NoError(t, repo.Update(ctx, trigger))

	found, err := repo.FindByTaskID(ctx, trigger.TaskID())
	require.NoError(t, err)
	require.Len(t, found, 1)

	assert.Equal(t, escalation.StatusTriggered, found[0].Status())
	require.NotNil(t, found[0].TriggeredAt())
	assert.False(t, found[0].TriggeredAt().Time.IsZero())
}

func TestEscalationTriggerRepo_NullableTriggeredAt(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgEscalationTriggerRepository(db.Pool)
	ctx := context.Background()

	trigger := fixtures.NewTestEscalationTrigger()
	saveTaskForFK(t, db, trigger.TaskID())
	require.NoError(t, repo.Save(ctx, trigger))

	found, err := repo.FindByTaskID(ctx, trigger.TaskID())
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Nil(t, found[0].TriggeredAt())
}
