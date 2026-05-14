package outbound

import (
	"context"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

// TaskRepository defines persistence operations for the Task aggregate.
type TaskRepository interface {
	Save(ctx context.Context, t *task.Task) error
	FindByID(ctx context.Context, id shared.TaskID) (*task.Task, error)
	FindByParentID(ctx context.Context, parentID shared.TaskID) ([]*task.Task, error)
	UpdateStatus(ctx context.Context, t *task.Task) error
}

// WorkflowRunRepository defines persistence operations for the WorkflowRun aggregate.
type WorkflowRunRepository interface {
	Save(ctx context.Context, wf *workflow.WorkflowRun) error
	FindByID(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	Update(ctx context.Context, wf *workflow.WorkflowRun) error
	List(ctx context.Context, filter WorkflowListFilter) ([]*workflow.WorkflowRun, int, error)
}

// ExecutionLeaseRepository defines persistence operations for the ExecutionLease aggregate.
type ExecutionLeaseRepository interface {
	Save(ctx context.Context, lease *execution.ExecutionLease) error
	FindByWorkflowRunID(ctx context.Context, wfID shared.WorkflowRunID) (*execution.ExecutionLease, error)
	Update(ctx context.Context, lease *execution.ExecutionLease) error
}

// RoutingDecisionRepository defines persistence operations for the RoutingDecision aggregate.
// RoutingDecision is immutable — no Update method.
type RoutingDecisionRepository interface {
	Save(ctx context.Context, rd *routing.RoutingDecision) error
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
}

// PolicyDecisionRepository defines persistence operations for the PolicyDecision aggregate.
// PolicyDecision is immutable — no Update method.
type PolicyDecisionRepository interface {
	Save(ctx context.Context, pd *policy.PolicyDecision) error
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*policy.PolicyDecision, error)
}

// ApprovalRequestRepository defines persistence operations for the ApprovalRequest aggregate.
type ApprovalRequestRepository interface {
	Save(ctx context.Context, req *approval.ApprovalRequest) error
	FindByID(ctx context.Context, id shared.ApprovalRequestID) (*approval.ApprovalRequest, error)
	FindByTaskID(ctx context.Context, taskID shared.TaskID) ([]*approval.ApprovalRequest, error)
	FindPending(ctx context.Context) ([]*approval.ApprovalRequest, error)
	Update(ctx context.Context, req *approval.ApprovalRequest) error
}

// EscalationTriggerRepository defines persistence operations for the EscalationTrigger aggregate.
type EscalationTriggerRepository interface {
	Save(ctx context.Context, trigger *escalation.EscalationTrigger) error
	FindByTaskID(ctx context.Context, taskID shared.TaskID) ([]*escalation.EscalationTrigger, error)
	Update(ctx context.Context, trigger *escalation.EscalationTrigger) error
}

// WorkflowListFilter defines filter criteria for listing workflow runs.
type WorkflowListFilter struct {
	Status *workflow.WorkflowStatus
	TaskID *shared.TaskID
	Limit  int
	Offset int
}

// AuditFilter defines filter criteria for querying audit entries.
type AuditFilter struct {
	TaskID        *shared.TaskID
	WorkflowRunID *shared.WorkflowRunID
	Actor         *shared.ActorID
	Action        *string
	CreatedAfter  *time.Time // WHERE created_at > $N
	CreatedBefore *time.Time // WHERE created_at < $N
	Limit         int
	Offset        int
}

// AuditEntryRepository defines persistence operations for the AuditEntry aggregate.
// Audit entries are append-only — no Update or Delete methods.
type AuditEntryRepository interface {
	Append(ctx context.Context, entry *audit.AuditEntry) error
	Query(ctx context.Context, filter AuditFilter) ([]*audit.AuditEntry, int, error)
}

// --- M-E0 orchestator-facing decision facade ---
//
// PhaseDecisionRecord and PhaseApprovalRecord are intentionally flat structs,
// not domain aggregates. The /governance/v1/decisions/... surface is an
// orchestator-facing facade for V1 (default-allow). The "real" policy engine
// stays in the policy domain via policy_decisions. Sprint 3 unification will
// decide whether to promote these to aggregates or merge into policy.

// PhaseDecisionRecord is the row persisted for each /governance/v1/decisions/*
// evaluation.
type PhaseDecisionRecord struct {
	ID         string
	ChangeID   string
	PhaseType  string // e.g. "explore", "apply", "sensitive"
	Capability string // populated only for sensitive evaluations
	Sensitive  bool
	Decision   string // "allow" | "deny" | "require_approval"
	AgentRole  string
	Strategy   string
	Reason     string
	CreatedAt  time.Time
}

// PhaseApprovalRecord is the row queried by approval polling.
type PhaseApprovalRecord struct {
	ChangeID  string
	PhaseID   string
	Status    string // "pending" | "granted" | "denied"
	Reason    string
	DecidedBy string
	DecidedAt *time.Time
	CreatedAt time.Time
}

// PhaseDecisionRepository persists phase/sensitive decisions for M-E0 audit.
type PhaseDecisionRepository interface {
	Save(ctx context.Context, rec PhaseDecisionRecord) error
}

// PhaseApprovalRepository looks up explicit approval gates by (change, phase).
// Find returns (nil, nil) when no row exists — interpreted by callers as
// "auto-granted" under V1 default-allow.
type PhaseApprovalRepository interface {
	Find(ctx context.Context, changeID, phaseID string) (*PhaseApprovalRecord, error)
}
