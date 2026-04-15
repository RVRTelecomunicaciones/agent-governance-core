package outbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

// GovernanceNotifier defines callback notifications for consumer systems.
type GovernanceNotifier interface {
	OnExecutionReady(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error
	OnApprovalRequired(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error
	OnWorkflowTerminated(ctx context.Context, wf *workflow.WorkflowRun, reason string) error
}
