package workflowrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

// --- test setup helpers ---

type testHarness struct {
	svc          *WorkflowRunService
	wfRepo       *mockWfRepo
	leaseRepo    *mockLeaseRepo
	taskRepo     *mockTaskRepo
	routingRepo  *mockRoutingRepo
	policyRepo   *mockPolicyRepo
	approvalRepo *mockApprovalRepo
	idGen        *mockIDGen
	clock        *mockClock
	audit        *mockAuditRecorder
	notifier     *mockNotifier
}

func newTestHarness() *testHarness {
	h := &testHarness{
		wfRepo:       newMockWfRepo(),
		leaseRepo:    newMockLeaseRepo(),
		taskRepo:     newMockTaskRepo(),
		routingRepo:  newMockRoutingRepo(),
		policyRepo:   newMockPolicyRepo(),
		approvalRepo: newMockApprovalRepo(),
		idGen:        newMockIDGen(),
		clock:        newMockClock(),
		audit:        newMockAuditRecorder(),
		notifier:     newMockNotifier(),
	}
	h.svc = NewWorkflowRunService(
		h.wfRepo, h.leaseRepo, h.taskRepo, h.routingRepo, h.policyRepo,
		h.approvalRepo, h.idGen, h.clock, h.audit, h.notifier, nil,
	)
	return h
}

// seedPolicyCheckedWorkflow creates a task, routing decision, policy decision, and workflow
// in the policy_checked state. Returns (task, workflow, policyDecision).
func (h *testHarness) seedPolicyCheckedWorkflow(t *testing.T, outcome policy.PolicyOutcome) (shared.TaskID, *workflow.WorkflowRun) {
	t.Helper()
	ctx := context.Background()

	taskID := h.idGen.NewTaskID()
	now := h.clock.Now()
	actor := shared.ActorID("test-system")

	// Create task
	tk, err := newTestTask(taskID, now)
	require.NoError(t, err)
	require.NoError(t, h.taskRepo.Save(ctx, tk))

	// Routing decision
	rdID := h.idGen.NewRoutingDecisionID()
	rd, err := routing.NewRoutingDecision(
		rdID, taskID,
		[]routing.StrategyEvaluation{{Strategy: routing.StrategyDirect, Score: 0.8}},
		routing.StrategyDirect, routing.RoleImplementer, "test routing", nil, now,
	)
	require.NoError(t, err)
	require.NoError(t, h.routingRepo.Save(ctx, rd))

	// Policy decision
	pdID := h.idGen.NewPolicyDecisionID()
	var constraints []policy.PolicyConstraint
	var approvalReq *policy.ApprovalRequirement

	if outcome == policy.OutcomeAllowWithConstraints {
		constraints = []policy.PolicyConstraint{{Type: "scope", Value: "read_only", Reason: "test"}}
	}
	if outcome == policy.OutcomeRequireApproval {
		approvalReq = &policy.ApprovalRequirement{ApproverRole: "human", Reason: "test approval"}
	}

	allowOutcome := policy.OutcomeAllow
	pd, err := policy.NewPolicyDecision(
		pdID, taskID, "file_write", outcome, constraints, approvalReq,
		[]policy.RuleEvaluation{{RuleID: "r1", Passed: true, Outcome: &allowOutcome, Reason: "ok"}},
		"test policy reason", now,
	)
	require.NoError(t, err)
	require.NoError(t, h.policyRepo.Save(ctx, pd))

	// Workflow in policy_checked state
	wf := workflow.NewWorkflowRun(h.idGen.NewWorkflowRunID(), taskID, now)
	require.NoError(t, wf.MarkRouted(rdID, "routed", actor, now))
	require.NoError(t, wf.MarkPolicyChecked(pdID, actor, now))
	require.NoError(t, h.wfRepo.Save(ctx, wf))

	return taskID, wf
}

// ============================================================
// UC-4: StartWorkflow tests
// ============================================================

func TestStartWorkflow_NoRoutingDecision_Error(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID := h.idGen.NewTaskID()
	now := h.clock.Now()

	tk, err := newTestTask(taskID, now)
	require.NoError(t, err)
	require.NoError(t, h.taskRepo.Save(ctx, tk))

	// No routing decision seeded
	_, err = h.svc.StartWorkflow(ctx, taskID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoRoutingDecision)
}

func TestStartWorkflow_NoPolicyDecision_Error(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID := h.idGen.NewTaskID()
	now := h.clock.Now()

	tk, err := newTestTask(taskID, now)
	require.NoError(t, err)
	require.NoError(t, h.taskRepo.Save(ctx, tk))

	// Seed routing but not policy
	rd, err := routing.NewRoutingDecision(
		h.idGen.NewRoutingDecisionID(), taskID,
		[]routing.StrategyEvaluation{{Strategy: routing.StrategyDirect, Score: 0.8}},
		routing.StrategyDirect, routing.RoleImplementer, "test", nil, now,
	)
	require.NoError(t, err)
	require.NoError(t, h.routingRepo.Save(ctx, rd))

	_, err = h.svc.StartWorkflow(ctx, taskID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPolicyDecision)
}

