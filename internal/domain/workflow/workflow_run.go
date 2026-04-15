package workflow

import (
	"errors"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

var (
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrTerminalState     = errors.New("cannot transition from terminal state")
)

// WorkflowRun is the aggregate root for a workflow execution.
type WorkflowRun struct {
	ID                shared.WorkflowRunID
	TaskID            shared.TaskID
	Status            WorkflowStatus
	RoutingDecisionID *shared.RoutingDecisionID
	PolicyDecisionID  *shared.PolicyDecisionID
	CurrentStepIndex  int
	Transitions       []WorkflowTransition
	CreatedAt         shared.Timestamp
	UpdatedAt         shared.Timestamp
}

// NewWorkflowRun creates a new WorkflowRun in the created state.
func NewWorkflowRun(id shared.WorkflowRunID, taskID shared.TaskID, now shared.Timestamp) *WorkflowRun {
	return &WorkflowRun{
		ID:               id,
		TaskID:           taskID,
		Status:           StatusCreated,
		CurrentStepIndex: 0,
		Transitions:      nil,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// MarkRouted transitions from created → routed and stores the routing decision.
func (w *WorkflowRun) MarkRouted(rdID shared.RoutingDecisionID, reason string, actor shared.ActorID, now shared.Timestamp) error {
	if err := w.transitionTo(StatusRouted, reason, actor, now); err != nil {
		return err
	}
	w.RoutingDecisionID = &rdID
	return nil
}

// MarkPolicyChecked transitions from routed → policy_checked and stores the policy decision.
func (w *WorkflowRun) MarkPolicyChecked(pdID shared.PolicyDecisionID, actor shared.ActorID, now shared.Timestamp) error {
	if err := w.transitionTo(StatusPolicyChecked, "", actor, now); err != nil {
		return err
	}
	w.PolicyDecisionID = &pdID
	return nil
}

// TransitionTo performs a general transition with validation against the transition table.
func (w *WorkflowRun) TransitionTo(target WorkflowStatus, reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.transitionTo(target, reason, actor, now)
}

// Kill transitions from any non-terminal state to killed.
func (w *WorkflowRun) Kill(reason string, actor shared.ActorID, now shared.Timestamp) error {
	if w.Status.IsTerminal() {
		return fmt.Errorf("%w: cannot kill from %s", ErrTerminalState, w.Status)
	}
	return w.transitionTo(StatusKilled, reason, actor, now)
}

// Pause transitions from running → paused.
func (w *WorkflowRun) Pause(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.transitionTo(StatusPaused, reason, actor, now)
}

// Resume transitions from paused → running.
func (w *WorkflowRun) Resume(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.transitionTo(StatusRunning, reason, actor, now)
}

// Complete transitions from running → completed.
func (w *WorkflowRun) Complete(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.transitionTo(StatusCompleted, reason, actor, now)
}

// Fail transitions to failed from any state that allows it.
func (w *WorkflowRun) Fail(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.transitionTo(StatusFailed, reason, actor, now)
}

// ReconstructWorkflowRun creates a WorkflowRun from persisted data (repository hydration).
func ReconstructWorkflowRun(
	id shared.WorkflowRunID,
	taskID shared.TaskID,
	status WorkflowStatus,
	routingDecisionID *shared.RoutingDecisionID,
	policyDecisionID *shared.PolicyDecisionID,
	currentStepIndex int,
	transitions []WorkflowTransition,
	createdAt shared.Timestamp,
	updatedAt shared.Timestamp,
) *WorkflowRun {
	return &WorkflowRun{
		ID:                id,
		TaskID:            taskID,
		Status:            status,
		RoutingDecisionID: routingDecisionID,
		PolicyDecisionID:  policyDecisionID,
		CurrentStepIndex:  currentStepIndex,
		Transitions:       transitions,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
}

func (w *WorkflowRun) transitionTo(target WorkflowStatus, reason string, actor shared.ActorID, now shared.Timestamp) error {
	if w.Status.IsTerminal() {
		return fmt.Errorf("%w: cannot transition from %s", ErrTerminalState, w.Status)
	}
	if !IsValidTransition(w.Status, target) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, w.Status, target)
	}

	transition := WorkflowTransition{
		From:      w.Status,
		To:        target,
		Reason:    reason,
		Actor:     actor,
		Timestamp: now,
	}
	w.Transitions = append(w.Transitions, transition)
	w.Status = target
	w.UpdatedAt = now
	return nil
}
