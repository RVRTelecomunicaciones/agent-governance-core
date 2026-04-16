package routing

import (
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

// EvaluatorInput holds all data needed for routing evaluation.
type EvaluatorInput struct {
	Task          *task.Task
	MemoryContext *MemoryContext
	FailureStats  *FailureStats // nil means no adaptation
}

// EvaluatorResult holds the outcome of routing evaluation.
type EvaluatorResult struct {
	Evaluations      []StrategyEvaluation
	SelectedStrategy RoutingStrategy
	SelectedRole     AgentRole
	Reason           string
}

// strategies evaluated in scoring phase (collaborate is phase 2).
var evaluatedStrategies = []RoutingStrategy{
	StrategyDirect,
	StrategyDecompose,
	StrategyEscalate,
}

// defaultRoleMapping maps each strategy to its default agent role.
var defaultRoleMapping = map[RoutingStrategy]AgentRole{
	StrategyDirect:    RoleImplementer,
	StrategyDecompose: RoleArchitect,
	StrategyEscalate:  RoleHuman,
}

// Evaluate runs the three-phase routing evaluation: overrides, scoring, tiebreaker.
func Evaluate(input EvaluatorInput) EvaluatorResult {
	t := input.Task

	// Phase 1: Hard overrides
	if result, ok := checkOverrides(t); ok {
		return result
	}

	// Phase 2: Score-based evaluation
	evals := scoreStrategies(t, input.MemoryContext)

	// Phase 2.5: Adaptive adjustment
	evals = applyAdaptiveAdjustments(evals, input.FailureStats)

	// Phase 3: Tiebreaker — uses AdjustedScore now
	selected := tiebreak(evals)

	role := defaultRoleMapping[selected.Strategy]
	reason := fmt.Sprintf("score-based: %s scored %.4f", selected.Strategy, selected.Score)
	if selected.AdaptiveAdjustment != nil {
		reason = fmt.Sprintf("[adaptive] score-based: %s scored %.4f → adjusted to %.4f (failure_rate=%.2f, confidence=%.2f)",
			selected.Strategy, selected.Score, selected.AdjustedScore,
			selected.AdaptiveAdjustment.FailureRate, selected.AdaptiveAdjustment.Confidence)
	}

	return EvaluatorResult{
		Evaluations:      evals,
		SelectedStrategy: selected.Strategy,
		SelectedRole:     role,
		Reason:           reason,
	}
}

// checkOverrides applies hard override rules that short-circuit scoring.
func checkOverrides(t *task.Task) (EvaluatorResult, bool) {
	// Override 1: critical risk → escalate
	if t.RiskLevel() == shared.RiskLevelCritical {
		return overrideResult(StrategyEscalate, "[override] risk_level is critical"), true
	}

	// Override 2: system-scope deployment → escalate
	if t.Scope() == task.ScopeSystem && t.Type() == task.TypeDeployment {
		return overrideResult(StrategyEscalate, "[override] system-scope deployment"), true
	}

	// Override 3: force_strategy in metadata
	meta := t.Metadata()
	if forceRaw, ok := meta["force_strategy"]; ok {
		if forceStr, ok := forceRaw.(string); ok {
			strategy, err := NewRoutingStrategy(forceStr)
			if err == nil {
				return overrideResult(strategy, fmt.Sprintf("[override] force_strategy=%s", strategy)), true
			}
		}
	}

	return EvaluatorResult{}, false
}

func overrideResult(strategy RoutingStrategy, reason string) EvaluatorResult {
	role := defaultRoleMapping[strategy]
	eval := StrategyEvaluation{
		Strategy:      strategy,
		Score:         1.0,
		AdjustedScore: 1.0, // overrides are not affected by adaptive
		FactorScores:  map[string]float64{},
		Overridden:    true,
		Reason:        reason,
	}
	return EvaluatorResult{
		Evaluations:      []StrategyEvaluation{eval},
		SelectedStrategy: strategy,
		SelectedRole:     role,
		Reason:           reason,
	}
}

// scoreStrategies computes weighted scores for each evaluable strategy.
func scoreStrategies(t *task.Task, memCtx *MemoryContext) []StrategyEvaluation {
	evals := make([]StrategyEvaluation, 0, len(evaluatedStrategies))

	for _, strategy := range evaluatedStrategies {
		factors := map[string]float64{
			"risk_level":           scoreRiskLevel(t.RiskLevel(), strategy),
			"task_scope":           scoreTaskScope(t.Scope(), strategy),
			"task_type":            scoreTaskType(t.Type(), strategy),
			"task_priority":        scoreTaskPriority(t.Priority(), strategy),
			"memory_similar_tasks": scoreMemorySimilarTasks(memCtx, strategy),
			"memory_heuristics":    scoreMemoryHeuristics(memCtx, strategy),
		}

		score := factors["risk_level"]*WeightRiskLevel +
			factors["task_scope"]*WeightTaskScope +
			factors["task_type"]*WeightTaskType +
			factors["task_priority"]*WeightTaskPriority +
			factors["memory_similar_tasks"]*WeightMemorySimilar +
			factors["memory_heuristics"]*WeightMemoryHeuristics

		evals = append(evals, StrategyEvaluation{
			Strategy:      strategy,
			Score:         score,
			AdjustedScore: score, // default before adaptive
			FactorScores:  factors,
			Overridden:    false,
			Reason:        fmt.Sprintf("%s scored %.4f", strategy, score),
		})
	}

	return evals
}

// tiebreak selects the winning strategy. Highest score wins.
// On tie, prefer simpler: direct > decompose > escalate.
func tiebreak(evals []StrategyEvaluation) StrategyEvaluation {
	// Strategy simplicity order for tiebreaking
	simplicity := map[RoutingStrategy]int{
		StrategyDirect:    0, // simplest
		StrategyDecompose: 1,
		StrategyEscalate:  2,
	}

	best := evals[0]
	for _, e := range evals[1:] {
		if e.AdjustedScore > best.AdjustedScore {
			best = e
		} else if e.AdjustedScore == best.AdjustedScore && simplicity[e.Strategy] < simplicity[best.Strategy] {
			best = e
		}
	}
	return best
}
