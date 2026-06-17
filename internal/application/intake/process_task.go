package intake

import (
	"context"
	"fmt"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
)

// TaskRouter is the minimal interface ProcessTask needs from the routing service.
type TaskRouter interface {
	RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
}

// PolicyEvaluator is the minimal interface ProcessTask needs from the policy service.
type PolicyEvaluator interface {
	EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
}

// WorkflowStarter is the minimal interface ProcessTask needs from the workflow service.
type WorkflowStarter interface {
	StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}

// ProcessTaskService is a thin coordinator that calls each pipeline step in
// sequence and returns all intermediate results. It is NOT an orchestrator —
// it does not bypass governance steps, collapse aggregates, or hide results.
type ProcessTaskService struct {
	submitTask *SubmitTaskService
	router     TaskRouter
	policy     PolicyEvaluator
	workflow   WorkflowStarter
}

// NewProcessTaskService creates a ProcessTaskService with all pipeline dependencies.
func NewProcessTaskService(
	submitTask *SubmitTaskService,
	router TaskRouter,
	policy PolicyEvaluator,
	workflow WorkflowStarter,
) *ProcessTaskService {
	return &ProcessTaskService{
		submitTask: submitTask,
		router:     router,
		policy:     policy,
		workflow:   workflow,
	}
}

// ProcessTask runs the full intake pipeline: submit, route, evaluate policy, start workflow.
// If any step fails, the error is returned immediately — no catch-and-continue.
func (s *ProcessTaskService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	t, err := s.submitTask.SubmitTask(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("submit task: %w", err)
	}

	rd, err := s.router.RouteTask(ctx, t.ID())
	if err != nil {
		return nil, fmt.Errorf("route task: %w", err)
	}

	pd, err := s.policy.EvaluatePolicy(ctx, t.ID(), action)
	if err != nil {
		return nil, fmt.Errorf("evaluate policy: %w", err)
	}

	wf, err := s.workflow.StartWorkflow(ctx, t.ID())
	if err != nil {
		return nil, fmt.Errorf("start workflow: %w", err)
	}

	return &inbound.ProcessTaskResult{
		Task:            t,
		RoutingDecision: rd,
		PolicyDecision:  pd,
		WorkflowRun:     wf,
	}, nil
}
