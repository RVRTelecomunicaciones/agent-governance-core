//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveTaskForFK saves a task fixture so FK constraints are met.
func saveTaskForFK(t *testing.T, db *testhelpers.TestDB, taskID shared.TaskID) {
	t.Helper()
	taskRepo := persistence.NewPgTaskRepository(db.Pool)
	tk := fixtures.NewTestTask(fixtures.WithTaskID(taskID))
	require.NoError(t, taskRepo.Save(context.Background(), tk))
}

// saveRunningWorkflowDeps creates task + routing decision + policy decision so a
// running workflow with non-nil FK references can be saved.
func saveRunningWorkflowDeps(t *testing.T, db *testhelpers.TestDB, wf *workflow.WorkflowRun) {
	t.Helper()
	ctx := context.Background()

	// Save task
	saveTaskForFK(t, db, wf.TaskID)

	// Save routing decision if present
	if wf.RoutingDecisionID != nil {
		rdRepo := persistence.NewPgRoutingDecisionRepository(db.Pool)
		rd := fixtures.NewTestRoutingDecision(
			fixtures.WithRoutingDecisionID(*wf.RoutingDecisionID),
			fixtures.WithRoutingTaskID(wf.TaskID),
		)
		require.NoError(t, rdRepo.Save(ctx, rd))
	}

	// Save policy decision if present
	if wf.PolicyDecisionID != nil {
		pdRepo := persistence.NewPgPolicyDecisionRepository(db.Pool)
		pd := fixtures.NewTestPolicyDecision(
			fixtures.WithPolicyDecisionID(*wf.PolicyDecisionID),
			fixtures.WithPolicyTaskID(wf.TaskID),
		)
		require.NoError(t, pdRepo.Save(ctx, pd))
	}
}

func TestWorkflowRunRepo_SaveAndFindByID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgWorkflowRunRepository(db.Pool)
	ctx := context.Background()

	wf := fixtures.NewTestWorkflowRun()
	saveTaskForFK(t, db, wf.TaskID)

	require.NoError(t, repo.Save(ctx, wf))

	found, err := repo.FindByID(ctx, wf.ID)
	require.NoError(t, err)

	assert.Equal(t, wf.ID, found.ID)
	assert.Equal(t, wf.TaskID, found.TaskID)
	assert.Equal(t, wf.Status, found.Status)
	assert.Equal(t, wf.CurrentStepIndex, found.CurrentStepIndex)
	assert.Nil(t, found.RoutingDecisionID)
	assert.Nil(t, found.PolicyDecisionID)
}

func TestWorkflowRunRepo_FindByTaskID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgWorkflowRunRepository(db.Pool)
	ctx := context.Background()

	wf := fixtures.NewTestWorkflowRun()
	saveTaskForFK(t, db, wf.TaskID)
	require.NoError(t, repo.Save(ctx, wf))

	found, err := repo.FindByTaskID(ctx, wf.TaskID)
	require.NoError(t, err)
	assert.Equal(t, wf.ID, found.ID)
}

func TestWorkflowRunRepo_TransitionsJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgWorkflowRunRepository(db.Pool)
	ctx := context.Background()

	wf := fixtures.NewRunningWorkflowRun()
	saveRunningWorkflowDeps(t, db, wf)
	require.NoError(t, repo.Save(ctx, wf))

	found, err := repo.FindByID(ctx, wf.ID)
	require.NoError(t, err)

	// NewRunningWorkflowRun creates transitions: created->routed, routed->policy_checked, policy_checked->running
	require.Len(t, found.Transitions, 3)

	assert.Equal(t, workflow.StatusCreated, found.Transitions[0].From)
	assert.Equal(t, workflow.StatusRouted, found.Transitions[0].To)

	assert.Equal(t, workflow.StatusRouted, found.Transitions[1].From)
	assert.Equal(t, workflow.StatusPolicyChecked, found.Transitions[1].To)

	assert.Equal(t, workflow.StatusPolicyChecked, found.Transitions[2].From)
	assert.Equal(t, workflow.StatusRunning, found.Transitions[2].To)

	// Verify actor and reason survive
	for _, tr := range found.Transitions {
		assert.NotEmpty(t, string(tr.Actor))
		assert.False(t, tr.Timestamp.Time.IsZero())
	}
}

func TestWorkflowRunRepo_UpdateWithNewTransitions(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgWorkflowRunRepository(db.Pool)
	ctx := context.Background()

	wf := fixtures.NewRunningWorkflowRun()
	saveRunningWorkflowDeps(t, db, wf)
	require.NoError(t, repo.Save(ctx, wf))

	actor, _ := shared.NewActorID("test-system")
	ts := shared.MustTimestamp(wf.CreatedAt.Time.Add(1))
	require.NoError(t, wf.Complete("done", actor, ts))
	require.NoError(t, repo.Update(ctx, wf))

	found, err := repo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusCompleted, found.Status)
	assert.Len(t, found.Transitions, 4) // 3 original + 1 new
}

func TestWorkflowRunRepo_NullableRoutingAndPolicyIDs(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgWorkflowRunRepository(db.Pool)
	ctx := context.Background()

	// Created status has nil routing/policy IDs
	wf := fixtures.NewTestWorkflowRun()
	saveTaskForFK(t, db, wf.TaskID)
	require.NoError(t, repo.Save(ctx, wf))

	found, err := repo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Nil(t, found.RoutingDecisionID)
	assert.Nil(t, found.PolicyDecisionID)

	// Running status has both set
	wf2 := fixtures.NewRunningWorkflowRun()
	saveRunningWorkflowDeps(t, db, wf2)
	require.NoError(t, repo.Save(ctx, wf2))

	found2, err := repo.FindByID(ctx, wf2.ID)
	require.NoError(t, err)
	require.NotNil(t, found2.RoutingDecisionID)
	require.NotNil(t, found2.PolicyDecisionID)
}

func TestWorkflowRunRepo_AllStatusesRoundtrip(t *testing.T) {
	statuses := []workflow.WorkflowStatus{
		workflow.StatusCreated,
		workflow.StatusRouted,
		workflow.StatusPolicyChecked,
		workflow.StatusRunning,
		workflow.StatusAwaitingApproval,
		workflow.StatusApproved,
		workflow.StatusPaused,
		workflow.StatusCompleted,
		workflow.StatusFailed,
		workflow.StatusKilled,
	}

	db := testhelpers.NewTestDB(t)
	ctx := context.Background()

	for _, s := range statuses {
		t.Run(string(s), func(t *testing.T) {
			repo := persistence.NewPgWorkflowRunRepository(db.Pool)
			wf := fixtures.NewTestWorkflowRun()
			saveTaskForFK(t, db, wf.TaskID)

			// Force status via reconstruct to test all enum values
			wf = workflow.ReconstructWorkflowRun(
				wf.ID, wf.TaskID, s, nil, nil, 0, nil, wf.CreatedAt, wf.UpdatedAt,
			)
			require.NoError(t, repo.Save(ctx, wf))

			found, err := repo.FindByID(ctx, wf.ID)
			require.NoError(t, err)
			assert.Equal(t, s, found.Status)
		})
	}
}
