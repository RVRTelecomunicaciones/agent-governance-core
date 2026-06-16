//go:build integration

package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveTaskAndWorkflowForFK creates a task and workflow run so FK constraints are met.
func saveTaskAndWorkflowForFK(t *testing.T, db *testhelpers.TestDB, wfID shared.WorkflowRunID) shared.TaskID {
	t.Helper()
	ctx := context.Background()
	tk := fixtures.NewTestTask()
	taskRepo := persistence.NewPgTaskRepository(db.Pool)
	require.NoError(t, taskRepo.Save(ctx, tk))

	wfRepo := persistence.NewPgWorkflowRunRepository(db.Pool)
	wf := workflow.NewWorkflowRun(wfID, tk.ID(), fixtures.Now())
	require.NoError(t, wfRepo.Save(ctx, wf))

	return tk.ID()
}

func TestExecutionLeaseRepo_SaveAndFindByWorkflowRunID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgExecutionLeaseRepository(db.Pool)
	ctx := context.Background()

	lease := fixtures.NewTestExecutionLease()
	saveTaskAndWorkflowForFK(t, db, lease.WorkflowRunID)

	require.NoError(t, repo.Save(ctx, lease))

	found, err := repo.FindByWorkflowRunID(ctx, lease.WorkflowRunID)
	require.NoError(t, err)

	assert.Equal(t, lease.ID, found.ID)
	assert.Equal(t, lease.WorkflowRunID, found.WorkflowRunID)
	assert.Equal(t, lease.Status, found.Status)
	assert.Equal(t, lease.AttemptsUsed, found.AttemptsUsed)
	assert.Equal(t, lease.RetryBudget.MaxAttempts, found.RetryBudget.MaxAttempts)
}

func TestExecutionLeaseRepo_DurationAsMilliseconds(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgExecutionLeaseRepository(db.Pool)
	ctx := context.Background()

	timeout := 5 * time.Minute
	lease := fixtures.NewTestExecutionLease(fixtures.WithTimeoutBudget(timeout))
	saveTaskAndWorkflowForFK(t, db, lease.WorkflowRunID)

	require.NoError(t, repo.Save(ctx, lease))

	found, err := repo.FindByWorkflowRunID(ctx, lease.WorkflowRunID)
	require.NoError(t, err)

	assert.Equal(t, timeout, found.TimeoutBudget)
	assert.Equal(t, time.Duration(0), found.TimeElapsed)
}

func TestExecutionLeaseRepo_UpdateAttemptsUsed(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgExecutionLeaseRepository(db.Pool)
	ctx := context.Background()

	lease := fixtures.NewTestExecutionLease()
	saveTaskAndWorkflowForFK(t, db, lease.WorkflowRunID)
	require.NoError(t, repo.Save(ctx, lease))

	lease.AttemptsUsed = 2
	lease.TimeElapsed = 30 * time.Second
	require.NoError(t, repo.Update(ctx, lease))

	found, err := repo.FindByWorkflowRunID(ctx, lease.WorkflowRunID)
	require.NoError(t, err)
	assert.Equal(t, 2, found.AttemptsUsed)
	assert.Equal(t, 30*time.Second, found.TimeElapsed)
}

func TestExecutionLeaseRepo_LeaseStatusRoundtrip(t *testing.T) {
	statuses := []execution.LeaseStatus{
		execution.LeaseStatusActive,
		execution.LeaseStatusExhausted,
		execution.LeaseStatusRevoked,
	}

	db := testhelpers.NewTestDB(t)
	ctx := context.Background()

	for _, s := range statuses {
		t.Run(string(s), func(t *testing.T) {
			repo := persistence.NewPgExecutionLeaseRepository(db.Pool)
			lease := fixtures.NewTestExecutionLease()
			saveTaskAndWorkflowForFK(t, db, lease.WorkflowRunID)

			// Force status via reconstruct
			lease = execution.ReconstructExecutionLease(
				lease.ID, lease.WorkflowRunID, lease.TimeoutBudget,
				lease.RetryBudget, lease.AttemptsUsed, lease.TimeElapsed,
				s, lease.CreatedAt,
			)
			require.NoError(t, repo.Save(ctx, lease))

			found, err := repo.FindByWorkflowRunID(ctx, lease.WorkflowRunID)
			require.NoError(t, err)
			assert.Equal(t, s, found.Status)
		})
	}
}
