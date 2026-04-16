package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adapter "github.com/russellcxl/agent-governance-core/internal/adapters/inbound/http"
	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	escalationdomain "github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementations ---

type mockGovernanceService struct {
	submitTaskFn     func(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error)
	processTaskFn    func(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error)
	routeTaskFn      func(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
	evaluatePolicyFn func(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
	startWorkflowFn  func(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}

func (m *mockGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	if m.submitTaskFn != nil {
		return m.submitTaskFn(ctx, input)
	}
	return nil, nil
}
func (m *mockGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	if m.processTaskFn != nil {
		return m.processTaskFn(ctx, input, action)
	}
	return nil, nil
}
func (m *mockGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	if m.routeTaskFn != nil {
		return m.routeTaskFn(ctx, taskID)
	}
	return nil, nil
}
func (m *mockGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	if m.evaluatePolicyFn != nil {
		return m.evaluatePolicyFn(ctx, taskID, action)
	}
	return nil, nil
}
func (m *mockGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	if m.startWorkflowFn != nil {
		return m.startWorkflowFn(ctx, taskID)
	}
	return nil, nil
}

type mockWorkflowControl struct {
	killFn    func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	pauseFn   func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	resumeFn  func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	attemptFn func(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error)
}

func (m *mockWorkflowControl) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	if m.killFn != nil {
		return m.killFn(ctx, id, reason, actor)
	}
	return nil
}
func (m *mockWorkflowControl) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	if m.pauseFn != nil {
		return m.pauseFn(ctx, id, reason, actor)
	}
	return nil
}
func (m *mockWorkflowControl) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	if m.resumeFn != nil {
		return m.resumeFn(ctx, id, reason, actor)
	}
	return nil
}
func (m *mockWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	if m.attemptFn != nil {
		return m.attemptFn(ctx, id, result)
	}
	return nil, nil
}

type mockApprovalService struct {
	resolveFn    func(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error)
	getPendingFn func(ctx context.Context) ([]*approval.ApprovalRequest, error)
}

func (m *mockApprovalService) ResolveApproval(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
	if m.resolveFn != nil {
		return m.resolveFn(ctx, input)
	}
	return nil, nil
}
func (m *mockApprovalService) GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error) {
	if m.getPendingFn != nil {
		return m.getPendingFn(ctx)
	}
	return nil, nil
}

type mockQueryService struct {
	getTaskFn     func(ctx context.Context, id shared.TaskID) (*task.Task, error)
	getWorkflowFn func(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	getWfByTaskFn func(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	queryAuditFn  func(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error)
}

func (m *mockQueryService) GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	if m.getTaskFn != nil {
		return m.getTaskFn(ctx, id)
	}
	return nil, nil
}
func (m *mockQueryService) GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	if m.getWorkflowFn != nil {
		return m.getWorkflowFn(ctx, id)
	}
	return nil, nil
}
func (m *mockQueryService) GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	if m.getWfByTaskFn != nil {
		return m.getWfByTaskFn(ctx, taskID)
	}
	return nil, nil
}
func (m *mockQueryService) QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
	if m.queryAuditFn != nil {
		return m.queryAuditFn(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockQueryService) ListWorkflows(_ context.Context, _ outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, nil
}

type mockEscalationService struct {
	triggerFn func(ctx context.Context, taskID shared.TaskID, condition escalationdomain.EscalationCondition, target escalationdomain.EscalationTarget) (*escalationdomain.EscalationTrigger, error)
}

func (m *mockEscalationService) TriggerEscalation(ctx context.Context, taskID shared.TaskID, condition escalationdomain.EscalationCondition, target escalationdomain.EscalationTarget) (*escalationdomain.EscalationTrigger, error) {
	if m.triggerFn != nil {
		return m.triggerFn(ctx, taskID, condition, target)
	}
	return nil, nil
}

// --- Helpers ---

// Valid ULID for tests
const testULID = "01KP7XXH5Z4F9W4P14SNKPF787"

func newTestServer(
	gov *mockGovernanceService,
	ctrl *mockWorkflowControl,
	approvals *mockApprovalService,
	queries *mockQueryService,
	opts ...func(*mockEscalationService),
) *adapter.Server {
	if gov == nil {
		gov = &mockGovernanceService{}
	}
	if ctrl == nil {
		ctrl = &mockWorkflowControl{}
	}
	if approvals == nil {
		approvals = &mockApprovalService{}
	}
	if queries == nil {
		queries = &mockQueryService{}
	}
	esc := &mockEscalationService{}
	for _, o := range opts {
		o(esc)
	}
	return adapter.NewServer(gov, ctrl, approvals, queries, esc)
}

func doRequest(srv http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func parseErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) adapter.ErrorResponse {
	t.Helper()
	var errResp adapter.ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	return errResp
}

// --- Tests ---

func TestSubmitTask_ValidRequest(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	fakeTask, _ := task.NewTask(
		shared.TaskID(testULID), nil, task.TypeDevelopment, "test task",
		task.ScopeFile, shared.PriorityNormal, shared.RiskLevelLow, nil, now,
	)

	gov := &mockGovernanceService{
		submitTaskFn: func(_ context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
			assert.Equal(t, task.TypeDevelopment, input.Type)
			assert.Equal(t, task.ScopeFile, input.Scope)
			assert.Equal(t, shared.PriorityNormal, input.Priority)
			return fakeTask, nil
		},
	}

	srv := newTestServer(gov, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks", map[string]any{
		"type":     "development",
		"title":    "test task",
		"scope":    "file",
		"priority": "normal",
	})

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, testULID, resp["id"])
	assert.Equal(t, "development", resp["type"])
}

