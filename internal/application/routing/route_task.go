package routing

import (
	"context"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// RouteTaskService orchestrates the routing evaluation for a task.
type RouteTaskService struct {
	taskRepo    outbound.TaskRepository
	routingRepo outbound.RoutingDecisionRepository
	wfRepo      outbound.WorkflowRunRepository
	idGen       outbound.IDGenerator
	clock       outbound.Clock
	audit       outbound.AuditRecorder
	memory      outbound.MemoryContextProvider // can be nil — degradable
}

// NewRouteTaskService creates a new RouteTaskService with all required dependencies.
// memory can be nil — context enrichment will be skipped.
func NewRouteTaskService(
	taskRepo outbound.TaskRepository,
	routingRepo outbound.RoutingDecisionRepository,
	wfRepo outbound.WorkflowRunRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	audit outbound.AuditRecorder,
	memory outbound.MemoryContextProvider,
) *RouteTaskService {
	return &RouteTaskService{
		taskRepo:    taskRepo,
		routingRepo: routingRepo,
		wfRepo:      wfRepo,
		idGen:       idGen,
		clock:       clock,
		audit:       audit,
		memory:      memory,
	}
}

// RouteTask evaluates routing for the given task and persists the decision.
func (s *RouteTaskService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	// 1. Load task
	t, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("loading task: %w", err)
	}

	// 2. Query memory context (degradable)
	var memCtx *routing.MemoryContext
	if s.memory != nil {
		memCtx, err = s.memory.GetRelevantContext(ctx, taskID, t.Title())
		if err != nil {
			// Memory is degradable — proceed with nil context
			memCtx = nil
		}
	}

	// 3. Evaluate routing
	result := routing.Evaluate(routing.EvaluatorInput{
		Task:          t,
		MemoryContext: memCtx,
	})

	// 4. Create routing decision
	now := s.clock.Now()
	rdID := s.idGen.NewRoutingDecisionID()
	rd, err := routing.NewRoutingDecision(
		rdID,
		taskID,
		result.Evaluations,
		result.SelectedStrategy,
		result.SelectedRole,
		result.Reason,
		nil, // no constraints in phase 1
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating routing decision: %w", err)
	}

	// 5. Persist routing decision
	if err := s.routingRepo.Save(ctx, rd); err != nil {
		return nil, fmt.Errorf("saving routing decision: %w", err)
	}

	// 6. Load or create WorkflowRun
	wf, err := s.wfRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		// WorkflowRun not found — create one
		wfID := s.idGen.NewWorkflowRunID()
		wf = workflow.NewWorkflowRun(wfID, taskID, now)
		if err := s.wfRepo.Save(ctx, wf); err != nil {
			return nil, fmt.Errorf("saving new workflow run: %w", err)
		}
	}

	// 7. Transition to routed
	systemActor := shared.ActorID("system")
	if err := wf.MarkRouted(rdID, result.Reason, systemActor, now); err != nil {
		return nil, fmt.Errorf("marking workflow routed: %w", err)
	}
	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return nil, fmt.Errorf("updating workflow run: %w", err)
	}

	// 8. Record audit entry
	auditCtx := audit.NewAuditContext().
		WithStrategy(string(result.SelectedStrategy)).
		WithAgentRole(string(result.SelectedRole))
	if err := s.audit.Record(ctx, systemActor, "task_routed", string(result.SelectedStrategy), auditCtx, &taskID, &wf.ID); err != nil {
		return nil, fmt.Errorf("recording audit: %w", err)
	}

	// 9. Handle decompose strategy — create subtasks
	if result.SelectedStrategy == routing.StrategyDecompose {
		if err := s.createDecomposeSubtasks(ctx, t, now); err != nil {
			return nil, fmt.Errorf("creating decompose subtasks: %w", err)
		}
	}

	return rd, nil
}

// childScope returns the scope one level down from the parent scope.
func childScope(parent task.TaskScope) task.TaskScope {
	switch parent {
	case task.ScopeSystem:
		return task.ScopeRepo
	case task.ScopeRepo:
		return task.ScopeModule
	case task.ScopeModule:
		return task.ScopeFile
	default:
		return task.ScopeFile
	}
}

// subtaskSuffix returns the descriptive suffixes for the two subtasks based on parent scope.
func subtaskSuffixes(parent task.TaskScope) (string, string) {
	switch parent {
	case task.ScopeSystem:
		return "implementation", "review"
	case task.ScopeRepo:
		return "implementation", "testing"
	default:
		return "part 1", "part 2"
	}
}

// createDecomposeSubtasks creates two subtasks for a decomposed parent task.
func (s *RouteTaskService) createDecomposeSubtasks(ctx context.Context, parent *task.Task, now shared.Timestamp) error {
	childScp := childScope(parent.Scope())
	suffix1, suffix2 := subtaskSuffixes(parent.Scope())
	parentID := parent.ID()
	systemActor := shared.ActorID("system")

	for _, suffix := range []string{suffix1, suffix2} {
		subID := s.idGen.NewTaskID()
		title := fmt.Sprintf("[decompose] %s - %s", parent.Title(), suffix)

		sub, err := task.NewTask(
			subID,
			&parentID,
			parent.Type(),
			title,
			childScp,
			parent.Priority(),
			parent.RiskLevel(),
			nil,
			now,
		)
		if err != nil {
			return fmt.Errorf("creating subtask %q: %w", suffix, err)
		}

		if err := s.taskRepo.Save(ctx, sub); err != nil {
			return fmt.Errorf("persisting subtask %q: %w", suffix, err)
		}

		auditCtx := audit.NewAuditContext().
			Set("parent_task_id", parentID.String())
		if err := s.audit.Record(ctx, systemActor, "subtask_created", suffix, auditCtx, &subID, nil); err != nil {
			return fmt.Errorf("auditing subtask %q: %w", suffix, err)
		}
	}

	return nil
}
