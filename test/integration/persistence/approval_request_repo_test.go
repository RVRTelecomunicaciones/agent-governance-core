//go:build integration

package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveTaskAndWorkflowForApproval creates task + workflow run for FK constraints.
func saveTaskAndWorkflowForApproval(t *testing.T, db *testhelpers.TestDB, taskID shared.TaskID, wfID shared.WorkflowRunID) {
	t.Helper()
	ctx := context.Background()
	taskRepo := persistence.NewPgTaskRepository(db.Pool)
	tk := fixtures.NewTestTask(fixtures.WithTaskID(taskID))
	require.NoError(t, taskRepo.Save(ctx, tk))

	wfRepo := persistence.NewPgWorkflowRunRepository(db.Pool)
	wf := workflow.NewWorkflowRun(wfID, taskID, fixtures.Now())
	require.NoError(t, wfRepo.Save(ctx, wf))
}

func TestApprovalRequestRepo_SaveAndFindByID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgApprovalRequestRepository(db.Pool)
	ctx := context.Background()

	ar := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar.TaskID(), ar.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar))

	found, err := repo.FindByID(ctx, ar.ID())
	require.NoError(t, err)

	assert.Equal(t, ar.ID(), found.ID())
	assert.Equal(t, ar.TaskID(), found.TaskID())
	assert.Equal(t, ar.WorkflowRunID(), found.WorkflowRunID())
	assert.Equal(t, ar.Reason(), found.Reason())
	assert.Equal(t, approval.StatusPending, found.Status())
	assert.Nil(t, found.Resolution())
}

func TestApprovalRequestRepo_FindByTaskID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgApprovalRequestRepository(db.Pool)
	ctx := context.Background()

	ar1 := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar1.TaskID(), ar1.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar1))

	// Second request for same task (different workflow run)
	wf2ID, _ := shared.NewWorkflowRunID(fixtures.NewULID())
	wfRepo := persistence.NewPgWorkflowRunRepository(db.Pool)
	wf2 := workflow.NewWorkflowRun(wf2ID, ar1.TaskID(), fixtures.Now())
	require.NoError(t, wfRepo.Save(ctx, wf2))

	ar2 := fixtures.NewTestApprovalRequest(
		fixtures.WithApprovalTaskID(ar1.TaskID()),
		fixtures.WithApprovalWorkflowRunID(wf2ID),
	)
	require.NoError(t, repo.Save(ctx, ar2))

	found, err := repo.FindByTaskID(ctx, ar1.TaskID())
	require.NoError(t, err)
	assert.Len(t, found, 2)
}

func TestApprovalRequestRepo_FindPending(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgApprovalRequestRepository(db.Pool)
	ctx := context.Background()

	// Create pending request
	ar1 := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar1.TaskID(), ar1.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar1))

	// Create and approve another request
	ar2 := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar2.TaskID(), ar2.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar2))

	actor, _ := shared.NewActorID("approver")
	require.NoError(t, ar2.Approve(actor, "looks good", fixtures.Now()))
	require.NoError(t, repo.Update(ctx, ar2))

	pending, err := repo.FindPending(ctx)
	require.NoError(t, err)

	// At least our pending one should be there
	var foundPending bool
	for _, p := range pending {
		if p.ID() == ar1.ID() {
			foundPending = true
		}
		assert.Equal(t, approval.StatusPending, p.Status())
	}
	assert.True(t, foundPending, "expected pending request not found")
}

func TestApprovalRequestRepo_UpdateAfterResolution(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgApprovalRequestRepository(db.Pool)
	ctx := context.Background()

	ar := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar.TaskID(), ar.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar))

	actor, _ := shared.NewActorID("approver")
	require.NoError(t, ar.Approve(actor, "approved for testing", fixtures.Now()))
	require.NoError(t, repo.Update(ctx, ar))

	found, err := repo.FindByID(ctx, ar.ID())
	require.NoError(t, err)
	assert.Equal(t, approval.StatusApproved, found.Status())

	res := found.Resolution()
	require.NotNil(t, res)
	assert.Equal(t, actor, res.ResolvedBy)
	assert.Equal(t, "approved for testing", res.Reason)
	assert.Equal(t, approval.StatusApproved, res.Status)
	assert.False(t, res.ResolvedAt.Time.IsZero())
}

func TestApprovalRequestRepo_ApproverSpecJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgApprovalRequestRepository(db.Pool)
	ctx := context.Background()

	ar := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar.TaskID(), ar.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar))

	found, err := repo.FindByID(ctx, ar.ID())
	require.NoError(t, err)
	assert.Equal(t, "human", found.RequiredApprover().Role)
}

func TestApprovalRequestRepo_NullableExpiresAt(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgApprovalRequestRepository(db.Pool)
	ctx := context.Background()

	// Without expiration
	ar := fixtures.NewTestApprovalRequest()
	saveTaskAndWorkflowForApproval(t, db, ar.TaskID(), ar.WorkflowRunID())
	require.NoError(t, repo.Save(ctx, ar))

	found, err := repo.FindByID(ctx, ar.ID())
	require.NoError(t, err)
	assert.Nil(t, found.ExpiresAt())

	// With expiration - use Reconstruct to set expiresAt
	expiry := shared.MustTimestamp(time.Now().Add(1 * time.Hour))
	ar2ID, _ := shared.NewApprovalRequestID(fixtures.NewULID())
	ar2TaskID := ar.TaskID() // reuse same task
	ar2WfID, _ := shared.NewWorkflowRunID(fixtures.NewULID())

	wfRepo := persistence.NewPgWorkflowRunRepository(db.Pool)
	wf2 := workflow.NewWorkflowRun(ar2WfID, ar2TaskID, fixtures.Now())
	require.NoError(t, wfRepo.Save(ctx, wf2))

	ar2 := approval.ReconstructApprovalRequest(
		ar2ID, ar2TaskID, ar2WfID,
		"test with expiry", approval.ApproverSpec{Role: "admin"},
		approval.StatusPending, nil, &expiry, fixtures.Now(),
	)
	require.NoError(t, repo.Save(ctx, ar2))

	found2, err := repo.FindByID(ctx, ar2ID)
	require.NoError(t, err)
	require.NotNil(t, found2.ExpiresAt())
	assert.WithinDuration(t, expiry.Time, found2.ExpiresAt().Time, time.Second)
}
