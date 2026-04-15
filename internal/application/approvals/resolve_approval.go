// Package approvals contains the approval resolution use case.
package approvals

import (
	"context"
	"fmt"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

const (
	defaultTimeout    = 5 * time.Minute
	defaultMaxRetries = 3
)

// ApprovalService handles approval resolution.
type ApprovalService struct {
	approvalRepo outbound.ApprovalRequestRepository
	wfRepo       outbound.WorkflowRunRepository
	leaseRepo    outbound.ExecutionLeaseRepository
	idGen        outbound.IDGenerator
	clock        outbound.Clock
	auditRec     outbound.AuditRecorder
	notifier     outbound.GovernanceNotifier
}

// NewApprovalService creates an ApprovalService with all required dependencies.
func NewApprovalService(
	approvalRepo outbound.ApprovalRequestRepository,
	wfRepo outbound.WorkflowRunRepository,
	leaseRepo outbound.ExecutionLeaseRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	auditRec outbound.AuditRecorder,
	notifier outbound.GovernanceNotifier,
) *ApprovalService {
	return &ApprovalService{
		approvalRepo: approvalRepo,
		wfRepo:       wfRepo,
		leaseRepo:    leaseRepo,
		idGen:        idGen,
		clock:        clock,
		auditRec:     auditRec,
		notifier:     notifier,
	}
}

// ResolveApproval approves or denies a pending approval request and transitions the workflow.
func (s *ApprovalService) ResolveApproval(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
	// 1. Load approval request
	req, err := s.approvalRepo.FindByID(ctx, input.ApprovalRequestID)
	if err != nil {
		return nil, fmt.Errorf("loading approval request: %w", err)
	}

	now := s.clock.Now()

	// 2. Resolve
	if input.Approved {
		if err := req.Approve(input.ResolvedBy, input.Reason, now); err != nil {
			return nil, fmt.Errorf("approving request: %w", err)
		}
	} else {
		if err := req.Deny(input.ResolvedBy, input.Reason, now); err != nil {
			return nil, fmt.Errorf("denying request: %w", err)
		}
	}

	// 3. Persist approval
	if err := s.approvalRepo.Update(ctx, req); err != nil {
		return nil, fmt.Errorf("persisting approval: %w", err)
	}

	// 4. Load workflow
	wf, err := s.wfRepo.FindByID(ctx, req.WorkflowRunID())
	if err != nil {
		return nil, fmt.Errorf("loading workflow: %w", err)
	}

	wfID := wf.ID

	// 5/6. Transition workflow
	if input.Approved {
		// awaiting_approval → approved → running
		if err := wf.TransitionTo(workflow.StatusApproved, "approval granted", input.ResolvedBy, now); err != nil {
			return nil, fmt.Errorf("transitioning to approved: %w", err)
		}
		if err := wf.TransitionTo(workflow.StatusRunning, "starting execution", input.ResolvedBy, now); err != nil {
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

		_ = s.notifier.OnExecutionReady(ctx, wf, lease)
	} else {
		// awaiting_approval → failed
		if err := wf.Fail("approval denied: "+input.Reason, input.ResolvedBy, now); err != nil {
			return nil, fmt.Errorf("transitioning to failed: %w", err)
		}
		if err := s.wfRepo.Update(ctx, wf); err != nil {
			return nil, fmt.Errorf("persisting workflow: %w", err)
		}

		_ = s.notifier.OnWorkflowTerminated(ctx, wf, "approval denied")
	}

	// 7. Audit
	outcome := "approved"
	if !input.Approved {
		outcome = "denied"
	}
	_ = s.auditRec.Record(ctx, input.ResolvedBy, "approval_resolved", outcome, audit.NewAuditContext(), nil, &wfID)

	return req, nil
}

// GetPendingApprovals returns all approval requests in pending status.
func (s *ApprovalService) GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error) {
	reqs, err := s.approvalRepo.FindPending(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying pending approvals: %w", err)
	}
	return reqs, nil
}
