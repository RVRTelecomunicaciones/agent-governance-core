package task_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Enum Tests ---

func TestTaskType(t *testing.T) {
	validValues := []string{"development", "review", "research", "refactor", "bugfix", "deployment"}

	for _, v := range validValues {
		t.Run("accepts "+v, func(t *testing.T) {
			tt, err := task.NewTaskType(v)
			require.NoError(t, err)
			assert.Equal(t, v, tt.String())
		})
	}

	t.Run("rejects invalid value", func(t *testing.T) {
		_, err := task.NewTaskType("unknown")
		assert.Error(t, err)
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := task.NewTaskType("")
		assert.Error(t, err)
	})
}

func TestTaskScope(t *testing.T) {
	validValues := []string{"file", "module", "repo", "system"}

	for _, v := range validValues {
		t.Run("accepts "+v, func(t *testing.T) {
			ts, err := task.NewTaskScope(v)
			require.NoError(t, err)
			assert.Equal(t, v, ts.String())
		})
	}

	t.Run("rejects invalid value", func(t *testing.T) {
		_, err := task.NewTaskScope("global")
		assert.Error(t, err)
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := task.NewTaskScope("")
		assert.Error(t, err)
	})
}

func TestTaskStatus(t *testing.T) {
	validValues := []string{"created", "accepted", "in_progress", "completed", "failed", "cancelled"}

	for _, v := range validValues {
		t.Run("accepts "+v, func(t *testing.T) {
			ts, err := task.NewTaskStatus(v)
			require.NoError(t, err)
			assert.Equal(t, v, ts.String())
		})
	}

	t.Run("rejects invalid value", func(t *testing.T) {
		_, err := task.NewTaskStatus("pending")
		assert.Error(t, err)
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := task.NewTaskStatus("")
		assert.Error(t, err)
	})
}

// --- Helper ---

func validULID() string {
	return "01JRXRZ9FGADQ51K3W5JVBK5YS" // pre-generated valid ULID
}

func validULID2() string {
	return "01JRXS0AQBEHNFY9RSWRMN6PGC" // second valid ULID for parent tasks
}

func now() shared.Timestamp {
	return shared.MustTimestamp(time.Now())
}

// --- Task Creation Tests ---

func TestNewTask_ValidCreation(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	ts := now()

	tsk, err := task.NewTask(
		id,
		nil, // no parent
		task.TypeDevelopment,
		"Implement user authentication",
		task.ScopeModule,
		shared.PriorityHigh,
		shared.RiskLevelMedium,
		task.TaskMetadata{"language": "go"},
		ts,
	)
	require.NoError(t, err)

	assert.Equal(t, id, tsk.ID())
	assert.Nil(t, tsk.ParentTaskID())
	assert.Equal(t, task.TypeDevelopment, tsk.Type())
	assert.Equal(t, "Implement user authentication", tsk.Title())
	assert.Equal(t, task.ScopeModule, tsk.Scope())
	assert.Equal(t, shared.PriorityHigh, tsk.Priority())
	assert.Equal(t, shared.RiskLevelMedium, tsk.RiskLevel())
	assert.Equal(t, task.StatusCreated, tsk.Status())
	assert.Equal(t, "go", tsk.Metadata()["language"])
	assert.Equal(t, ts, tsk.CreatedAt())
	assert.Equal(t, ts, tsk.UpdatedAt())
}

func TestNewTask_WithParentTaskID(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	parentID, err := shared.NewTaskID(validULID2())
	require.NoError(t, err)

	tsk, err := task.NewTask(
		id,
		&parentID,
		task.TypeBugfix,
		"Fix login redirect",
		task.ScopeFile,
		shared.PriorityUrgent,
		shared.RiskLevelLow,
		nil,
		now(),
	)
	require.NoError(t, err)

	require.NotNil(t, tsk.ParentTaskID())
	assert.Equal(t, parentID, *tsk.ParentTaskID())
}

func TestNewTask_EmptyTitleFails(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	_, err = task.NewTask(
		id,
		nil,
		task.TypeDevelopment,
		"",
		task.ScopeModule,
		shared.PriorityNormal,
		shared.RiskLevelLow,
		nil,
		now(),
	)
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrEmptyTitle)
}

func TestNewTask_WhitespaceTitleFails(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	_, err = task.NewTask(
		id,
		nil,
		task.TypeDevelopment,
		"   ",
		task.ScopeModule,
		shared.PriorityNormal,
		shared.RiskLevelLow,
		nil,
		now(),
	)
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrEmptyTitle)
}

func TestNewTask_NilMetadataBecomesEmpty(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	tsk, err := task.NewTask(
		id,
		nil,
		task.TypeResearch,
		"Research options",
		task.ScopeRepo,
		shared.PriorityLow,
		shared.RiskLevelLow,
		nil,
		now(),
	)
	require.NoError(t, err)
	assert.NotNil(t, tsk.Metadata())
	assert.Empty(t, tsk.Metadata())
}

func TestNewTask_MetadataIsImmutable(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	meta := task.TaskMetadata{"key": "value"}
	tsk, err := task.NewTask(
		id,
		nil,
		task.TypeDevelopment,
		"Some task",
		task.ScopeFile,
		shared.PriorityNormal,
		shared.RiskLevelLow,
		meta,
		now(),
	)
	require.NoError(t, err)

	// Mutating original should not affect task
	meta["key"] = "mutated"
	assert.Equal(t, "value", tsk.Metadata()["key"])

	// Mutating returned metadata should not affect task
	returned := tsk.Metadata()
	returned["key"] = "also mutated"
	assert.Equal(t, "value", tsk.Metadata()["key"])
}

// --- Transition Tests ---

