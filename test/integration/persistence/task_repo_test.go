//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskRepo_SaveAndFindByID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	tk := fixtures.NewTestTask()
	err := repo.Save(ctx, tk)
	require.NoError(t, err)

	found, err := repo.FindByID(ctx, tk.ID())
	require.NoError(t, err)
	require.NotNil(t, found)

	assert.Equal(t, tk.ID(), found.ID())
	assert.Equal(t, tk.Type(), found.Type())
	assert.Equal(t, tk.Title(), found.Title())
	assert.Equal(t, tk.Scope(), found.Scope())
	assert.Equal(t, tk.Priority(), found.Priority())
	assert.Equal(t, tk.RiskLevel(), found.RiskLevel())
	assert.Equal(t, tk.Status(), found.Status())
	assert.Nil(t, found.ParentTaskID())
}

func TestTaskRepo_SaveWithParentID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	parent := fixtures.NewTestTask()
	require.NoError(t, repo.Save(ctx, parent))

	child := fixtures.NewTestTask(fixtures.WithTaskParent(parent.ID()))
	require.NoError(t, repo.Save(ctx, child))

	found, err := repo.FindByID(ctx, child.ID())
	require.NoError(t, err)
	require.NotNil(t, found.ParentTaskID())
	assert.Equal(t, parent.ID(), *found.ParentTaskID())
}

func TestTaskRepo_FindByParentID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	parent := fixtures.NewTestTask()
	require.NoError(t, repo.Save(ctx, parent))

	child1 := fixtures.NewTestTask(fixtures.WithTaskParent(parent.ID()))
	child2 := fixtures.NewTestTask(fixtures.WithTaskParent(parent.ID()))
	require.NoError(t, repo.Save(ctx, child1))
	require.NoError(t, repo.Save(ctx, child2))

	children, err := repo.FindByParentID(ctx, parent.ID())
	require.NoError(t, err)
	assert.Len(t, children, 2)
}

func TestTaskRepo_UpdateStatus(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	tk := fixtures.NewTestTask()
	require.NoError(t, repo.Save(ctx, tk))

	ts := shared.MustTimestamp(tk.CreatedAt().Time.Add(1))
	require.NoError(t, tk.Accept(ts))
	require.NoError(t, repo.UpdateStatus(ctx, tk))

	found, err := repo.FindByID(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, task.StatusAccepted, found.Status())
}

func TestTaskRepo_MetadataJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	meta := task.TaskMetadata{"repo": "test", "count": float64(42)}
	tk := fixtures.NewTestTask(fixtures.WithTaskMetadata(meta))
	require.NoError(t, repo.Save(ctx, tk))

	found, err := repo.FindByID(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, "test", found.Metadata()["repo"])
	assert.Equal(t, float64(42), found.Metadata()["count"])
}

func TestTaskRepo_FindByID_NotFound(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	_, err := repo.FindByID(ctx, shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"))
	assert.Error(t, err)
}

func TestTaskRepo_ULID_Roundtrip(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	tk := fixtures.NewTestTask()
	require.NoError(t, repo.Save(ctx, tk))

	found, err := repo.FindByID(ctx, tk.ID())
	require.NoError(t, err)
	// ULID is 26 chars, stored as VARCHAR(26)
	assert.Len(t, string(found.ID()), 26)
	assert.Equal(t, tk.ID(), found.ID())
}

func TestTaskRepo_EnumRoundtrip(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgTaskRepository(db.Pool)
	ctx := context.Background()

	tk := fixtures.NewTestTask(
		fixtures.WithTaskPriority(shared.PriorityUrgent),
		fixtures.WithTaskRiskLevel(shared.RiskLevelCritical),
	)
	require.NoError(t, repo.Save(ctx, tk))

	found, err := repo.FindByID(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, shared.PriorityUrgent, found.Priority())
	assert.Equal(t, shared.RiskLevelCritical, found.RiskLevel())
}
