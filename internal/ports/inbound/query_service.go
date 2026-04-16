package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// QueryService defines the read/query interface for the governance system.
type QueryService interface {
	GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error)
	GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error)
	ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error)
	ListBreakers(ctx context.Context, filter resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error)
	GetBreakerState(ctx context.Context, tool, agentRole string) (*resilience.BreakerSnapshot, error)
}
