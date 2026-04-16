package sdk

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	escalationdomain "github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// GovernanceFacade composes all inbound ports into a single consumer-friendly entry point.
// It contains NO business logic — all methods delegate directly to the underlying services.
type GovernanceFacade struct {
	governance inbound.GovernanceService
	control    inbound.WorkflowControl
	approvals  inbound.ApprovalService
	queries    inbound.QueryService
	escalation inbound.EscalationPort
}

func NewGovernanceFacade(
	governance inbound.GovernanceService,
	control inbound.WorkflowControl,
	approvals inbound.ApprovalService,
	queries inbound.QueryService,
	escalation inbound.EscalationPort,
) *GovernanceFacade {
	return &GovernanceFacade{
		governance: governance,
		control:    control,
		approvals:  approvals,
		queries:    queries,
		escalation: escalation,
	}
}

// --- GovernanceService methods ---

func (f *GovernanceFacade) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	return f.governance.SubmitTask(ctx, input)
}

func (f *GovernanceFacade) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	return f.governance.ProcessTask(ctx, input, action)
}

func (f *GovernanceFacade) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	return f.governance.RouteTask(ctx, taskID)
}

func (f *GovernanceFacade) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	return f.governance.EvaluatePolicy(ctx, taskID, action)
}

func (f *GovernanceFacade) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return f.governance.StartWorkflow(ctx, taskID)
}

// --- WorkflowControl methods ---

func (f *GovernanceFacade) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return f.control.KillWorkflow(ctx, id, reason, actor)
}

func (f *GovernanceFacade) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return f.control.PauseWorkflow(ctx, id, reason, actor)
}

func (f *GovernanceFacade) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return f.control.ResumeWorkflow(ctx, id, reason, actor)
}

func (f *GovernanceFacade) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	return f.control.RegisterAttempt(ctx, id, result)
}

// --- ApprovalService methods ---

func (f *GovernanceFacade) ResolveApproval(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
	return f.approvals.ResolveApproval(ctx, input)
}

func (f *GovernanceFacade) GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error) {
	return f.approvals.GetPendingApprovals(ctx)
}

// --- QueryService methods ---

func (f *GovernanceFacade) GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	return f.queries.GetTask(ctx, id)
}

func (f *GovernanceFacade) GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	return f.queries.GetWorkflowStatus(ctx, id)
}

func (f *GovernanceFacade) GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return f.queries.GetWorkflowByTask(ctx, taskID)
}

func (f *GovernanceFacade) QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
	return f.queries.QueryAuditTrail(ctx, filter)
}

func (f *GovernanceFacade) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return f.queries.ListWorkflows(ctx, filter)
}

func (f *GovernanceFacade) ListBreakers(ctx context.Context, filter resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error) {
	return f.queries.ListBreakers(ctx, filter)
}

func (f *GovernanceFacade) GetBreakerState(ctx context.Context, tool, agentRole string) (*resilience.BreakerSnapshot, error) {
	return f.queries.GetBreakerState(ctx, tool, agentRole)
}

// --- EscalationPort methods ---

func (f *GovernanceFacade) TriggerEscalation(ctx context.Context, taskID shared.TaskID, condition escalationdomain.EscalationCondition, target escalationdomain.EscalationTarget) (*escalationdomain.EscalationTrigger, error) {
	return f.escalation.TriggerEscalation(ctx, taskID, condition, target)
}
