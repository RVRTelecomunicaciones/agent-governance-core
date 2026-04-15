package fixtures

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

// TaskOption configures a test task.
type TaskOption func(*taskConfig)

type taskConfig struct {
	id        shared.TaskID
	taskType  task.TaskType
	title     string
	scope     task.TaskScope
	priority  shared.Priority
	riskLevel shared.RiskLevel
	parentID  *shared.TaskID
	metadata  task.TaskMetadata
}

func defaultTaskConfig() taskConfig {
	id, _ := shared.NewTaskID(newULID())
	return taskConfig{
		id:        id,
		taskType:  task.TypeDevelopment,
		title:     "Test task",
		scope:     task.ScopeFile,
		priority:  shared.PriorityNormal,
		riskLevel: shared.RiskLevelLow,
		parentID:  nil,
		metadata:  nil,
	}
}

// WithTaskID sets the task ID.
func WithTaskID(id shared.TaskID) TaskOption {
	return func(c *taskConfig) { c.id = id }
}

// WithTaskType sets the task type.
func WithTaskType(tt task.TaskType) TaskOption {
	return func(c *taskConfig) { c.taskType = tt }
}

// WithTaskTitle sets the task title.
func WithTaskTitle(title string) TaskOption {
	return func(c *taskConfig) { c.title = title }
}

// WithTaskScope sets the task scope.
func WithTaskScope(scope task.TaskScope) TaskOption {
	return func(c *taskConfig) { c.scope = scope }
}

// WithTaskPriority sets the task priority.
func WithTaskPriority(p shared.Priority) TaskOption {
	return func(c *taskConfig) { c.priority = p }
}

// WithTaskRiskLevel sets the task risk level.
func WithTaskRiskLevel(rl shared.RiskLevel) TaskOption {
	return func(c *taskConfig) { c.riskLevel = rl }
}

// WithTaskParent sets the parent task ID.
func WithTaskParent(parentID shared.TaskID) TaskOption {
	return func(c *taskConfig) { c.parentID = &parentID }
}

// WithTaskMetadata sets the task metadata.
func WithTaskMetadata(m task.TaskMetadata) TaskOption {
	return func(c *taskConfig) { c.metadata = m }
}

// NewTestTask creates a valid Task with sensible defaults. Panics on error (test-only).
func NewTestTask(opts ...TaskOption) *task.Task {
	cfg := defaultTaskConfig()
	for _, o := range opts {
		o(&cfg)
	}

	t, err := task.NewTask(
		cfg.id,
		cfg.parentID,
		cfg.taskType,
		cfg.title,
		cfg.scope,
		cfg.priority,
		cfg.riskLevel,
		cfg.metadata,
		now(),
	)
	if err != nil {
		panic("fixtures.NewTestTask: " + err.Error())
	}
	return t
}
