package fixtures

import (
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// ExecutionLeaseOption configures a test execution lease.
type ExecutionLeaseOption func(*executionLeaseConfig)

type executionLeaseConfig struct {
	id            shared.ExecutionLeaseID
	workflowRunID shared.WorkflowRunID
	timeoutBudget time.Duration
	retryBudget   execution.RetryBudget
}

func defaultExecutionLeaseConfig() executionLeaseConfig {
	id, _ := shared.NewExecutionLeaseID(newULID())
	wfID, _ := shared.NewWorkflowRunID(newULID())
	rb, _ := execution.NewRetryBudget(3)
	return executionLeaseConfig{
		id:            id,
		workflowRunID: wfID,
		timeoutBudget: 5 * time.Minute,
		retryBudget:   rb,
	}
}

// WithLeaseID sets the execution lease ID.
func WithLeaseID(id shared.ExecutionLeaseID) ExecutionLeaseOption {
	return func(c *executionLeaseConfig) { c.id = id }
}

// WithLeaseWorkflowRunID sets the workflow run ID for the lease.
func WithLeaseWorkflowRunID(id shared.WorkflowRunID) ExecutionLeaseOption {
	return func(c *executionLeaseConfig) { c.workflowRunID = id }
}

// WithTimeoutBudget sets the timeout budget for the lease.
func WithTimeoutBudget(d time.Duration) ExecutionLeaseOption {
	return func(c *executionLeaseConfig) { c.timeoutBudget = d }
}

// WithRetryBudget sets the retry budget for the lease.
func WithRetryBudget(rb execution.RetryBudget) ExecutionLeaseOption {
	return func(c *executionLeaseConfig) { c.retryBudget = rb }
}

// NewTestExecutionLease creates a valid active ExecutionLease. Panics on error (test-only).
func NewTestExecutionLease(opts ...ExecutionLeaseOption) *execution.ExecutionLease {
	cfg := defaultExecutionLeaseConfig()
	for _, o := range opts {
		o(&cfg)
	}

	lease, err := execution.NewExecutionLease(
		cfg.id,
		cfg.workflowRunID,
		cfg.timeoutBudget,
		cfg.retryBudget,
		now(),
	)
	if err != nil {
		panic("fixtures.NewTestExecutionLease: " + err.Error())
	}
	return lease
}
