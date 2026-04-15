//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/russellcxl/agent-governance-core/test/fixtures"
	"github.com/russellcxl/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check: PgAuditEntryRepository only has Append + Query (no Update/Delete).
var _ outbound.AuditEntryRepository = (*persistence.PgAuditEntryRepository)(nil)

func TestAuditEntryRepo_Append(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	entry := fixtures.NewTestAuditEntry()
	require.NoError(t, repo.Append(ctx, entry))

	// Verify it was saved by querying
	actor := entry.Actor()
	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Actor:  &actor,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)

	var found bool
	for _, r := range results {
		if r.ID() == entry.ID() {
			found = true
			assert.Equal(t, entry.Actor(), r.Actor())
			assert.Equal(t, entry.Action(), r.Action())
			assert.Equal(t, entry.Outcome(), r.Outcome())
		}
	}
	assert.True(t, found, "appended entry not found")
}

func TestAuditEntryRepo_QueryByTaskID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	// Save a task for FK
	tk := fixtures.NewTestTask()
	taskRepo := persistence.NewPgTaskRepository(db.Pool)
	require.NoError(t, taskRepo.Save(ctx, tk))

	taskID := tk.ID()
	entry := fixtures.NewTestAuditEntry(fixtures.WithAuditTaskID(taskID))
	require.NoError(t, repo.Append(ctx, entry))

	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		TaskID: &taskID,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, results)
	assert.Equal(t, taskID, *results[0].TaskID())
}

func TestAuditEntryRepo_QueryByActor(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	actor, _ := shared.NewActorID("unique-actor-filter-test")
	entry := fixtures.NewTestAuditEntry(fixtures.WithAuditActor(actor))
	require.NoError(t, repo.Append(ctx, entry))

	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Actor:  &actor,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
}

func TestAuditEntryRepo_QueryByAction(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	action := "unique_action_filter_test"
	entry := fixtures.NewTestAuditEntry(fixtures.WithAuditAction(action))
	require.NoError(t, repo.Append(ctx, entry))

	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action: &action,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, results)
	assert.Equal(t, action, results[0].Action())
}

func TestAuditEntryRepo_QueryPagination(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	actor, _ := shared.NewActorID("pagination-test-actor")
	for i := 0; i < 5; i++ {
		entry := fixtures.NewTestAuditEntry(fixtures.WithAuditActor(actor))
		require.NoError(t, repo.Append(ctx, entry))
	}

	// First page
	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Actor:  &actor,
		Limit:  2,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, results, 2)

	// Second page
	results2, total2, err := repo.Query(ctx, outbound.AuditFilter{
		Actor:  &actor,
		Limit:  2,
		Offset: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, total2)
	assert.Len(t, results2, 2)
}

func TestAuditEntryRepo_ContextJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	auditCtx := audit.NewAuditContext().
		WithFailure("routing", "ERR_001", true).
		WithToolName("code_review").
		WithStrategy("direct")

	entry := fixtures.NewTestAuditEntry(fixtures.WithAuditContext(auditCtx))
	require.NoError(t, repo.Append(ctx, entry))

	actor := entry.Actor()
	results, _, err := repo.Query(ctx, outbound.AuditFilter{
		Actor:  &actor,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)

	var found *audit.AuditEntry
	for _, r := range results {
		if r.ID() == entry.ID() {
			found = r
			break
		}
	}
	require.NotNil(t, found)

	c := found.Context()
	assert.Equal(t, "routing", c["failure_stage"])
	assert.Equal(t, "ERR_001", c["failure_code"])
	assert.Equal(t, true, c["retryable"])
	assert.Equal(t, "code_review", c["tool_name"])
	assert.Equal(t, "direct", c["strategy_used"])
}

func TestAuditEntryRepo_NullableTaskAndWorkflowRunIDs(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	ctx := context.Background()

	// System-level entry without task/workflow references
	entry := fixtures.NewTestAuditEntry()
	require.NoError(t, repo.Append(ctx, entry))

	actor := entry.Actor()
	results, _, err := repo.Query(ctx, outbound.AuditFilter{
		Actor:  &actor,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)

	var found *audit.AuditEntry
	for _, r := range results {
		if r.ID() == entry.ID() {
			found = r
			break
		}
	}
	require.NotNil(t, found)
	assert.Nil(t, found.TaskID())
	assert.Nil(t, found.WorkflowRunID())
}
