package outbound

import "github.com/russellcxl/agent-governance-core/internal/domain/shared"

// IDGenerator abstracts ID generation. ULID implementation lives in the adapter layer.
type IDGenerator interface {
	NewTaskID() shared.TaskID
	NewWorkflowRunID() shared.WorkflowRunID
	NewRoutingDecisionID() shared.RoutingDecisionID
	NewPolicyDecisionID() shared.PolicyDecisionID
	NewApprovalRequestID() shared.ApprovalRequestID
	NewExecutionLeaseID() shared.ExecutionLeaseID
	NewEscalationTriggerID() shared.EscalationTriggerID
	NewAuditEntryID() shared.AuditEntryID
}
