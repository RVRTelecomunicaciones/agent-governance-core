package workflowrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

var (
	ErrNoRoutingDecision = errors.New("no routing decision found for task — cannot start workflow without routing")
	ErrNoPolicyDecision  = errors.New("no policy decision found for task — cannot start workflow without policy evaluation")
	ErrNoWorkflowRun     = errors.New("no workflow run found for task — workflow must be created by RouteTask first")
)

const (
	defaultTimeout    = 5 * time.Minute
	defaultMaxRetries = 3
)

// StartWorkflow evaluates the policy decision for a task and transitions the workflow accordingly.
//
// NO-BYPASS INVARIANTS:
//   - RoutingDecision MUST exist for the task
//   - PolicyDecision MUST exist for the task
func (s *WorkflowRunService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	// 1. Load task
	tk, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("loading task: %w", err)
	}

	// 2. Load routing decision — NO-BYPASS
	_, err = s.routingRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoRoutingDecision, err)
	}

	// 3. Load policy decision — NO-BYPASS
	policyDecision, err := s.policyRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoPolicyDecision, err)
	}

	// 4. Load workflow run
	wf, err := s.wfRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoWorkflowRun, err)
	}

	now := s.clock.Now()
	actor := shared.ActorID("system")
	tID := tk.ID()
	wfID := wf.ID

	switch policyDecision.Outcome() {
	case policy.OutcomeDeny:
		if err := wf.Fail("policy denied: "+policyDecision.Reason(), actor, now); err != nil {
			return nil, fmt.Errorf("transitioning to failed: %w", err)
		}
		if err := s.wfRepo.Update(ctx, wf); err != nil {
			return nil, fmt.Errorf("persisting workflow: %w", err)
		}
		_ = s.audit.Record(ctx, actor, "workflow_started", "denied", audit.NewAuditContext(), &tID, &wfID)
		_ = s.notifier.OnWorkflowTerminated(ctx, wf, "policy denied")
		durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
		s.lifecycleMetrics.emitTerminal(ctx, "failed", durationMs)
		return wf, nil

	case policy.OutcomeRequireApproval:
		if err := wf.TransitionTo(workflow.StatusAwaitingApproval, "approval required: "+policyDecision.Reason(), actor, now); err != nil {
			return nil, fmt.Errorf("transitioning to awaiting_approval: %w", err)
		}

		approverRole := ""
		if ar := policyDecision.ApprovalRequirement(); ar != nil {
			approverRole = ar.ApproverRole
		}

		approvalReq, err := approval.NewApprovalRequest(
			s.idGen.NewApprovalRequestID(),
			tk.ID(),
			wf.ID,
			policyDecision.Reason(),
			approval.ApproverSpec{Role: approverRole},
			nil, // no expiry in phase 1
			now,
		)
		if err != nil {
			return nil, fmt.Errorf("creating approval request: %w", err)
		}

		if err := s.approvalRepo.Save(ctx, approvalReq); err != nil {
			return nil, fmt.Errorf("persisting approval request: %w", err)
		}
		if err := s.wfRepo.Update(ctx, wf); err != nil {
			return nil, fmt.Errorf("persisting workflow: %w", err)
		}
		_ = s.audit.Record(ctx, actor, "workflow_started", "awaiting_approval", audit.NewAuditContext(), &tID, &wfID)
		_ = s.notifier.OnApprovalRequired(ctx, wf, approvalReq)
		return wf, nil

	case policy.OutcomeAllow, policy.OutcomeAllowWithConstraints:
		if err := wf.TransitionTo(workflow.StatusRunning, "policy allowed", actor, now); err != nil {
			return nil, fmt.Errorf("transitioning to running: %w", err)
		}

		retryBudget, err := execution.NewRetryBudget(defaultMaxRetries)
		if err != nil {
			return nil, fmt.Errorf("creating retry budget: %w", err)
		}

		lease, err := execution.NewExecutionLease(
			s.idGen.NewExecutionLeaseID(),
			wf.ID,
			defaultTimeout,
			retryBudget,
			now,
		)
		if err != nil {
			return nil, fmt.Errorf("creating execution lease: %w", err)
		}

		if err := s.leaseRepo.Save(ctx, lease); err != nil {
			return nil, fmt.Errorf("persisting lease: %w", err)
		}
		if err := s.wfRepo.Update(ctx, wf); err != nil {
			return nil, fmt.Errorf("persisting workflow: %w", err)
		}
		_ = s.audit.Record(ctx, actor, "workflow_started", "running", audit.NewAuditContext(), &tID, &wfID)
		_ = s.notifier.OnExecutionReady(ctx, wf, lease)
		return wf, nil

	default:
		return nil, fmt.Errorf("unknown policy outcome: %v", policyDecision.Outcome())
	}
}
