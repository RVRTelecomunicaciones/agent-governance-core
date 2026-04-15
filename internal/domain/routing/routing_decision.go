package routing

import (
	"errors"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// Errors for the routing decision aggregate.
var (
	ErrMissingTaskID     = errors.New("routing decision must reference a task")
	ErrNoEvaluations     = errors.New("routing decision must have at least one evaluated strategy")
	ErrEmptyReason       = errors.New("routing decision must have a reason")
)

// RoutingDecision is an IMMUTABLE aggregate representing the outcome of routing evaluation.
type RoutingDecision struct {
	id                  shared.RoutingDecisionID
	taskID              shared.TaskID
	evaluatedStrategies []StrategyEvaluation
	selectedStrategy    RoutingStrategy
	selectedAgentRole   AgentRole
	reason              string
	constraints         []RoutingConstraint
	createdAt           shared.Timestamp
}

// NewRoutingDecision creates a validated, immutable routing decision.
func NewRoutingDecision(
	id shared.RoutingDecisionID,
	taskID shared.TaskID,
	evaluatedStrategies []StrategyEvaluation,
	selectedStrategy RoutingStrategy,
	selectedAgentRole AgentRole,
	reason string,
	constraints []RoutingConstraint,
	createdAt shared.Timestamp,
) (*RoutingDecision, error) {
	if taskID == "" {
		return nil, ErrMissingTaskID
	}
	if len(evaluatedStrategies) == 0 {
		return nil, ErrNoEvaluations
	}
	if reason == "" {
		return nil, ErrEmptyReason
	}

	// Defensive copies
	evals := make([]StrategyEvaluation, len(evaluatedStrategies))
	for i, e := range evaluatedStrategies {
		fs := make(map[string]float64, len(e.FactorScores))
		for k, v := range e.FactorScores {
			fs[k] = v
		}
		evals[i] = StrategyEvaluation{
			Strategy:     e.Strategy,
			Score:        e.Score,
			FactorScores: fs,
			Overridden:   e.Overridden,
			Reason:       e.Reason,
		}
	}

	cons := make([]RoutingConstraint, len(constraints))
	copy(cons, constraints)

	return &RoutingDecision{
		id:                  id,
		taskID:              taskID,
		evaluatedStrategies: evals,
		selectedStrategy:    selectedStrategy,
		selectedAgentRole:   selectedAgentRole,
		reason:              reason,
		constraints:         cons,
		createdAt:           createdAt,
	}, nil
}

// Reconstruct creates a RoutingDecision from persisted state without validation.
// Intended for repository hydration only.
func Reconstruct(
	id shared.RoutingDecisionID,
	taskID shared.TaskID,
	evaluatedStrategies []StrategyEvaluation,
	selectedStrategy RoutingStrategy,
	selectedAgentRole AgentRole,
	reason string,
	constraints []RoutingConstraint,
	createdAt shared.Timestamp,
) *RoutingDecision {
	return &RoutingDecision{
		id:                  id,
		taskID:              taskID,
		evaluatedStrategies: evaluatedStrategies,
		selectedStrategy:    selectedStrategy,
		selectedAgentRole:   selectedAgentRole,
		reason:              reason,
		constraints:         constraints,
		createdAt:           createdAt,
	}
}

// --- Accessors ---

func (d *RoutingDecision) ID() shared.RoutingDecisionID        { return d.id }
func (d *RoutingDecision) TaskID() shared.TaskID                { return d.taskID }
func (d *RoutingDecision) SelectedStrategy() RoutingStrategy    { return d.selectedStrategy }
func (d *RoutingDecision) SelectedAgentRole() AgentRole         { return d.selectedAgentRole }
func (d *RoutingDecision) Reason() string                       { return d.reason }
func (d *RoutingDecision) CreatedAt() shared.Timestamp          { return d.createdAt }

// EvaluatedStrategies returns a defensive copy of the evaluated strategies.
func (d *RoutingDecision) EvaluatedStrategies() []StrategyEvaluation {
	cp := make([]StrategyEvaluation, len(d.evaluatedStrategies))
	copy(cp, d.evaluatedStrategies)
	return cp
}

// Constraints returns a defensive copy of the constraints.
func (d *RoutingDecision) Constraints() []RoutingConstraint {
	cp := make([]RoutingConstraint, len(d.constraints))
	copy(cp, d.constraints)
	return cp
}
