package workflowrun

import (
	"context"
	"fmt"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// KillWorkflow terminates a workflow run from any non-terminal state.
func (s *WorkflowRunService) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	wf, err := s.wfRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	now := s.clock.Now()
	if err := wf.Kill(reason, actor, now); err != nil {
		return fmt.Errorf("killing workflow: %w", err)
	}

	// Revoke lease if one exists — ignore not-found.
	lease, leaseErr := s.leaseRepo.FindByWorkflowRunID(ctx, id)
	if leaseErr == nil && lease != nil {
		if revokeErr := lease.Revoke(); revokeErr == nil {
			_ = s.leaseRepo.Update(ctx, lease)
		}
	}

	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return fmt.Errorf("persisting workflow: %w", err)
	}

	wfID := wf.ID
	_ = s.audit.Record(ctx, actor, "workflow_killed", "killed", audit.NewAuditContext(), nil, &wfID)
	_ = s.notifier.OnWorkflowTerminated(ctx, wf, reason)

	durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
	s.lifecycleMetrics.emitTerminal(ctx, "killed", durationMs)

	return nil
}
