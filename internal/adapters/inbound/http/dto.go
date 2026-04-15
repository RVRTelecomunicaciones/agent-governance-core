package http

import (
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

// --- Request DTOs ---

type submitTaskRequest struct {
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Scope    string         `json:"scope"`
	Priority string         `json:"priority"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Action   string         `json:"action,omitempty"`
}

type evaluatePolicyRequest struct {
	Action string `json:"action"`
}

type workflowActionRequest struct {
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

type registerAttemptRequest struct {
	Status       string  `json:"status"`
	FailureStage *string `json:"failure_stage,omitempty"`
	FailureCode  *string `json:"failure_code,omitempty"`
	Retryable    *bool   `json:"retryable,omitempty"`
	ToolName     *string `json:"tool_name,omitempty"`
	StrategyUsed *string `json:"strategy_used,omitempty"`
	AgentRole    *string `json:"agent_role,omitempty"`
	Detail       *string `json:"detail,omitempty"`
}

type resolveApprovalRequest struct {
	Approved   bool   `json:"approved"`
	ResolvedBy string `json:"resolved_by"`
	Reason     string `json:"reason"`
}

// --- Response DTOs ---

type taskResponse struct {
	ID           string         `json:"id"`
	ParentTaskID *string        `json:"parent_task_id,omitempty"`
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Scope        string         `json:"scope"`
	Priority     string         `json:"priority"`
	RiskLevel    string         `json:"risk_level"`
	Status       string         `json:"status"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

func taskToResponse(t *task.Task) taskResponse {
	resp := taskResponse{
		ID:        t.ID().String(),
		Type:      string(t.Type()),
		Title:     t.Title(),
		Scope:     string(t.Scope()),
		Priority:  string(t.Priority()),
		RiskLevel: string(t.RiskLevel()),
		Status:    string(t.Status()),
		Metadata:  t.Metadata(),
		CreatedAt: t.CreatedAt().Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt().Format(time.RFC3339),
	}
	if t.ParentTaskID() != nil {
		s := t.ParentTaskID().String()
		resp.ParentTaskID = &s
	}
	return resp
}

type workflowTransitionResponse struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason"`
	Actor     string `json:"actor"`
	Timestamp string `json:"timestamp"`
}

type workflowRunResponse struct {
	ID                string                       `json:"id"`
	TaskID            string                       `json:"task_id"`
	Status            string                       `json:"status"`
	RoutingDecisionID *string                      `json:"routing_decision_id,omitempty"`
	PolicyDecisionID  *string                      `json:"policy_decision_id,omitempty"`
	CurrentStepIndex  int                          `json:"current_step_index"`
	Transitions       []workflowTransitionResponse `json:"transitions"`
	CreatedAt         string                       `json:"created_at"`
	UpdatedAt         string                       `json:"updated_at"`
}

func workflowRunToResponse(wf *workflow.WorkflowRun) workflowRunResponse {
	resp := workflowRunResponse{
		ID:               wf.ID.String(),
		TaskID:           wf.TaskID.String(),
		Status:           string(wf.Status),
		CurrentStepIndex: wf.CurrentStepIndex,
		Transitions:      make([]workflowTransitionResponse, 0, len(wf.Transitions)),
		CreatedAt:        wf.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        wf.UpdatedAt.Format(time.RFC3339),
	}
	if wf.RoutingDecisionID != nil {
		s := wf.RoutingDecisionID.String()
		resp.RoutingDecisionID = &s
	}
	if wf.PolicyDecisionID != nil {
		s := wf.PolicyDecisionID.String()
		resp.PolicyDecisionID = &s
	}
	for _, tr := range wf.Transitions {
		resp.Transitions = append(resp.Transitions, workflowTransitionResponse{
			From:      string(tr.From),
			To:        string(tr.To),
			Reason:    tr.Reason,
			Actor:     tr.Actor.String(),
			Timestamp: tr.Timestamp.Format(time.RFC3339),
		})
	}
	return resp
}

type strategyEvaluationResponse struct {
	Strategy     string             `json:"strategy"`
	Score        float64            `json:"score"`
	FactorScores map[string]float64 `json:"factor_scores"`
	Overridden   bool               `json:"overridden"`
	Reason       string             `json:"reason"`
}

type routingConstraintResponse struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type routingDecisionResponse struct {
	ID                  string                       `json:"id"`
	TaskID              string                       `json:"task_id"`
	EvaluatedStrategies []strategyEvaluationResponse `json:"evaluated_strategies"`
	SelectedStrategy    string                       `json:"selected_strategy"`
	SelectedAgentRole   string                       `json:"selected_agent_role"`
	Reason              string                       `json:"reason"`
	Constraints         []routingConstraintResponse  `json:"constraints"`
	CreatedAt           string                       `json:"created_at"`
}

func routingDecisionToResponse(rd *routing.RoutingDecision) routingDecisionResponse {
	evals := make([]strategyEvaluationResponse, 0, len(rd.EvaluatedStrategies()))
	for _, e := range rd.EvaluatedStrategies() {
		evals = append(evals, strategyEvaluationResponse{
			Strategy:     string(e.Strategy),
			Score:        e.Score,
			FactorScores: e.FactorScores,
			Overridden:   e.Overridden,
			Reason:       e.Reason,
		})
	}
	cons := make([]routingConstraintResponse, 0, len(rd.Constraints()))
	for _, c := range rd.Constraints() {
		cons = append(cons, routingConstraintResponse{Type: c.Type, Value: c.Value})
	}
	return routingDecisionResponse{
		ID:                  rd.ID().String(),
		TaskID:              rd.TaskID().String(),
		EvaluatedStrategies: evals,
		SelectedStrategy:    string(rd.SelectedStrategy()),
		SelectedAgentRole:   string(rd.SelectedAgentRole()),
		Reason:              rd.Reason(),
		Constraints:         cons,
		CreatedAt:           rd.CreatedAt().Format(time.RFC3339),
	}
}

type policyConstraintResponse struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type approvalRequirementResponse struct {
	ApproverRole string `json:"approver_role"`
	Reason       string `json:"reason"`
	TimeoutSecs  *int   `json:"timeout_secs,omitempty"`
}

type ruleEvaluationResponse struct {
	RuleID  string  `json:"rule_id"`
	Passed  bool    `json:"passed"`
	Outcome *string `json:"outcome,omitempty"`
	Reason  string  `json:"reason"`
}

type policyDecisionResponse struct {
	ID                  string                       `json:"id"`
	TaskID              string                       `json:"task_id"`
	EvaluatedAction     string                       `json:"evaluated_action"`
	Outcome             string                       `json:"outcome"`
	Constraints         []policyConstraintResponse   `json:"constraints"`
	ApprovalRequirement *approvalRequirementResponse `json:"approval_requirement,omitempty"`
	RulesEvaluated      []ruleEvaluationResponse     `json:"rules_evaluated"`
	Reason              string                       `json:"reason"`
	CreatedAt           string                       `json:"created_at"`
}

func policyDecisionToResponse(pd *policy.PolicyDecision) policyDecisionResponse {
	cons := make([]policyConstraintResponse, 0, len(pd.Constraints()))
	for _, c := range pd.Constraints() {
		cons = append(cons, policyConstraintResponse{Type: c.Type, Value: c.Value, Reason: c.Reason})
	}
	rules := make([]ruleEvaluationResponse, 0, len(pd.RulesEvaluated()))
	for _, r := range pd.RulesEvaluated() {
		re := ruleEvaluationResponse{RuleID: r.RuleID, Passed: r.Passed, Reason: r.Reason}
		if r.Outcome != nil {
			s := r.Outcome.String()
			re.Outcome = &s
		}
		rules = append(rules, re)
	}
	resp := policyDecisionResponse{
		ID:              pd.ID().String(),
		TaskID:          pd.TaskID().String(),
		EvaluatedAction: pd.EvaluatedAction(),
		Outcome:         pd.Outcome().String(),
		Constraints:     cons,
		RulesEvaluated:  rules,
		Reason:          pd.Reason(),
		CreatedAt:       pd.CreatedAt().Format(time.RFC3339),
	}
	if ar := pd.ApprovalRequirement(); ar != nil {
		resp.ApprovalRequirement = &approvalRequirementResponse{
			ApproverRole: ar.ApproverRole,
			Reason:       ar.Reason,
			TimeoutSecs:  ar.TimeoutSecs,
		}
	}
	return resp
}

type approverSpecResponse struct {
	Role    string  `json:"role"`
	ActorID *string `json:"actor_id,omitempty"`
}

type approvalResolutionResponse struct {
	ResolvedBy string `json:"resolved_by"`
	ResolvedAt string `json:"resolved_at"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
}

type approvalRequestResponse struct {
	ID               string                      `json:"id"`
	TaskID           string                      `json:"task_id"`
	WorkflowRunID    string                      `json:"workflow_run_id"`
	Reason           string                      `json:"reason"`
	RequiredApprover approverSpecResponse        `json:"required_approver"`
	Status           string                      `json:"status"`
	Resolution       *approvalResolutionResponse `json:"resolution,omitempty"`
	ExpiresAt        *string                     `json:"expires_at,omitempty"`
	CreatedAt        string                      `json:"created_at"`
}

func approvalRequestToResponse(ar *approval.ApprovalRequest) approvalRequestResponse {
	spec := ar.RequiredApprover()
	approver := approverSpecResponse{Role: spec.Role}
	if spec.ActorID != nil {
		s := spec.ActorID.String()
		approver.ActorID = &s
	}
	resp := approvalRequestResponse{
		ID:               ar.ID().String(),
		TaskID:           ar.TaskID().String(),
		WorkflowRunID:    ar.WorkflowRunID().String(),
		Reason:           ar.Reason(),
		RequiredApprover: approver,
		Status:           string(ar.Status()),
		CreatedAt:        ar.CreatedAt().Format(time.RFC3339),
	}
	if res := ar.Resolution(); res != nil {
		resp.Resolution = &approvalResolutionResponse{
			ResolvedBy: res.ResolvedBy.String(),
			ResolvedAt: res.ResolvedAt.Format(time.RFC3339),
			Reason:     res.Reason,
			Status:     string(res.Status),
		}
	}
	if exp := ar.ExpiresAt(); exp != nil {
		s := exp.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	return resp
}

type auditEntryResponse struct {
	ID            string         `json:"id"`
	TaskID        *string        `json:"task_id,omitempty"`
	WorkflowRunID *string        `json:"workflow_run_id,omitempty"`
	Actor         string         `json:"actor"`
	Action        string         `json:"action"`
	Outcome       string         `json:"outcome"`
	Context       map[string]any `json:"context"`
	CreatedAt     string         `json:"created_at"`
}

func auditEntryToResponse(ae *audit.AuditEntry) auditEntryResponse {
	resp := auditEntryResponse{
		ID:        ae.ID().String(),
		Actor:     ae.Actor().String(),
		Action:    ae.Action(),
		Outcome:   ae.Outcome(),
		Context:   ae.Context(),
		CreatedAt: ae.CreatedAt().Format(time.RFC3339),
	}
	if tid := ae.TaskID(); tid != nil {
		s := tid.String()
		resp.TaskID = &s
	}
	if wid := ae.WorkflowRunID(); wid != nil {
		s := wid.String()
		resp.WorkflowRunID = &s
	}
	return resp
}

type processTaskResponse struct {
	Task            *taskResponse            `json:"task,omitempty"`
	RoutingDecision *routingDecisionResponse `json:"routing_decision,omitempty"`
	PolicyDecision  *policyDecisionResponse  `json:"policy_decision,omitempty"`
	WorkflowRun     *workflowRunResponse     `json:"workflow_run,omitempty"`
}

func processTaskResultToResponse(r *inbound.ProcessTaskResult) processTaskResponse {
	resp := processTaskResponse{}
	if r.Task != nil {
		tr := taskToResponse(r.Task)
		resp.Task = &tr
	}
	if r.RoutingDecision != nil {
		rd := routingDecisionToResponse(r.RoutingDecision)
		resp.RoutingDecision = &rd
	}
	if r.PolicyDecision != nil {
		pd := policyDecisionToResponse(r.PolicyDecision)
		resp.PolicyDecision = &pd
	}
	if r.WorkflowRun != nil {
		wf := workflowRunToResponse(r.WorkflowRun)
		resp.WorkflowRun = &wf
	}
	return resp
}
