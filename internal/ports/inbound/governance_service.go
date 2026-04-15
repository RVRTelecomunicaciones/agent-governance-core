package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

// SubmitTaskInput holds the data required to submit a new task.
type SubmitTaskInput struct {
	Type     task.TaskType
	Title    string
	Scope    task.TaskScope
	Priority shared.Priority
	Metadata task.TaskMetadata
}

// ProcessTaskResult holds the full pipeline result after processing a task.
type ProcessTaskResult struct {
	Task            *task.Task
	RoutingDecision *routing.RoutingDecision
	PolicyDecision  *policy.PolicyDecision
	WorkflowRun     *workflow.WorkflowRun
}

// GovernanceService defines the main governance pipeline interface.
type GovernanceService interface {
	SubmitTask(ctx context.Context, input SubmitTaskInput) (*task.Task, error)
	ProcessTask(ctx context.Context, input SubmitTaskInput, action string) (*ProcessTaskResult, error)
	RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
	EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
	StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}