func TestTask_Accept(t *testing.T) {
	tsk := createTestTask(t)
	ts := now()

	err := tsk.Accept(ts)
	require.NoError(t, err)
	assert.Equal(t, task.StatusAccepted, tsk.Status())
	assert.Equal(t, ts, tsk.UpdatedAt())
}

func TestTask_Start(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))

	ts := now()
	err := tsk.Start(ts)
	require.NoError(t, err)
	assert.Equal(t, task.StatusInProgress, tsk.Status())
	assert.Equal(t, ts, tsk.UpdatedAt())
}

func TestTask_Complete(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))
	require.NoError(t, tsk.Start(now()))

	ts := now()
	err := tsk.Complete(ts)
	require.NoError(t, err)
	assert.Equal(t, task.StatusCompleted, tsk.Status())
	assert.Equal(t, ts, tsk.UpdatedAt())
}

func TestTask_Fail(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))
	require.NoError(t, tsk.Start(now()))

	ts := now()
	err := tsk.Fail(ts)
	require.NoError(t, err)
	assert.Equal(t, task.StatusFailed, tsk.Status())
	assert.Equal(t, ts, tsk.UpdatedAt())
}

func TestTask_Cancel_FromCreated(t *testing.T) {
	tsk := createTestTask(t)

	err := tsk.Cancel(now())
	require.NoError(t, err)
	assert.Equal(t, task.StatusCancelled, tsk.Status())
}

func TestTask_Cancel_FromAccepted(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))

	err := tsk.Cancel(now())
	require.NoError(t, err)
	assert.Equal(t, task.StatusCancelled, tsk.Status())
}

func TestTask_Cancel_FromInProgress(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))
	require.NoError(t, tsk.Start(now()))

	err := tsk.Cancel(now())
	require.NoError(t, err)
	assert.Equal(t, task.StatusCancelled, tsk.Status())
}

// --- Invalid Transition Tests ---

func TestTask_InvalidTransition_CreatedToCompleted(t *testing.T) {
	tsk := createTestTask(t)

	err := tsk.Complete(now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrInvalidTransition)
	assert.Equal(t, task.StatusCreated, tsk.Status())
}

func TestTask_InvalidTransition_CreatedToFailed(t *testing.T) {
	tsk := createTestTask(t)

	err := tsk.Fail(now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrInvalidTransition)
}

func TestTask_InvalidTransition_CreatedToInProgress(t *testing.T) {
	tsk := createTestTask(t)

	err := tsk.Start(now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrInvalidTransition)
}

func TestTask_InvalidTransition_AcceptedToCompleted(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))

	err := tsk.Complete(now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrInvalidTransition)
}

func TestTask_InvalidTransition_AcceptedToFailed(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))

	err := tsk.Fail(now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, task.ErrInvalidTransition)
}

// --- Terminal State Tests ---

func TestTask_TerminalState_CompletedRejectsAll(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))
	require.NoError(t, tsk.Start(now()))
	require.NoError(t, tsk.Complete(now()))

	assert.ErrorIs(t, tsk.Accept(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Start(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Complete(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Fail(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Cancel(now()), task.ErrInvalidTransition)
}

func TestTask_TerminalState_FailedRejectsAll(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Accept(now()))
	require.NoError(t, tsk.Start(now()))
	require.NoError(t, tsk.Fail(now()))

	assert.ErrorIs(t, tsk.Accept(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Start(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Complete(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Fail(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Cancel(now()), task.ErrInvalidTransition)
}

func TestTask_TerminalState_CancelledRejectsAll(t *testing.T) {
	tsk := createTestTask(t)
	require.NoError(t, tsk.Cancel(now()))

	assert.ErrorIs(t, tsk.Accept(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Start(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Complete(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Fail(now()), task.ErrInvalidTransition)
	assert.ErrorIs(t, tsk.Cancel(now()), task.ErrInvalidTransition)
}

// --- Reconstruct Test ---

func TestReconstruct(t *testing.T) {
	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	parentID, err := shared.NewTaskID(validULID2())
	require.NoError(t, err)

	createdAt := now()
	updatedAt := now()

	tsk := task.Reconstruct(
		id,
		&parentID,
		task.TypeReview,
		"Review PR #42",
		task.ScopeRepo,
		shared.PriorityNormal,
		shared.RiskLevelHigh,
		task.StatusInProgress,
		task.TaskMetadata{"pr": 42},
		createdAt,
		updatedAt,
	)

	assert.Equal(t, id, tsk.ID())
	require.NotNil(t, tsk.ParentTaskID())
	assert.Equal(t, parentID, *tsk.ParentTaskID())
	assert.Equal(t, task.TypeReview, tsk.Type())
	assert.Equal(t, "Review PR #42", tsk.Title())
	assert.Equal(t, task.ScopeRepo, tsk.Scope())
	assert.Equal(t, shared.PriorityNormal, tsk.Priority())
	assert.Equal(t, shared.RiskLevelHigh, tsk.RiskLevel())
	assert.Equal(t, task.StatusInProgress, tsk.Status())
	assert.Equal(t, 42, tsk.Metadata()["pr"])
	assert.Equal(t, createdAt, tsk.CreatedAt())
	assert.Equal(t, updatedAt, tsk.UpdatedAt())
}

// --- Helper ---

func createTestTask(t *testing.T) *task.Task {
	t.Helper()

	id, err := shared.NewTaskID(validULID())
	require.NoError(t, err)

	tsk, err := task.NewTask(
		id,
		nil,
		task.TypeDevelopment,
		"Test task",
		task.ScopeModule,
		shared.PriorityNormal,
		shared.RiskLevelLow,
		nil,
		now(),
	)
	require.NoError(t, err)
	return tsk
}
