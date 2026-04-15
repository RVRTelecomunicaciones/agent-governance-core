package execution

import (
	"errors"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// AttemptStatus represents the outcome of an execution attempt.
type AttemptStatus string

const (
	AttemptStatusSuccess AttemptStatus = "success"
	AttemptStatusFailure AttemptStatus = "failure"
	AttemptStatusRetry   AttemptStatus = "retry"
)

var (
	ErrFailureFieldsOnSuccess = errors.New("success result must not have failure fields")
	ErrMissingFailureStage    = errors.New("failure/retry result must have failure stage")
	ErrMissingFailureCode     = errors.New("failure/retry result must have failure code")
	ErrEmptyFailureCode       = errors.New("failure code must not be empty")
	ErrMissingRetryable       = errors.New("failure/retry result must have retryable flag")
)

// AttemptResult is a value object that captures the outcome of a single execution attempt.
type AttemptResult struct {
	Status       AttemptStatus
	FailureStage *shared.FailureStage
	FailureCode  *string
	Retryable    *bool
	ToolName     *string
	StrategyUsed *string
	AgentRole    *string
	Detail       *string
}

// NewSuccessResult creates a successful attempt result with no failure fields.
func NewSuccessResult() AttemptResult {
	return AttemptResult{Status: AttemptStatusSuccess}
}

// NewFailureResult creates a failure attempt result. Code must be non-empty.
func NewFailureResult(stage shared.FailureStage, code string, retryable bool) (AttemptResult, error) {
	if code == "" {
		return AttemptResult{}, ErrEmptyFailureCode
	}
	return AttemptResult{
		Status:       AttemptStatusFailure,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
	}, nil
}

// NewRetryResult creates a retry attempt result. Retryable is always true.
func NewRetryResult(stage shared.FailureStage, code string) (AttemptResult, error) {
	if code == "" {
		return AttemptResult{}, ErrEmptyFailureCode
	}
	retryable := true
	return AttemptResult{
		Status:       AttemptStatusRetry,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
	}, nil
}

// WithToolName returns a copy with the tool name set.
func (a AttemptResult) WithToolName(name string) AttemptResult {
	a.ToolName = &name
	return a
}

// WithStrategy returns a copy with the strategy set.
func (a AttemptResult) WithStrategy(s string) AttemptResult {
	a.StrategyUsed = &s
	return a
}

// WithAgentRole returns a copy with the agent role set.
func (a AttemptResult) WithAgentRole(role string) AttemptResult {
	a.AgentRole = &role
	return a
}

// WithDetail returns a copy with the detail set.
func (a AttemptResult) WithDetail(detail string) AttemptResult {
	a.Detail = &detail
	return a
}

// Validate checks the invariants of the attempt result.
func (a AttemptResult) Validate() error {
	switch a.Status {
	case AttemptStatusSuccess:
		if a.FailureStage != nil || a.FailureCode != nil || a.Retryable != nil {
			return fmt.Errorf("%w", ErrFailureFieldsOnSuccess)
		}
	case AttemptStatusFailure, AttemptStatusRetry:
		if a.FailureStage == nil {
			return fmt.Errorf("%w", ErrMissingFailureStage)
		}
		if a.FailureCode == nil {
			return fmt.Errorf("%w", ErrMissingFailureCode)
		}
		if *a.FailureCode == "" {
			return fmt.Errorf("%w", ErrEmptyFailureCode)
		}
		if a.Retryable == nil {
			return fmt.Errorf("%w", ErrMissingRetryable)
		}
	default:
		return fmt.Errorf("unknown attempt status: %q", a.Status)
	}
	return nil
}
