package fixtures

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// ApprovalRequestOption configures a test approval request.
type ApprovalRequestOption func(*approvalRequestConfig)

type approvalRequestConfig struct {
	id            shared.ApprovalRequestID
	taskID        shared.TaskID
	workflowRunID shared.WorkflowRunID
	reason        string
}

func defaultApprovalRequestConfig() approvalRequestConfig {
	id, _ := shared.NewApprovalRequestID(newULID())
	taskID, _ := shared.NewTaskID(newULID())
	wfID, _ := shared.NewWorkflowRunID(newULID())
	return approvalRequestConfig{
		id:            id,
		taskID:        taskID,
		workflowRunID: wfID,
		reason:        "test approval needed",
	}
}

// WithApprovalRequestID sets the approval request ID.
func WithApprovalRequestID(id shared.ApprovalRequestID) ApprovalRequestOption {
	return func(c *approvalRequestConfig) { c.id = id }
}

// WithApprovalTaskID sets the task ID for the approval request.
func WithApprovalTaskID(id shared.TaskID) ApprovalRequestOption {
	return func(c *approvalRequestConfig) { c.taskID = id }
}

// WithApprovalWorkflowRunID sets the workflow run ID for the approval request.
func WithApprovalWorkflowRunID(id shared.WorkflowRunID) ApprovalRequestOption {
	return func(c *approvalRequestConfig) { c.workflowRunID = id }
}

// WithApprovalReason sets the reason for the approval request.
func WithApprovalReason(reason string) ApprovalRequestOption {
	return func(c *approvalRequestConfig) { c.reason = reason }
}

// NewTestApprovalRequest creates a valid pending ApprovalRequest. Panics on error (test-only).
func NewTestApprovalRequest(opts ...ApprovalRequestOption) *approval.ApprovalRequest {
	cfg := defaultApprovalRequestConfig()
	for _, o := range opts {
		o(&cfg)
	}

	ar, err := approval.NewApprovalRequest(
		cfg.id,
		cfg.taskID,
		cfg.workflowRunID,
		cfg.reason,
		approval.ApproverSpec{Role: "human"},
		nil, // no expiration
		now(),
	)
	if err != nil {
		panic("fixtures.NewTestApprovalRequest: " + err.Error())
	}
	return ar
}
