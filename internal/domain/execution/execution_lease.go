package execution

import (
	"errors"
	"fmt"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// LeaseStatus represents the current state of an execution lease.
type LeaseStatus string

const (
	LeaseStatusActive    LeaseStatus = "active"
	LeaseStatusExhausted LeaseStatus = "exhausted"
	LeaseStatusRevoked   LeaseStatus = "revoked"
)

var (
	ErrLeaseNotActive       = errors.New("lease is not active")
	ErrInvalidTimeoutBudget = errors.New("timeout budget must be greater than zero")
	ErrInvalidRetryBudget   = errors.New("retry budget max attempts must be greater than zero")
)

// RetryBudget constrains how many attempts an execution lease allows.
type RetryBudget struct {
	MaxAttempts int
}

// NewRetryBudget creates a RetryBudget, validating that max > 0.
func NewRetryBudget(max int) (RetryBudget, error) {
	if max <= 0 {
		return RetryBudget{}, fmt.Errorf("%w: got %d", ErrInvalidRetryBudget, max)
	}
	return RetryBudget{MaxAttempts: max}, nil
}

// ExecutionLease is the aggregate root for execution resource budgets.
type ExecutionLease struct {
	ID            shared.ExecutionLeaseID
	WorkflowRunID shared.WorkflowRunID
	TimeoutBudget time.Duration
	RetryBudget   RetryBudget
	AttemptsUsed  int
	TimeElapsed   time.Duration
	Status        LeaseStatus
	CreatedAt     shared.Timestamp
}

// NewExecutionLease creates a new active execution lease with validated budgets.
func NewExecutionLease(
	id shared.ExecutionLeaseID,
	workflowRunID shared.WorkflowRunID,
	timeoutBudget time.Duration,
	retryBudget RetryBudget,
	createdAt shared.Timestamp,
) (*ExecutionLease, error) {
	if timeoutBudget <= 0 {
		return nil, ErrInvalidTimeoutBudget
	}
	return &ExecutionLease{
		ID:            id,
		WorkflowRunID: workflowRunID,
		TimeoutBudget: timeoutBudget,
		RetryBudget:   retryBudget,
		AttemptsUsed:  0,
		TimeElapsed:   0,
		Status:        LeaseStatusActive,
		CreatedAt:     createdAt,
	}, nil
}

// HasRetryBudget returns true if there are remaining retry attempts and the lease is active.
func (l *ExecutionLease) HasRetryBudget() bool {
	return l.Status == LeaseStatusActive && l.AttemptsUsed < l.RetryBudget.MaxAttempts
}

// RecordAttempt increments AttemptsUsed. If budget is reached, status becomes exhausted.
// Returns an error if the lease is not active.
func (l *ExecutionLease) RecordAttempt() error {
	if l.Status != LeaseStatusActive {
		return fmt.Errorf("%w: status is %s", ErrLeaseNotActive, l.Status)
	}
	l.AttemptsUsed++
	if l.AttemptsUsed >= l.RetryBudget.MaxAttempts {
		l.Status = LeaseStatusExhausted
	}
	return nil
}

// UpdateTimeElapsed sets the elapsed time on the lease.
func (l *ExecutionLease) UpdateTimeElapsed(elapsed time.Duration) {
	l.TimeElapsed = elapsed
}

// Revoke transitions the lease from active to revoked.
func (l *ExecutionLease) Revoke() error {
	if l.Status != LeaseStatusActive {
		return fmt.Errorf("%w: status is %s", ErrLeaseNotActive, l.Status)
	}
	l.Status = LeaseStatusRevoked
	return nil
}

// ReconstructExecutionLease creates an ExecutionLease from persisted data (repository hydration).
func ReconstructExecutionLease(
	id shared.ExecutionLeaseID,
	workflowRunID shared.WorkflowRunID,
	timeoutBudget time.Duration,
	retryBudget RetryBudget,
	attemptsUsed int,
	timeElapsed time.Duration,
	status LeaseStatus,
	createdAt shared.Timestamp,
) *ExecutionLease {
	return &ExecutionLease{
		ID:            id,
		WorkflowRunID: workflowRunID,
		TimeoutBudget: timeoutBudget,
		RetryBudget:   retryBudget,
		AttemptsUsed:  attemptsUsed,
		TimeElapsed:   timeElapsed,
		Status:        status,
		CreatedAt:     createdAt,
	}
}
