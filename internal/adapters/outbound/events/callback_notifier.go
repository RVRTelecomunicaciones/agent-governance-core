package events

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.GovernanceNotifier = (*CallbackNotifier)(nil)

// ExecutionReadyFunc is a callback invoked when execution is ready.
type ExecutionReadyFunc func(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error

// ApprovalRequiredFunc is a callback invoked when approval is required.
type ApprovalRequiredFunc func(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error

// WorkflowTerminatedFunc is a callback invoked when a workflow is terminated.
type WorkflowTerminatedFunc func(ctx context.Context, wf *workflow.WorkflowRun, reason string) error

// CallbackNotifier is an in-process notifier that delegates to registered Go callbacks.
// If no callback is registered for a given event, the call is a no-op.
type CallbackNotifier struct {
	onExecutionReady     ExecutionReadyFunc
	onApprovalRequired   ApprovalRequiredFunc
	onWorkflowTerminated WorkflowTerminatedFunc
}

// NewCallbackNotifier creates a new CallbackNotifier with no callbacks registered.
func NewCallbackNotifier() *CallbackNotifier {
	return &CallbackNotifier{}
}

// SetOnExecutionReady registers a callback for execution-ready events.
func (n *CallbackNotifier) SetOnExecutionReady(f ExecutionReadyFunc) { n.onExecutionReady = f }

// SetOnApprovalRequired registers a callback for approval-required events.
func (n *CallbackNotifier) SetOnApprovalRequired(f ApprovalRequiredFunc) {
	n.onApprovalRequired = f
}

// SetOnWorkflowTerminated registers a callback for workflow-terminated events.
func (n *CallbackNotifier) SetOnWorkflowTerminated(f WorkflowTerminatedFunc) {
	n.onWorkflowTerminated = f
}

// OnExecutionReady invokes the registered callback, or returns nil if none is set.
func (n *CallbackNotifier) OnExecutionReady(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error {
	if n.onExecutionReady != nil {
		return n.onExecutionReady(ctx, wf, lease)
	}
	return nil
}

// OnApprovalRequired invokes the registered callback, or returns nil if none is set.
func (n *CallbackNotifier) OnApprovalRequired(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error {
	if n.onApprovalRequired != nil {
		return n.onApprovalRequired(ctx, wf, req)
	}
	return nil
}

// OnWorkflowTerminated invokes the registered callback, or returns nil if none is set.
func (n *CallbackNotifier) OnWorkflowTerminated(ctx context.Context, wf *workflow.WorkflowRun, reason string) error {
	if n.onWorkflowTerminated != nil {
		return n.onWorkflowTerminated(ctx, wf, reason)
	}
	return nil
}
