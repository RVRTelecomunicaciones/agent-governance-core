package policy

import "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"

// PolicyContext holds the data needed for policy rule evaluation.
type PolicyContext struct {
	Task   *task.Task
	Action string
}

// PolicyRule is the interface that all policy rules must implement.
type PolicyRule interface {
	ID() string
	Evaluate(ctx PolicyContext) RuleEvaluation
}
