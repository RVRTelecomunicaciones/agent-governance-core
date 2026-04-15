package routing

import "fmt"

// --- RoutingStrategy enum ---

// RoutingStrategy determines how a task should be executed.
type RoutingStrategy string

const (
	StrategyDirect      RoutingStrategy = "direct"
	StrategyDecompose   RoutingStrategy = "decompose"
	StrategyCollaborate RoutingStrategy = "collaborate" // phase 2 — defined but not implemented
	StrategyEscalate    RoutingStrategy = "escalate"
)

var validStrategies = map[RoutingStrategy]bool{
	StrategyDirect:      true,
	StrategyDecompose:   true,
	StrategyCollaborate: true,
	StrategyEscalate:    true,
}

func NewRoutingStrategy(s string) (RoutingStrategy, error) {
	rs := RoutingStrategy(s)
	if !validStrategies[rs] {
		return "", fmt.Errorf("invalid routing strategy: %q", s)
	}
	return rs, nil
}

func (rs RoutingStrategy) String() string { return string(rs) }

// --- AgentRole enum ---

// AgentRole identifies which kind of agent should handle a task.
type AgentRole string

const (
	RoleImplementer AgentRole = "implementer"
	RoleReviewer    AgentRole = "reviewer"
	RoleResearcher  AgentRole = "researcher"
	RoleArchitect   AgentRole = "architect"
	RoleHuman       AgentRole = "human"
)

var validAgentRoles = map[AgentRole]bool{
	RoleImplementer: true,
	RoleReviewer:    true,
	RoleResearcher:  true,
	RoleArchitect:   true,
	RoleHuman:       true,
}

func NewAgentRole(s string) (AgentRole, error) {
	ar := AgentRole(s)
	if !validAgentRoles[ar] {
		return "", fmt.Errorf("invalid agent role: %q", s)
	}
	return ar, nil
}

func (ar AgentRole) String() string { return string(ar) }

// --- StrategyEvaluation VO ---

// StrategyEvaluation captures the scoring result for a single strategy.
type StrategyEvaluation struct {
	Strategy     RoutingStrategy
	Score        float64
	FactorScores map[string]float64
	Overridden   bool
	Reason       string
}

// --- RoutingConstraint VO ---

// RoutingConstraint represents a constraint applied to a routing decision.
type RoutingConstraint struct {
	Type  string
	Value string
}