func TestStartWorkflow_Allow_RunningAndLeaseCreated(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusRunning, wf.Status)

	// Verify lease was created
	lease, err := h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, execution.LeaseStatusActive, lease.Status)
	assert.Equal(t, 3, lease.RetryBudget.MaxAttempts)

	// Verify notifier was called
	assert.Equal(t, 1, h.notifier.executionReadyCalls)
	assert.Equal(t, 0, h.notifier.approvalRequiredCalls)

	// Verify audit
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "workflow_started", h.audit.records[0].Action)
	assert.Equal(t, "running", h.audit.records[0].Outcome)
}

func TestStartWorkflow_AllowWithConstraints_RunningAndLeaseCreated(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllowWithConstraints)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusRunning, wf.Status)
	_, err = h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
}

func TestStartWorkflow_RequireApproval_AwaitingApprovalAndRequestCreated(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeRequireApproval)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusAwaitingApproval, wf.Status)

	// Verify approval request was saved
	require.Len(t, h.approvalRepo.saved, 1)

	// Verify notifier
	assert.Equal(t, 1, h.notifier.approvalRequiredCalls)
	assert.Equal(t, 0, h.notifier.executionReadyCalls)

	// Verify audit
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "workflow_started", h.audit.records[0].Action)
	assert.Equal(t, "awaiting_approval", h.audit.records[0].Outcome)
}

func TestStartWorkflow_Deny_Failed(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeDeny)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusFailed, wf.Status)

	// Verify terminated notification
	assert.Equal(t, 1, h.notifier.terminatedCalls)
	assert.Equal(t, 0, h.notifier.executionReadyCalls)

	// Verify audit
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "workflow_started", h.audit.records[0].Action)
	assert.Equal(t, "denied", h.audit.records[0].Outcome)
}

// ============================================================
// UC-7: KillWorkflow tests
// ============================================================

func TestKillWorkflow_FromRunning_KilledAndLeaseRevoked(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)
	require.Equal(t, workflow.StatusRunning, wf.Status)

	h.audit.records = nil // reset audit

	err = h.svc.KillWorkflow(ctx, wf.ID, "manual kill", shared.ActorID("admin"))
	require.NoError(t, err)

	// Verify workflow killed
	killed, err := h.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusKilled, killed.Status)

	// Verify lease revoked
	lease, err := h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, execution.LeaseStatusRevoked, lease.Status)

	// Verify audit and notifier
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "workflow_killed", h.audit.records[0].Action)
	assert.Equal(t, 1, h.notifier.terminatedCalls)
}

func TestKillWorkflow_FromPaused_Killed(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	err = h.svc.PauseWorkflow(ctx, wf.ID, "pausing", shared.ActorID("admin"))
	require.NoError(t, err)

	err = h.svc.KillWorkflow(ctx, wf.ID, "kill paused", shared.ActorID("admin"))
	require.NoError(t, err)

	killed, err := h.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusKilled, killed.Status)
}

func TestKillWorkflow_NoContinuationAfterKill(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)

	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	err = h.svc.KillWorkflow(ctx, wf.ID, "kill it", shared.ActorID("admin"))
	require.NoError(t, err)

	// Attempting to register attempt on killed workflow should fail
	result := execution.NewSuccessResult()
	_, err = h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTerminalWorkflow)
}

// ============================================================
// UC-8: Pause / Resume tests
// ============================================================

func TestPauseWorkflow_FromRunning_Paused(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	h.audit.records = nil

	err = h.svc.PauseWorkflow(ctx, wf.ID, "user pause", shared.ActorID("user"))
	require.NoError(t, err)

	paused, err := h.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusPaused, paused.Status)

	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "workflow_paused", h.audit.records[0].Action)
}

func TestResumeWorkflow_FromPaused_Running(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	err = h.svc.PauseWorkflow(ctx, wf.ID, "pause", shared.ActorID("user"))
	require.NoError(t, err)

	h.audit.records = nil
	h.notifier.executionReadyCalls = 0

	err = h.svc.ResumeWorkflow(ctx, wf.ID, "resume", shared.ActorID("user"))
	require.NoError(t, err)

	resumed, err := h.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusRunning, resumed.Status)

	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "workflow_resumed", h.audit.records[0].Action)
	assert.Equal(t, 1, h.notifier.executionReadyCalls)
}

