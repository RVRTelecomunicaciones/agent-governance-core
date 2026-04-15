package policyeval

import (
	"context"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// EvaluatePolicyService orchestrates policy evaluation for a task action.
type EvaluatePolicyService struct {
	taskRepo    outbound.TaskRepository
	routingRepo outbound.RoutingDecisionRepository
	policyRepo  outbound.PolicyDecisionRepository
	wfRepo      outbound.WorkflowRunRepository
	idGen       outbound.IDGenerator
	clock       outbound.Clock
	audit       outbound.AuditRecorder
}

// NewEvaluatePolicyService creates a new EvaluatePolicyService with all required dependencies.
func NewEvaluatePolicyService(
	taskRepo outbound.TaskRepository,
	routingRepo outbound.RoutingDecisionRepository,
	policyRepo outbound.PolicyDecisionRepository,
	wfRepo outbound.WorkflowRunRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	audit outbound.AuditRecorder,
) *EvaluatePolicyService {
	return &EvaluatePolicyService{
		taskRepo:    taskRepo,
		routingRepo: routingRepo,
		policyRepo:  policyRepo,
		wfRepo:      wfRepo,
		idGen:       idGen,
		clock:       clock,
		audit:       audit,
	}
}

// EvaluatePolicy runs policy evaluation for the given task and action.
// NO-BYPASS INVARIANT: a RoutingDecision must exist for this task.
func (s *EvaluatePolicyService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	// 1. Load task
	t, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("loading task: %w", err)
	}

	// 2. Load routing decision — NO-BYPASS INVARIANT
	_, err = s.routingRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("routing decision required before policy evaluation: %w", err)
	}

	// 3. Evaluate policy
	result := policy.EvaluatePolicy(policy.DefaultRules(), policy.PolicyContext{
		Task:   t,
		Action: action,
	})

	// 4. Create policy decision
	now := s.clock.Now()
	pdID := s.idGen.NewPolicyDecisionID()
	pd, err := policy.NewPolicyDecision(
		pdID,
		taskID,
		action,
		result.Outcome,
		result.Constraints,
		result.ApprovalRequirement,
		result.RulesEvaluated,
		result.Reason,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating policy decision: %w", err)
	}

	// 5. Persist policy decision
	if err := s.policyRepo.Save(ctx, pd); err != nil {
		return nil, fmt.Errorf("saving policy decision: %w", err)
	}

	// 6. Load WorkflowRun and transition to policy_checked
	wf, err := s.wfRepo.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("loading workflow run: %w", err)
	}

	systemActor := shared.ActorID("system")
	if err := wf.MarkPolicyChecked(pdID, systemActor, now); err != nil {
		return nil, fmt.Errorf("marking workflow policy_checked: %w", err)
	}
	if err := s.wfRepo.Update(ctx, wf); err != nil {
		return nil, fmt.Errorf("updating workflow run: %w", err)
	}

	// 7. Record audit entry
	auditCtx := audit.NewAuditContext().
		Set("evaluated_action", action).
		Set("policy_outcome", result.Outcome.String())
	if err := s.audit.Record(ctx, systemActor, "policy_evaluated", result.Outcome.String(), auditCtx, &taskID, &wf.ID); err != nil {
		return nil, fmt.Errorf("recording audit: %w", err)
	}

	return pd, nil
}
