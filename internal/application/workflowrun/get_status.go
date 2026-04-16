package workflowrun

import (
	"context"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// GetWorkflowStatus returns a workflow run by its ID.
func (s *WorkflowRunService) GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	wf, err := s.wfRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("loading workflow: %w", err)
	}
	return wf, nil
}

// GetWorkflowByTask returns a workflow run by its associated task ID.
func (s *WorkflowRunService) GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	wf, err := s.wfRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("loading workflow by task: %w", err)
	}
	return wf, nil
}

// ListWorkflows returns workflow runs matching the given filter criteria.
func (s *WorkflowRunService) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return s.wfRepo.List(ctx, filter)
}
