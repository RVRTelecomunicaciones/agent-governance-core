package execution

import (
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSuccessResult_Validates(t *testing.T) {
	r := NewSuccessResult()
	err := r.Validate()
	require.NoError(t, err)
	assert.Equal(t, AttemptStatusSuccess, r.Status)
	assert.Nil(t, r.FailureStage)
	assert.Nil(t, r.FailureCode)
	assert.Nil(t, r.Retryable)
}

func TestSuccessResult_WithFailureFields_FailsValidation(t *testing.T) {
	stage := shared.FailureStageExecution
	r := AttemptResult{
		Status:       AttemptStatusSuccess,
		FailureStage: &stage,
	}
	err := r.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFailureFieldsOnSuccess)
}

func TestNewFailureResult_WithAllRequiredFields_Validates(t *testing.T) {
	r, err := NewFailureResult(shared.FailureStageExecution, "tool/shell_timeout", false)
	require.NoError(t, err)

	err = r.Validate()
	require.NoError(t, err)
	assert.Equal(t, AttemptStatusFailure, r.Status)
	assert.NotNil(t, r.FailureStage)
	assert.NotNil(t, r.FailureCode)
	assert.NotNil(t, r.Retryable)
	assert.False(t, *r.Retryable)
}

func TestNewFailureResult_EmptyCode_Fails(t *testing.T) {
	_, err := NewFailureResult(shared.FailureStageExecution, "", false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFailureCode)
}

func TestFailureResult_MissingCode_FailsValidation(t *testing.T) {
	stage := shared.FailureStageExecution
	retryable := true
	r := AttemptResult{
		Status:       AttemptStatusFailure,
		FailureStage: &stage,
		Retryable:    &retryable,
	}
	err := r.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingFailureCode)
}

func TestNewRetryResult_AlwaysRetryable(t *testing.T) {
	r, err := NewRetryResult(shared.FailureStageRuntime, "runtime/oom")
	require.NoError(t, err)

	err = r.Validate()
	require.NoError(t, err)
	assert.Equal(t, AttemptStatusRetry, r.Status)
	require.NotNil(t, r.Retryable)
	assert.True(t, *r.Retryable)
}

func TestNewRetryResult_EmptyCode_Fails(t *testing.T) {
	_, err := NewRetryResult(shared.FailureStageRuntime, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFailureCode)
}

func TestBuilderMethods(t *testing.T) {
	r, _ := NewFailureResult(shared.FailureStageExecution, "tool/timeout", true)
	r = r.WithToolName("shell").WithStrategy("retry-backoff").WithAgentRole("executor").WithDetail("timed out after 30s")

	require.NotNil(t, r.ToolName)
	assert.Equal(t, "shell", *r.ToolName)
	require.NotNil(t, r.StrategyUsed)
	assert.Equal(t, "retry-backoff", *r.StrategyUsed)
	require.NotNil(t, r.AgentRole)
	assert.Equal(t, "executor", *r.AgentRole)
	require.NotNil(t, r.Detail)
	assert.Equal(t, "timed out after 30s", *r.Detail)

	// Still validates after builder methods.
	err := r.Validate()
	require.NoError(t, err)
}

func TestSuccessResult_WithRetryableSet_FailsValidation(t *testing.T) {
	retryable := false
	r := AttemptResult{
		Status:    AttemptStatusSuccess,
		Retryable: &retryable,
	}
	err := r.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFailureFieldsOnSuccess)
}

func TestFailureResult_MissingStage_FailsValidation(t *testing.T) {
	code := "tool/error"
	retryable := true
	r := AttemptResult{
		Status:      AttemptStatusFailure,
		FailureCode: &code,
		Retryable:   &retryable,
	}
	err := r.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingFailureStage)
}

func TestFailureResult_MissingRetryable_FailsValidation(t *testing.T) {
	stage := shared.FailureStageExecution
	code := "tool/error"
	r := AttemptResult{
		Status:       AttemptStatusFailure,
		FailureStage: &stage,
		FailureCode:  &code,
	}
	err := r.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingRetryable)
}
