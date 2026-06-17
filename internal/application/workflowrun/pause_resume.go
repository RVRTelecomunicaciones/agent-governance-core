package workflowrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

var ErrLeaseExhausted = errors.New("cannot resume: execution lease is exhausted")

// PauseWorkflow pauses a running workflow.
func (s *WorkflowRunService) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	wf, err := s.wfRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	now := s.clock.Now()
	if err := wf.Pause(reason, actor, now); err != nil {
		return fmt.Errorf("pausing workflow: %w", err)
	}

	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return fmt.Errorf("persisting workflow: %w", err)
	}

	wfID := wf.ID
	_ = s.audit.Record(ctx, actor, "workflow_paused", "paused", audit.NewAuditContext(), nil, &wfID)

	return nil
}

// ResumeWorkflow resumes a paused workflow, verifying the lease is not exhausted.
func (s *WorkflowRunService) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	wf, err := s.wfRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	lease, err := s.leaseRepo.FindByWorkflowRunID(ctx, id)
	if err != nil {
		return fmt.Errorf("loading lease: %w", err)
	}
	if !lease.HasRetryBudget() {
		return ErrLeaseExhausted
	}

	now := s.clock.Now()
	if err := wf.Resume(reason, actor, now); err != nil {
		return fmt.Errorf("resuming workflow: %w", err)
	}

	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return fmt.Errorf("persisting workflow: %w", err)
	}

	wfID := wf.ID
	_ = s.audit.Record(ctx, actor, "workflow_resumed", "running", audit.NewAuditContext(), nil, &wfID)
	_ = s.notifier.OnExecutionReady(ctx, wf, lease)

	return nil
}
