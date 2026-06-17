// Package intake contains the use cases for task intake and processing.
package intake

import (
	"context"
	"fmt"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

// SubmitTaskService handles the intake of new tasks into the governance pipeline.
type SubmitTaskService struct {
	taskRepo outbound.TaskRepository
	idGen    outbound.IDGenerator
	clock    outbound.Clock
	audit    outbound.AuditRecorder
	memory   outbound.MemoryContextProvider // can be nil
}

// NewSubmitTaskService creates a SubmitTaskService with required dependencies.
// memory may be nil — the service degrades gracefully without it.
func NewSubmitTaskService(
	taskRepo outbound.TaskRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	audit outbound.AuditRecorder,
	memory outbound.MemoryContextProvider,
) *SubmitTaskService {
	return &SubmitTaskService{
		taskRepo: taskRepo,
		idGen:    idGen,
		clock:    clock,
		audit:    audit,
		memory:   memory,
	}
}

// SubmitTask creates a new task, persists it, and records an audit entry.
func (s *SubmitTaskService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	id := s.idGen.NewTaskID()
	now := s.clock.Now()

	risk := classifyRisk(input.Type, input.Scope)

	// Optionally enrich via memory context — degradable, errors are ignored.
	if s.memory != nil {
		_, _ = s.memory.GetRelevantContext(ctx, id, input.Title)
	}

	t, err := task.NewTask(id, nil, input.Type, input.Title, input.Scope, input.Priority, risk, input.Metadata, now)
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	if err := s.taskRepo.Save(ctx, t); err != nil {
		return nil, fmt.Errorf("persisting task: %w", err)
	}

	taskID := t.ID()
	if err := s.audit.Record(ctx, shared.ActorID("system"), "task_submitted", "created", audit.NewAuditContext(), &taskID, nil); err != nil {
		return nil, fmt.Errorf("recording audit: %w", err)
	}

	return t, nil
}

// classifyRisk determines the risk level based on task type and scope.
// This is a simple deterministic classification — not a full scoring system.
func classifyRisk(taskType task.TaskType, scope task.TaskScope) shared.RiskLevel {
	switch {
	case taskType == task.TypeDeployment && scope == task.ScopeSystem:
		return shared.RiskLevelCritical
	case taskType == task.TypeDeployment && scope == task.ScopeRepo:
		return shared.RiskLevelHigh
	case scope == task.ScopeSystem:
		return shared.RiskLevelHigh
	case taskType == task.TypeRefactor && scope == task.ScopeRepo:
		return shared.RiskLevelMedium
	case taskType == task.TypeBugfix && scope == task.ScopeFile:
		return shared.RiskLevelLow
	default:
		return shared.RiskLevelMedium
	}
}
