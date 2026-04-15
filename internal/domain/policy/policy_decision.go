package policy

import (
	"errors"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

var (
	ErrEmptyAction              = errors.New("evaluated action must not be empty")
	ErrMissingConstraints       = errors.New("allow_with_constraints outcome requires at least one constraint")
	ErrMissingApproval          = errors.New("require_approval outcome requires an approval requirement")
	ErrEmptyReason              = errors.New("reason must not be empty")
)

// PolicyDecision is an immutable aggregate representing the result of policy evaluation for a task action.
type PolicyDecision struct {
	id                  shared.PolicyDecisionID
	taskID              shared.TaskID
	evaluatedAction     string
	outcome             PolicyOutcome
	constraints         []PolicyConstraint
	approvalRequirement *ApprovalRequirement
	rulesEvaluated      []RuleEvaluation
	reason              string
	createdAt           shared.Timestamp
}

// NewPolicyDecision creates a new PolicyDecision with full validation.
func NewPolicyDecision(
	id shared.PolicyDecisionID,
	taskID shared.TaskID,
	evaluatedAction string,
	outcome PolicyOutcome,
	constraints []PolicyConstraint,
	approvalRequirement *ApprovalRequirement,
	rulesEvaluated []RuleEvaluation,
	reason string,
	createdAt shared.Timestamp,
) (*PolicyDecision, error) {
	if evaluatedAction == "" {
		return nil, ErrEmptyAction
	}
	if reason == "" {
		return nil, ErrEmptyReason
	}
	if outcome == OutcomeAllowWithConstraints && len(constraints) == 0 {
		return nil, ErrMissingConstraints
	}
	if outcome == OutcomeRequireApproval && approvalRequirement == nil {
		return nil, ErrMissingApproval
	}

	cs := make([]PolicyConstraint, len(constraints))
	copy(cs, constraints)

	evals := make([]RuleEvaluation, len(rulesEvaluated))
	copy(evals, rulesEvaluated)

	return &PolicyDecision{
		id:                  id,
		taskID:              taskID,
		evaluatedAction:     evaluatedAction,
		outcome:             outcome,
		constraints:         cs,
		approvalRequirement: approvalRequirement,
		rulesEvaluated:      evals,
		reason:              reason,
		createdAt:           createdAt,
	}, nil
}

// Reconstruct creates a PolicyDecision from persisted state without validation.
// Intended for repository hydration only.
func Reconstruct(
	id shared.PolicyDecisionID,
	taskID shared.TaskID,
	evaluatedAction string,
	outcome PolicyOutcome,
	constraints []PolicyConstraint,
	approvalRequirement *ApprovalRequirement,
	rulesEvaluated []RuleEvaluation,
	reason string,
	createdAt shared.Timestamp,
) *PolicyDecision {
	cs := make([]PolicyConstraint, len(constraints))
	copy(cs, constraints)

	evals := make([]RuleEvaluation, len(rulesEvaluated))
	copy(evals, rulesEvaluated)

	return &PolicyDecision{
		id:                  id,
		taskID:              taskID,
		evaluatedAction:     evaluatedAction,
		outcome:             outcome,
		constraints:         cs,
		approvalRequirement: approvalRequirement,
		rulesEvaluated:      evals,
		reason:              reason,
		createdAt:           createdAt,
	}
}

// --- Accessors ---

func (d *PolicyDecision) ID() shared.PolicyDecisionID        { return d.id }
func (d *PolicyDecision) TaskID() shared.TaskID               { return d.taskID }
func (d *PolicyDecision) EvaluatedAction() string             { return d.evaluatedAction }
func (d *PolicyDecision) Outcome() PolicyOutcome              { return d.outcome }
func (d *PolicyDecision) ApprovalRequirement() *ApprovalRequirement { return d.approvalRequirement }
func (d *PolicyDecision) Reason() string                      { return d.reason }
func (d *PolicyDecision) CreatedAt() shared.Timestamp         { return d.createdAt }

// Constraints returns a defensive copy of the constraints slice.
func (d *PolicyDecision) Constraints() []PolicyConstraint {
	cp := make([]PolicyConstraint, len(d.constraints))
	copy(cp, d.constraints)
	return cp
}

// RulesEvaluated returns a defensive copy of the rule evaluations.
func (d *PolicyDecision) RulesEvaluated() []RuleEvaluation {
	cp := make([]RuleEvaluation, len(d.rulesEvaluated))
	copy(cp, d.rulesEvaluated)
	return cp
}
