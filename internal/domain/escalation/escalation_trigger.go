package escalation

import (
	"errors"
	"fmt"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// Errors for the escalation aggregate.
var (
	ErrNotPending   = errors.New("escalation trigger is not in pending status")
	ErrNotTriggered = errors.New("escalation trigger is not in triggered status")
)

// --- EscalationTarget enum ---

// EscalationTarget represents who should handle the escalation.
type EscalationTarget string

const (
	TargetHuman       EscalationTarget = "human"
	TargetSeniorAgent EscalationTarget = "senior_agent"
	TargetAdmin       EscalationTarget = "admin"
)

var validEscalationTargets = map[EscalationTarget]bool{
	TargetHuman:       true,
	TargetSeniorAgent: true,
	TargetAdmin:       true,
}

// NewEscalationTarget validates and returns an EscalationTarget.
func NewEscalationTarget(s string) (EscalationTarget, error) {
	et := EscalationTarget(s)
	if !validEscalationTargets[et] {
		return "", fmt.Errorf("invalid escalation target: %q", s)
	}
	return et, nil
}

func (et EscalationTarget) String() string { return string(et) }

// --- EscalationStatus enum ---

// EscalationStatus represents the lifecycle state of an escalation trigger.
type EscalationStatus string

const (
	StatusPending   EscalationStatus = "pending"
	StatusTriggered EscalationStatus = "triggered"
	StatusResolved  EscalationStatus = "resolved"
)

func (es EscalationStatus) String() string { return string(es) }

// --- EscalationCondition VO ---

// EscalationCondition describes when an escalation should fire.
type EscalationCondition struct {
	Type       string
	Parameters map[string]any
}

// --- EscalationTrigger aggregate ---

// EscalationTrigger is the aggregate root for escalation decisions.
type EscalationTrigger struct {
	id          shared.EscalationTriggerID
	taskID      shared.TaskID
	condition   EscalationCondition
	target      EscalationTarget
	status      EscalationStatus
	triggeredAt *shared.Timestamp
	createdAt   shared.Timestamp
}

// NewEscalationTrigger creates a new EscalationTrigger in pending status.
func NewEscalationTrigger(
	id shared.EscalationTriggerID,
	taskID shared.TaskID,
	condition EscalationCondition,
	target EscalationTarget,
	now shared.Timestamp,
) *EscalationTrigger {
	return &EscalationTrigger{
		id:          id,
		taskID:      taskID,
		condition:   condition,
		target:      target,
		status:      StatusPending,
		triggeredAt: nil,
		createdAt:   now,
	}
}

// ReconstructEscalationTrigger creates an EscalationTrigger from persisted state without validation.
func ReconstructEscalationTrigger(
	id shared.EscalationTriggerID,
	taskID shared.TaskID,
	condition EscalationCondition,
	target EscalationTarget,
	status EscalationStatus,
	triggeredAt *shared.Timestamp,
	createdAt shared.Timestamp,
) *EscalationTrigger {
	return &EscalationTrigger{
		id:          id,
		taskID:      taskID,
		condition:   condition,
		target:      target,
		status:      status,
		triggeredAt: triggeredAt,
		createdAt:   createdAt,
	}
}

// Trigger fires the escalation, transitioning from pending to triggered.
func (et *EscalationTrigger) Trigger(now shared.Timestamp) error {
	if et.status != StatusPending {
		return fmt.Errorf("%w: current status is %q", ErrNotPending, et.status)
	}
	et.status = StatusTriggered
	et.triggeredAt = &now
	return nil
}

// Resolve marks the escalation as handled, transitioning from triggered to resolved.
func (et *EscalationTrigger) Resolve() error {
	if et.status != StatusTriggered {
		return fmt.Errorf("%w: current status is %q", ErrNotTriggered, et.status)
	}
	et.status = StatusResolved
	return nil
}

// --- Accessors ---

func (et *EscalationTrigger) ID() shared.EscalationTriggerID { return et.id }
func (et *EscalationTrigger) TaskID() shared.TaskID          { return et.taskID }
func (et *EscalationTrigger) Condition() EscalationCondition { return et.condition }
func (et *EscalationTrigger) Target() EscalationTarget       { return et.target }
func (et *EscalationTrigger) Status() EscalationStatus       { return et.status }
func (et *EscalationTrigger) TriggeredAt() *shared.Timestamp { return et.triggeredAt }
func (et *EscalationTrigger) CreatedAt() shared.Timestamp    { return et.createdAt }
