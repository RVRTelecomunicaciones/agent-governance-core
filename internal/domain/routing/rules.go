package routing

import (
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
)

// MemoryContext provides routing enrichment from the memory engine.
type MemoryContext struct {
	SimilarTaskFailureRate float64
	HasRelevantHeuristics  bool
}

// Weight constants for scoring factors. Sum = 1.0.
const (
	WeightRiskLevel        = 0.30
	WeightTaskScope        = 0.25
	WeightTaskType         = 0.20
	WeightTaskPriority     = 0.10
	WeightMemorySimilar    = 0.10
	WeightMemoryHeuristics = 0.05
)

// --- Scoring factor functions ---
// Each returns a score 0.0-1.0 for a given strategy.

func scoreRiskLevel(risk shared.RiskLevel, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyDirect:
		switch risk {
		case shared.RiskLevelLow:
			return 0.9
		case shared.RiskLevelMedium:
			return 0.6
		case shared.RiskLevelHigh:
			return 0.3
		case shared.RiskLevelCritical:
			return 0.1
		}
	case StrategyDecompose:
		switch risk {
		case shared.RiskLevelLow:
			return 0.4
		case shared.RiskLevelMedium:
			return 0.7
		case shared.RiskLevelHigh:
			return 0.8
		case shared.RiskLevelCritical:
			return 0.5
		}
	case StrategyEscalate:
		switch risk {
		case shared.RiskLevelLow:
			return 0.1
		case shared.RiskLevelMedium:
			return 0.4
		case shared.RiskLevelHigh:
			return 0.7
		case shared.RiskLevelCritical:
			return 1.0
		}
	}
	return 0.5
}

func scoreTaskScope(scope task.TaskScope, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyDirect:
		switch scope {
		case task.ScopeFile:
			return 0.9
		case task.ScopeModule:
			return 0.7
		case task.ScopeRepo:
			return 0.4
		case task.ScopeSystem:
			return 0.2
		}
	case StrategyDecompose:
		switch scope {
		case task.ScopeFile:
			return 0.3
		case task.ScopeModule:
			return 0.6
		case task.ScopeRepo:
			return 0.8
		case task.ScopeSystem:
			return 0.7
		}
	case StrategyEscalate:
		switch scope {
		case task.ScopeFile:
			return 0.1
		case task.ScopeModule:
			return 0.3
		case task.ScopeRepo:
			return 0.5
		case task.ScopeSystem:
			return 0.8
		}
	}
	return 0.5
}

func scoreTaskType(tt task.TaskType, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyDirect:
		switch tt {
		case task.TypeBugfix:
			return 0.8
		case task.TypeDevelopment:
			return 0.7
		case task.TypeReview:
			return 0.6
		case task.TypeResearch:
			return 0.5
		case task.TypeRefactor:
			return 0.4
		case task.TypeDeployment:
			return 0.2
		}
	case StrategyDecompose:
		switch tt {
		case task.TypeBugfix:
			return 0.3
		case task.TypeDevelopment:
			return 0.6
		case task.TypeReview:
			return 0.4
		case task.TypeResearch:
			return 0.7
		case task.TypeRefactor:
			return 0.8
		case task.TypeDeployment:
			return 0.5
		}
	case StrategyEscalate:
		switch tt {
		case task.TypeBugfix:
			return 0.2
		case task.TypeDevelopment:
			return 0.3
		case task.TypeReview:
			return 0.4
		case task.TypeResearch:
			return 0.3
		case task.TypeRefactor:
			return 0.4
		case task.TypeDeployment:
			return 0.9
		}
	}
	return 0.5
}

func scoreTaskPriority(p shared.Priority, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyDirect:
		switch p {
		case shared.PriorityLow:
			return 0.7
		case shared.PriorityNormal:
			return 0.7
		case shared.PriorityHigh:
			return 0.5
		case shared.PriorityUrgent:
			return 0.4
		}
	case StrategyDecompose:
		switch p {
		case shared.PriorityLow:
			return 0.5
		case shared.PriorityNormal:
			return 0.5
		case shared.PriorityHigh:
			return 0.6
		case shared.PriorityUrgent:
			return 0.5
		}
	case StrategyEscalate:
		switch p {
		case shared.PriorityLow:
			return 0.2
		case shared.PriorityNormal:
			return 0.3
		case shared.PriorityHigh:
			return 0.6
		case shared.PriorityUrgent:
			return 0.8
		}
	}
	return 0.5
}

func scoreMemorySimilarTasks(ctx *MemoryContext, strategy RoutingStrategy) float64 {
	if ctx == nil {
		return 0.5
	}
	// High failure rate pushes toward escalate
	switch strategy {
	case StrategyDirect:
		return 1.0 - ctx.SimilarTaskFailureRate
	case StrategyDecompose:
		return 0.5 + ctx.SimilarTaskFailureRate*0.3
	case StrategyEscalate:
		return ctx.SimilarTaskFailureRate
	}
	return 0.5
}

func scoreMemoryHeuristics(ctx *MemoryContext, strategy RoutingStrategy) float64 {
	if ctx == nil {
		return 0.5
	}
	if ctx.HasRelevantHeuristics {
		// Having heuristics favors direct execution slightly
		switch strategy {
		case StrategyDirect:
			return 0.8
		case StrategyDecompose:
			return 0.6
		case StrategyEscalate:
			return 0.3
		}
	}
	return 0.5
}