func TestResumeWorkflow_ExhaustedLease_Error(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	// Exhaust the lease
	lease, err := h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	for lease.HasRetryBudget() {
		require.NoError(t, lease.RecordAttempt())
	}
	require.NoError(t, h.leaseRepo.Update(ctx, lease))

	err = h.svc.PauseWorkflow(ctx, wf.ID, "pause", shared.ActorID("user"))
	require.NoError(t, err)

	err = h.svc.ResumeWorkflow(ctx, wf.ID, "resume", shared.ActorID("user"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLeaseExhausted)
}

// ============================================================
// UC-9: RegisterAttempt tests
// ============================================================

func TestRegisterAttempt_Success_Completed(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	h.audit.records = nil
	h.notifier.terminatedCalls = 0

	result := execution.NewSuccessResult()
	completed, err := h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusCompleted, completed.Status)
	assert.Equal(t, 1, h.notifier.terminatedCalls)

	// Verify audit
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "attempt_registered", h.audit.records[0].Action)
	assert.Equal(t, "success", h.audit.records[0].Outcome)
}

func TestRegisterAttempt_FailureWithBudget_StaysRunning(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	h.audit.records = nil

	result, err := execution.NewFailureResult(shared.FailureStageExecution, "TIMEOUT", true)
	require.NoError(t, err)
	result = result.WithStrategy("direct").WithAgentRole("implementer").WithToolName("code_editor")

	running, err := h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusRunning, running.Status)

	// Lease should have 1 attempt used out of 3
	lease, err := h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, lease.AttemptsUsed)
	assert.True(t, lease.HasRetryBudget())
}

func TestRegisterAttempt_FailureWithoutBudget_Failed(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	// Use up 2 of 3 attempts
	lease, err := h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	require.NoError(t, lease.RecordAttempt()) // 1
	require.NoError(t, lease.RecordAttempt()) // 2 — now exhausted (3rd attempt will exhaust)
	require.NoError(t, h.leaseRepo.Update(ctx, lease))

	h.audit.records = nil
	h.notifier.terminatedCalls = 0

	result, err := execution.NewFailureResult(shared.FailureStageExecution, "CRASH", false)
	require.NoError(t, err)

	failed, err := h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.NoError(t, err)

	assert.Equal(t, workflow.StatusQuarantined, failed.Status)

	// Lease should be exhausted
	lease, err = h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, execution.LeaseStatusExhausted, lease.Status)
	assert.False(t, lease.HasRetryBudget())

	// Notification with quarantine reason
	assert.Equal(t, 1, h.notifier.terminatedCalls)
	assert.Equal(t, "quarantined:budget_exhausted", h.notifier.lastTerminatedReason)

	// Verify quarantine audit entry
	var quarantineAudit *auditRecord
	for i := range h.audit.records {
		if h.audit.records[i].Action == "workflow_quarantined" {
			quarantineAudit = &h.audit.records[i]
			break
		}
	}
	require.NotNil(t, quarantineAudit, "expected workflow_quarantined audit entry")
	assert.Equal(t, "budget_exhausted", quarantineAudit.Outcome)
}

func TestRegisterAttempt_OnTerminalWorkflow_Error(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	// Complete the workflow
	result := execution.NewSuccessResult()
	_, err = h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.NoError(t, err)

	// Attempt on terminal workflow
	_, err = h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTerminalWorkflow)
}

func TestRegisterAttempt_EnrichedAuditContext(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, _ := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	wf, err := h.svc.StartWorkflow(ctx, taskID)
	require.NoError(t, err)

	h.audit.records = nil

	result, err := execution.NewFailureResult(shared.FailureStageExecution, "PERMISSION_DENIED", false)
	require.NoError(t, err)
	result = result.WithStrategy("direct").WithAgentRole("implementer").WithToolName("file_writer")

	_, err = h.svc.RegisterAttempt(ctx, wf.ID, result)
	require.NoError(t, err)

	require.Len(t, h.audit.records, 1)
	auditCtx := h.audit.records[0].Ctx

	assert.Equal(t, "direct", auditCtx["strategy_used"])
	assert.Equal(t, "implementer", auditCtx["agent_role"])
	assert.Equal(t, "execution", auditCtx["failure_stage"])
	assert.Equal(t, "PERMISSION_DENIED", auditCtx["failure_code"])
	assert.Equal(t, false, auditCtx["retryable"])
	assert.Equal(t, "file_writer", auditCtx["tool_name"])
	assert.Equal(t, 1, auditCtx["attempts_used"])
	assert.Equal(t, 3, auditCtx["retry_budget"])
}

// ============================================================
// UC-10: GetWorkflowStatus tests
// ============================================================

func TestGetWorkflowStatus_ByID(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, wf := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)
	_ = taskID

	got, err := h.svc.GetWorkflowStatus(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, wf.ID, got.ID)
}

func TestGetWorkflowByTask(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	taskID, wf := h.seedPolicyCheckedWorkflow(t, policy.OutcomeAllow)

	got, err := h.svc.GetWorkflowByTask(ctx, taskID)
	require.NoError(t, err)
	assert.Equal(t, wf.ID, got.ID)
}

// --- helper to create test tasks without fixtures package dependency ---

func newTestTask(id shared.TaskID, now shared.Timestamp) (*task.Task, error) {
	return task.NewTask(
		id, nil,
		task.TypeDevelopment, "Test task", task.ScopeFile,
		shared.PriorityNormal, shared.RiskLevelLow, nil, now,
	)
}
