package idgen

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// ULIDGenerator generates unique, time-sortable IDs for all domain ID types.
type ULIDGenerator struct{}

func (g ULIDGenerator) generate() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

func (g ULIDGenerator) NewTaskID() shared.TaskID {
	return shared.TaskID(g.generate())
}

func (g ULIDGenerator) NewWorkflowRunID() shared.WorkflowRunID {
	return shared.WorkflowRunID(g.generate())
}

func (g ULIDGenerator) NewRoutingDecisionID() shared.RoutingDecisionID {
	return shared.RoutingDecisionID(g.generate())
}

func (g ULIDGenerator) NewPolicyDecisionID() shared.PolicyDecisionID {
	return shared.PolicyDecisionID(g.generate())
}

func (g ULIDGenerator) NewApprovalRequestID() shared.ApprovalRequestID {
	return shared.ApprovalRequestID(g.generate())
}

func (g ULIDGenerator) NewExecutionLeaseID() shared.ExecutionLeaseID {
	return shared.ExecutionLeaseID(g.generate())
}

func (g ULIDGenerator) NewEscalationTriggerID() shared.EscalationTriggerID {
	return shared.EscalationTriggerID(g.generate())
}

func (g ULIDGenerator) NewAuditEntryID() shared.AuditEntryID {
	return shared.AuditEntryID(g.generate())
}