func TestSubmitTask_InvalidTaskType(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks", map[string]any{
		"type":     "invalid_type",
		"title":    "test",
		"scope":    "file",
		"priority": "normal",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_TASK_TYPE", errResp.Code)
}

func TestSubmitTask_InvalidScope(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks", map[string]any{
		"type":     "development",
		"title":    "test",
		"scope":    "galaxy",
		"priority": "normal",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_TASK_SCOPE", errResp.Code)
}

func TestSubmitTask_InvalidPriority(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks", map[string]any{
		"type":     "development",
		"title":    "test",
		"scope":    "file",
		"priority": "mega",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_PRIORITY", errResp.Code)
}

func TestSubmitTask_InvalidJSON(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_JSON", errResp.Code)
}

func TestGetTask_Valid(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	fakeTask, _ := task.NewTask(
		shared.TaskID(testULID), nil, task.TypeDevelopment, "test task",
		task.ScopeFile, shared.PriorityNormal, shared.RiskLevelLow, nil, now,
	)

	queries := &mockQueryService{
		getTaskFn: func(_ context.Context, id shared.TaskID) (*task.Task, error) {
			assert.Equal(t, shared.TaskID(testULID), id)
			return fakeTask, nil
		},
	}

	srv := newTestServer(nil, nil, nil, queries)
	rec := doRequest(srv, "GET", "/api/v1/tasks/"+testULID, nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, testULID, resp["id"])
}

func TestGetTask_InvalidID(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "GET", "/api/v1/tasks/not-a-ulid", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_TASK_ID", errResp.Code)
}

func TestRouteTask_Valid(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	rdID, _ := shared.NewRoutingDecisionID(testULID)
	fakeRD := routing.Reconstruct(
		rdID,
		shared.TaskID(testULID),
		[]routing.StrategyEvaluation{{Strategy: routing.StrategyDirect, Score: 1.0, FactorScores: map[string]float64{"complexity": 0.2}}},
		routing.StrategyDirect,
		routing.RoleImplementer,
		"simple task",
		nil,
		now,
	)

	gov := &mockGovernanceService{
		routeTaskFn: func(_ context.Context, _ shared.TaskID) (*routing.RoutingDecision, error) {
			return fakeRD, nil
		},
	}

	srv := newTestServer(gov, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks/"+testULID+"/route", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "direct", resp["selected_strategy"])
}

func TestKillWorkflow_Valid(t *testing.T) {
	var calledWith struct {
		reason string
		actor  shared.ActorID
	}
	ctrl := &mockWorkflowControl{
		killFn: func(_ context.Context, _ shared.WorkflowRunID, reason string, actor shared.ActorID) error {
			calledWith.reason = reason
			calledWith.actor = actor
			return nil
		},
	}

	srv := newTestServer(nil, ctrl, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/workflows/"+testULID+"/kill", map[string]any{
		"reason": "test kill",
		"actor":  "admin",
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "test kill", calledWith.reason)
	assert.Equal(t, shared.ActorID("admin"), calledWith.actor)
}

func TestGetPendingApprovals_ReturnsList(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	fakeApproval, _ := approval.NewApprovalRequest(
		shared.ApprovalRequestID(testULID),
		shared.TaskID(testULID),
		shared.WorkflowRunID(testULID),
		"needs review",
		approval.ApproverSpec{Role: "lead"},
		nil,
		now,
	)

	approvalsSvc := &mockApprovalService{
		getPendingFn: func(_ context.Context) ([]*approval.ApprovalRequest, error) {
			return []*approval.ApprovalRequest{fakeApproval}, nil
		},
	}

	srv := newTestServer(nil, nil, approvalsSvc, nil)
	rec := doRequest(srv, "GET", "/api/v1/approvals/pending", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp []map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp, 1)
	assert.Equal(t, "pending", resp[0]["status"])
}

func TestResolveApproval_Valid(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	fakeApproval, _ := approval.NewApprovalRequest(
		shared.ApprovalRequestID(testULID),
		shared.TaskID(testULID),
		shared.WorkflowRunID(testULID),
		"needs review",
		approval.ApproverSpec{Role: "lead"},
		nil,
		now,
	)
	// Approve it so the response has a resolution
	_ = fakeApproval.Approve(shared.ActorID("admin"), "looks good", now)

	approvalsSvc := &mockApprovalService{
		resolveFn: func(_ context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
			assert.Equal(t, shared.ApprovalRequestID(testULID), input.ApprovalRequestID)
			assert.True(t, input.Approved)
			return fakeApproval, nil
		},
	}

	srv := newTestServer(nil, nil, approvalsSvc, nil)
	rec := doRequest(srv, "POST", "/api/v1/approvals/"+testULID+"/resolve", map[string]any{
		"approved":    true,
		"resolved_by": "admin",
		"reason":      "looks good",
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "approved", resp["status"])
}

func TestQueryAuditTrail_WithFilters(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	entry := audit.ReconstructAuditEntry(
		shared.AuditEntryID(testULID),
		nil, nil,
		shared.ActorID("system"),
		"task.submitted",
		"success",
		audit.NewAuditContext(),
		now,
	)

	queries := &mockQueryService{
		queryAuditFn: func(_ context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
			assert.NotNil(t, filter.Actor)
			assert.Equal(t, shared.ActorID("system"), *filter.Actor)
			assert.Equal(t, 10, filter.Limit)
			return []*audit.AuditEntry{entry}, 1, nil
		},
	}

	srv := newTestServer(nil, nil, nil, queries)
	rec := doRequest(srv, "GET", "/api/v1/audit?actor=system&limit=10", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["total"])
	entries := resp["entries"].([]any)
	require.Len(t, entries, 1)
}

func TestEnumValidationAtBoundary(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		wantCode string
	}{
		{
			name:     "bad task type",
			body:     map[string]any{"type": "hacking", "title": "x", "scope": "file", "priority": "normal"},
			wantCode: "INVALID_TASK_TYPE",
		},
		{
			name:     "bad scope",
			body:     map[string]any{"type": "development", "title": "x", "scope": "universe", "priority": "normal"},
			wantCode: "INVALID_TASK_SCOPE",
		},
		{
			name:     "bad priority",
			body:     map[string]any{"type": "development", "title": "x", "scope": "file", "priority": "insane"},
			wantCode: "INVALID_PRIORITY",
		},
	}

	srv := newTestServer(nil, nil, nil, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(srv, "POST", "/api/v1/tasks", tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			errResp := parseErrorResponse(t, rec)
			assert.Equal(t, tc.wantCode, errResp.Code)
		})
	}
}

