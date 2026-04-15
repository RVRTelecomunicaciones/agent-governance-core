package shared

import (
	"fmt"

	"github.com/oklog/ulid/v2"
)

func validateULID(s string) error {
	if s == "" {
		return ErrEmptyID
	}
	if _, err := ulid.ParseStrict(s); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidULID, s)
	}
	return nil
}

// TaskID identifies a task within the governance system.
type TaskID string

func NewTaskID(s string) (TaskID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return TaskID(s), nil
}

func (id TaskID) String() string { return string(id) }

// WorkflowRunID identifies a workflow run.
type WorkflowRunID string

func NewWorkflowRunID(s string) (WorkflowRunID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return WorkflowRunID(s), nil
}

func (id WorkflowRunID) String() string { return string(id) }

// RoutingDecisionID identifies a routing decision.
type RoutingDecisionID string

func NewRoutingDecisionID(s string) (RoutingDecisionID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return RoutingDecisionID(s), nil
}

func (id RoutingDecisionID) String() string { return string(id) }

// PolicyDecisionID identifies a policy decision.
type PolicyDecisionID string

func NewPolicyDecisionID(s string) (PolicyDecisionID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return PolicyDecisionID(s), nil
}

func (id PolicyDecisionID) String() string { return string(id) }

// ApprovalRequestID identifies an approval request.
type ApprovalRequestID string

func NewApprovalRequestID(s string) (ApprovalRequestID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return ApprovalRequestID(s), nil
}

func (id ApprovalRequestID) String() string { return string(id) }

// ExecutionLeaseID identifies an execution lease.
type ExecutionLeaseID string

func NewExecutionLeaseID(s string) (ExecutionLeaseID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return ExecutionLeaseID(s), nil
}

func (id ExecutionLeaseID) String() string { return string(id) }

// EscalationTriggerID identifies an escalation trigger.
type EscalationTriggerID string

func NewEscalationTriggerID(s string) (EscalationTriggerID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return EscalationTriggerID(s), nil
}

func (id EscalationTriggerID) String() string { return string(id) }

// AuditEntryID identifies an audit entry.
type AuditEntryID string

func NewAuditEntryID(s string) (AuditEntryID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return AuditEntryID(s), nil
}

func (id AuditEntryID) String() string { return string(id) }

// ActorID identifies an actor (human or system). Validates non-empty only.
type ActorID string

func NewActorID(s string) (ActorID, error) {
	if s == "" {
		return "", ErrEmptyID
	}
	return ActorID(s), nil
}

func (id ActorID) String() string { return string(id) }
