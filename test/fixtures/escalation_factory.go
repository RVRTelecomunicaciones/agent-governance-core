package fixtures

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// EscalationTriggerOption configures a test escalation trigger.
type EscalationTriggerOption func(*escalationTriggerConfig)

type escalationTriggerConfig struct {
	id        shared.EscalationTriggerID
	taskID    shared.TaskID
	condition escalation.EscalationCondition
	target    escalation.EscalationTarget
}

func defaultEscalationTriggerConfig() escalationTriggerConfig {
	id, _ := shared.NewEscalationTriggerID(newULID())
	taskID, _ := shared.NewTaskID(newULID())
	return escalationTriggerConfig{
		id:     id,
		taskID: taskID,
		condition: escalation.EscalationCondition{
			Type:       "retries_exhausted",
			Parameters: nil,
		},
		target: escalation.TargetHuman,
	}
}

// WithEscalationTriggerID sets the escalation trigger ID.
func WithEscalationTriggerID(id shared.EscalationTriggerID) EscalationTriggerOption {
	return func(c *escalationTriggerConfig) { c.id = id }
}

// WithEscalationTaskID sets the task ID for the escalation trigger.
func WithEscalationTaskID(id shared.TaskID) EscalationTriggerOption {
	return func(c *escalationTriggerConfig) { c.taskID = id }
}

// WithEscalationCondition sets the escalation condition.
func WithEscalationCondition(cond escalation.EscalationCondition) EscalationTriggerOption {
	return func(c *escalationTriggerConfig) { c.condition = cond }
}

// WithEscalationTarget sets the escalation target.
func WithEscalationTarget(t escalation.EscalationTarget) EscalationTriggerOption {
	return func(c *escalationTriggerConfig) { c.target = t }
}

// NewTestEscalationTrigger creates a valid pending EscalationTrigger.
func NewTestEscalationTrigger(opts ...EscalationTriggerOption) *escalation.EscalationTrigger {
	cfg := defaultEscalationTriggerConfig()
	for _, o := range opts {
		o(&cfg)
	}

	return escalation.NewEscalationTrigger(
		cfg.id,
		cfg.taskID,
		cfg.condition,
		cfg.target,
		now(),
	)
}