func TestGetWorkflowStatus_InvalidID(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "GET", "/api/v1/workflows/bad-id", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_WORKFLOW_ID", errResp.Code)
}

func TestRegisterAttempt_InvalidStatus(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/workflows/"+testULID+"/attempts", map[string]any{
		"status": "unknown_status",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_ATTEMPT_STATUS", errResp.Code)
}

func TestRegisterAttempt_FailureMissingStage(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/workflows/"+testULID+"/attempts", map[string]any{
		"status": "failure",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "MISSING_FAILURE_STAGE", errResp.Code)
}

func TestRegisterAttempt_InvalidFailureStage(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/workflows/"+testULID+"/attempts", map[string]any{
		"status":        "failure",
		"failure_stage": "nonexistent_stage",
		"failure_code":  "ERR001",
		"retryable":     false,
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_FAILURE_STAGE", errResp.Code)
}

func TestTriggerEscalation_Valid(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	condition := escalationdomain.EscalationCondition{Type: "timeout", Parameters: map[string]any{"threshold": 300}}
	trigger := escalationdomain.NewEscalationTrigger(
		shared.EscalationTriggerID(testULID),
		shared.TaskID(testULID),
		condition,
		escalationdomain.TargetHuman,
		now,
	)
	_ = trigger.Trigger(now)

	srv := newTestServer(nil, nil, nil, nil, func(m *mockEscalationService) {
		m.triggerFn = func(_ context.Context, taskID shared.TaskID, cond escalationdomain.EscalationCondition, target escalationdomain.EscalationTarget) (*escalationdomain.EscalationTrigger, error) {
			assert.Equal(t, shared.TaskID(testULID), taskID)
			assert.Equal(t, "timeout", cond.Type)
			assert.Equal(t, escalationdomain.TargetHuman, target)
			return trigger, nil
		}
	})

	rec := doRequest(srv, "POST", "/api/v1/tasks/"+testULID+"/escalate", map[string]any{
		"condition_type": "timeout",
		"condition_params": map[string]any{"threshold": 300},
		"target":         "human",
	})

	assert.Equal(t, http.StatusCreated, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, testULID, resp["id"])
	assert.Equal(t, "triggered", resp["status"])
	assert.Equal(t, "human", resp["target"])
}

func TestTriggerEscalation_InvalidTarget(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks/"+testULID+"/escalate", map[string]any{
		"condition_type": "timeout",
		"target":         "nobody",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_ESCALATION_TARGET", errResp.Code)
}

func TestTriggerEscalation_MissingConditionType(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil)
	rec := doRequest(srv, "POST", "/api/v1/tasks/"+testULID+"/escalate", map[string]any{
		"target": "human",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "MISSING_CONDITION_TYPE", errResp.Code)
}
