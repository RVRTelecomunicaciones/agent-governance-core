package events

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
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
	tracer               trace.Tracer
}

// NewCallbackNotifier creates a new CallbackNotifier with no callbacks registered.
func NewCallbackNotifier() *CallbackNotifier {
	return &CallbackNotifier{
		tracer: otel.Tracer("governance.notifier"),
	}
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
	ctx, span := n.tracer.Start(ctx, "GovernanceNotifier.OnExecutionReady")
	defer span.End()
	if n.onExecutionReady != nil {
		if err := n.onExecutionReady(ctx, wf, lease); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	return nil
}

// OnApprovalRequired invokes the registered callback, or returns nil if none is set.
func (n *CallbackNotifier) OnApprovalRequired(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error {
	ctx, span := n.tracer.Start(ctx, "GovernanceNotifier.OnApprovalRequired")
	defer span.End()
	if n.onApprovalRequired != nil {
		if err := n.onApprovalRequired(ctx, wf, req); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	return nil
}

// OnWorkflowTerminated invokes the registered callback, or returns nil if none is set.
func (n *CallbackNotifier) OnWorkflowTerminated(ctx context.Context, wf *workflow.WorkflowRun, reason string) error {
	ctx, span := n.tracer.Start(ctx, "GovernanceNotifier.OnWorkflowTerminated")
	defer span.End()
	if n.onWorkflowTerminated != nil {
		if err := n.onWorkflowTerminated(ctx, wf, reason); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	return nil
}
