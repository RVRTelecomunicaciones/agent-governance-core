package fixtures

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// RoutingDecisionOption configures a test routing decision.
type RoutingDecisionOption func(*routingDecisionConfig)

type routingDecisionConfig struct {
	id               shared.RoutingDecisionID
	taskID           shared.TaskID
	selectedStrategy routing.RoutingStrategy
	selectedRole     routing.AgentRole
}

func defaultRoutingDecisionConfig() routingDecisionConfig {
	id, _ := shared.NewRoutingDecisionID(newULID())
	taskID, _ := shared.NewTaskID(newULID())
	return routingDecisionConfig{
		id:               id,
		taskID:           taskID,
		selectedStrategy: routing.StrategyDirect,
		selectedRole:     routing.RoleImplementer,
	}
}

// WithRoutingDecisionID sets the routing decision ID.
func WithRoutingDecisionID(id shared.RoutingDecisionID) RoutingDecisionOption {
	return func(c *routingDecisionConfig) { c.id = id }
}

// WithRoutingTaskID sets the task ID for the routing decision.
func WithRoutingTaskID(id shared.TaskID) RoutingDecisionOption {
	return func(c *routingDecisionConfig) { c.taskID = id }
}

// WithSelectedStrategy sets the selected routing strategy.
func WithSelectedStrategy(s routing.RoutingStrategy) RoutingDecisionOption {
	return func(c *routingDecisionConfig) { c.selectedStrategy = s }
}

// WithSelectedAgentRole sets the selected agent role.
func WithSelectedAgentRole(r routing.AgentRole) RoutingDecisionOption {
	return func(c *routingDecisionConfig) { c.selectedRole = r }
}

// NewTestRoutingDecision creates a valid RoutingDecision. Panics on error (test-only).
func NewTestRoutingDecision(opts ...RoutingDecisionOption) *routing.RoutingDecision {
	cfg := defaultRoutingDecisionConfig()
	for _, o := range opts {
		o(&cfg)
	}

	evals := []routing.StrategyEvaluation{
		{
			Strategy: routing.StrategyDirect,
			Score:    0.8,
		},
	}

	rd, err := routing.NewRoutingDecision(
		cfg.id,
		cfg.taskID,
		evals,
		cfg.selectedStrategy,
		cfg.selectedRole,
		"test routing",
		nil,
		now(),
	)
	if err != nil {
		panic("fixtures.NewTestRoutingDecision: " + err.Error())
	}
	return rd
}
