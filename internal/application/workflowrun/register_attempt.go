package workflowrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

var ErrTerminalWorkflow = errors.New("cannot register attempt on terminal workflow")

// RegisterAttempt records an execution attempt and transitions the workflow based on the result.
func (s *WorkflowRunService) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	// 1. Load workflow — verify NOT terminal
	wf, err := s.wfRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("loading workflow: %w", err)
	}
	if wf.Status.IsTerminal() {
		return nil, fmt.Errorf("%w: status is %s", ErrTerminalWorkflow, wf.Status)
	}

	// 2. Validate AttemptResult
	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("invalid attempt result: %w", err)
	}

	// 3. Load lease
	lease, err := s.leaseRepo.FindByWorkflowRunID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("loading lease: %w", err)
	}

	// 4. Record attempt on lease
	if err := lease.RecordAttempt(); err != nil {
		return nil, fmt.Errorf("recording attempt on lease: %w", err)
	}

	// 5. Transition based on result
	now := s.clock.Now()
	actor := shared.ActorID("system")

	switch result.Status {
	case execution.AttemptStatusSuccess:
		if err := wf.Complete("attempt succeeded", actor, now); err != nil {
			return nil, fmt.Errorf("completing workflow: %w", err)
		}

	case execution.AttemptStatusFailure:
		if lease.HasRetryBudget() {
			// Stay running — attempt recorded, budget not exhausted
		} else {
			if err := wf.Quarantine("retry budget exhausted", actor, now); err != nil {
				return nil, fmt.Errorf("quarantining workflow: %w", err)
			}
		}

	case execution.AttemptStatusRetry:
		// Stay running — attempt already recorded on lease
	}

	// 6. Persist both
	if err := s.leaseRepo.Update(ctx, lease); err != nil {
		return nil, fmt.Errorf("persisting lease: %w", err)
	}
	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return nil, fmt.Errorf("persisting workflow: %w", err)
	}

	// 7. Build enriched AuditContext with failure telemetry
	auditCtx := audit.NewAuditContext().
		WithLeaseState(lease.AttemptsUsed, lease.RetryBudget.MaxAttempts, lease.TimeElapsed.Milliseconds())

	if result.StrategyUsed != nil {
		auditCtx = auditCtx.WithStrategy(*result.StrategyUsed)
	}
	if result.AgentRole != nil {
		auditCtx = auditCtx.WithAgentRole(*result.AgentRole)
	}
	if result.FailureStage != nil {
		auditCtx = auditCtx.WithFailure(result.FailureStage.String(), *result.FailureCode, *result.Retryable)
	}
	if result.ToolName != nil {
		auditCtx = auditCtx.WithToolName(*result.ToolName)
	}

	// 8. Record audit
	wfID := wf.ID
	_ = s.audit.Record(ctx, actor, "attempt_registered", string(result.Status), auditCtx, nil, &wfID)

	// Dedicated quarantine audit entry — emitted once on effective transition
	if wf.Status == workflow.StatusQuarantined {
		_ = s.audit.Record(ctx, actor, "workflow_quarantined", "budget_exhausted", auditCtx, nil, &wfID)
	}

	// 9. Notify if workflow terminated
	if wf.Status.IsTerminal() {
		reason := string(wf.Status)
		if wf.Status == workflow.StatusQuarantined {
			reason = "quarantined:budget_exhausted"
		}
		_ = s.notifier.OnWorkflowTerminated(ctx, wf, reason)
	}

	// 10. Emit lifecycle metrics
	if wf.Status.IsTerminal() {
		durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
		s.lifecycleMetrics.emitTerminal(ctx, string(wf.Status), durationMs)
	}
	if result.FailureStage != nil {
		category := extractCategory(*result.FailureCode)
		s.lifecycleMetrics.emitFailure(ctx, result.FailureStage.String(), category, *result.Retryable)
	}

	// 11. Observe circuit breaker (signal-only, side-effect)
	if s.breakerRegistry != nil && result.ToolName != nil && result.AgentRole != nil {
		success := result.Status == execution.AttemptStatusSuccess
		_, _ = s.breakerRegistry.Observe(ctx, *result.ToolName, *result.AgentRole, success, now.Time)
	}

	return wf, nil
}
