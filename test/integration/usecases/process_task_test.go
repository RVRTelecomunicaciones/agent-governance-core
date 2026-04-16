//go:build integration

package usecases_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/events"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/russellcxl/agent-governance-core/internal/application/approvals"
	appaudit "github.com/russellcxl/agent-governance-core/internal/application/audit"
	"github.com/russellcxl/agent-governance-core/internal/application/intake"
	"github.com/russellcxl/agent-governance-core/internal/application/policyeval"
	"github.com/russellcxl/agent-governance-core/internal/application/routing"
	"github.com/russellcxl/agent-governance-core/internal/application/workflowrun"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	domainworkflow "github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/clock"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/idgen"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/russellcxl/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServices struct {
	submitTask  *intake.SubmitTaskService
	routeTask   *routing.RouteTaskService
	evalPolicy  *policyeval.EvaluatePolicyService
	workflowSvc *workflowrun.WorkflowRunService
	approvalSvc *approvals.ApprovalService
	auditQuery  *appaudit.QueryAuditService
	notifier    *events.CallbackNotifier

	// repos for direct verification
	taskRepo     *persistence.PgTaskRepository
	wfRepo       *persistence.PgWorkflowRunRepository
	leaseRepo    *persistence.PgExecutionLeaseRepository
	auditRepo    *persistence.PgAuditEntryRepository
	approvalRepo *persistence.PgApprovalRequestRepository
}

func setupServices(t *testing.T) *testServices {
	t.Helper()
	db := testhelpers.NewTestDB(t)
	pool := db.Pool

	// Repos
	taskRepo := persistence.NewPgTaskRepository(pool)
	wfRepo := persistence.NewPgWorkflowRunRepository(pool)
	leaseRepo := persistence.NewPgExecutionLeaseRepository(pool)
	routingRepo := persistence.NewPgRoutingDecisionRepository(pool)
	policyRepo := persistence.NewPgPolicyDecisionRepository(pool)
	approvalRepo := persistence.NewPgApprovalRequestRepository(pool)
	auditRepo := persistence.NewPgAuditEntryRepository(pool)

	// Infrastructure
	clk := clock.RealClock{}
	gen := idgen.ULIDGenerator{}
	logger := slog.Default()
	memProvider := memory.NewStubMemoryContextProvider(logger)
	notifier := events.NewCallbackNotifier()

	// Audit recorder (transversal)
	auditRecorder := appaudit.NewRecordAuditService(auditRepo, &gen, clk)

	// Application services
	submitTaskSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeTaskSvc := routing.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, nil)
	evalPolicySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier, nil)
	approvalSvc := approvals.NewApprovalService(approvalRepo, wfRepo, leaseRepo, &gen, clk, auditRecorder, notifier, nil)
	auditQuery := appaudit.NewQueryAuditService(auditRepo)

	return &testServices{
		submitTask:   submitTaskSvc,
		routeTask:    routeTaskSvc,
		evalPolicy:   evalPolicySvc,
		workflowSvc:  workflowSvc,
		approvalSvc:  approvalSvc,
		auditQuery:   auditQuery,
		notifier:     notifier,
		taskRepo:     taskRepo,
		wfRepo:       wfRepo,
		leaseRepo:    leaseRepo,
		auditRepo:    auditRepo,
		approvalRepo: approvalRepo,
	}
}

// TestHappyPath_Allow_Completed exercises the full pipeline:
// submit → route → policy (allow) → start workflow (running) → register success → completed.
func TestHappyPath_Allow_Completed(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit task (bugfix + file scope → low risk → allow)
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "Fix null pointer in handler",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)
	require.NotNil(t, tk)
	assert.Equal(t, shared.RiskLevelLow, tk.RiskLevel())

	// 2. Route task
	rd, err := svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)
	require.NotNil(t, rd)

	// 3. Evaluate policy — low risk + file scope → allow
	pd, err := svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)
	require.NotNil(t, pd)

	// 4. Start workflow → running (policy allowed)
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusRunning, wf.Status)

	// 5. Register successful attempt
	wf, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, execution.NewSuccessResult())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusCompleted, wf.Status)

	// 6. Verify persistence roundtrip
	reloaded, err := svc.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusCompleted, reloaded.Status)
	assert.True(t, reloaded.Status.IsTerminal())

	// 7. Verify audit trail
	taskID := tk.ID()
	entries, total, err := svc.auditQuery.QueryAuditTrail(ctx, outbound.AuditFilter{
		TaskID: &taskID,
		Limit:  50,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 3, "expected at least task_submitted, task_routed, policy_evaluated audit entries")

	actions := make(map[string]bool)
	for _, e := range entries {
		actions[e.Action()] = true
	}
	assert.True(t, actions["task_submitted"], "missing task_submitted audit entry")
	assert.True(t, actions["task_routed"], "missing task_routed audit entry")
	assert.True(t, actions["policy_evaluated"], "missing policy_evaluated audit entry")
}

// TestApprovalFlow_RequireApproval_Approved_Completed exercises:
// submit (deployment) → route → policy (require_approval) → start (awaiting_approval) → approve → running → completed.
func TestApprovalFlow_RequireApproval_Approved_Completed(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit deployment task — deployment always requires approval
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeDeployment,
		Title:    "Deploy auth service v2",
		Scope:    task.ScopeRepo,
		Priority: shared.PriorityHigh,
	})
	require.NoError(t, err)

	// 2. Route
	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)

	// 3. Evaluate policy — deployment → require_approval
	pd, err := svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)
	require.NotNil(t, pd)

	// 4. Start workflow → awaiting_approval
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusAwaitingApproval, wf.Status)

	// 5. Find pending approval
	pending, err := svc.approvalSvc.GetPendingApprovals(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1, "expected exactly one pending approval")
	approvalReq := pending[0]
	assert.Equal(t, wf.ID, approvalReq.WorkflowRunID())

	// 6. Approve
	resolved, err := svc.approvalSvc.ResolveApproval(ctx, inbound.ResolveApprovalInput{
		ApprovalRequestID: approvalReq.ID(),
		Approved:          true,
		ResolvedBy:        shared.ActorID("tech-lead"),
		Reason:            "deployment reviewed and approved",
	})
	require.NoError(t, err)
	require.NotNil(t, resolved)

	// 7. Verify workflow is now running (approved → running)
	wf, err = svc.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusRunning, wf.Status)

	// 8. Register success → completed
	wf, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, execution.NewSuccessResult())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusCompleted, wf.Status)

	// 9. Verify audit trail includes approval_resolved
	wfID := wf.ID
	entries, _, err := svc.auditQuery.QueryAuditTrail(ctx, outbound.AuditFilter{
		WorkflowRunID: &wfID,
		Limit:         50,
	})
	require.NoError(t, err)

	actions := make(map[string]bool)
	for _, e := range entries {
		actions[e.Action()] = true
	}
	assert.True(t, actions["approval_resolved"], "missing approval_resolved audit entry")
}

// TestDenyFlow_DestructiveAction_Failed exercises:
// submit → route → policy (deny via data_deletion) → start workflow → failed.
func TestDenyFlow_DestructiveAction_Failed(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit task
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "Delete stale user records",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	// 2. Route
	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)

	// 3. Evaluate policy with destructive action → deny
	pd, err := svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "data_deletion")
	require.NoError(t, err)
	require.NotNil(t, pd)

	// 4. Start workflow → should fail (policy denied)
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusFailed, wf.Status)
	assert.True(t, wf.Status.IsTerminal())

	// 5. Verify no execution lease was created
	_, leaseErr := svc.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	assert.Error(t, leaseErr, "expected no lease for denied workflow")

	// 6. Verify persistence roundtrip
	reloaded, err := svc.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusFailed, reloaded.Status)
}

// TestKillMidExecution exercises:
// submit → route → policy (allow) → start (running) → kill → killed.
func TestKillMidExecution(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit (low risk, allow path)
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "Fix formatting in README",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityLow,
	})
	require.NoError(t, err)

	// 2-4. Route → policy → start
	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)
	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusRunning, wf.Status)

	// 5. Kill
	err = svc.workflowSvc.KillWorkflow(ctx, wf.ID, "operator abort", shared.ActorID("admin"))
	require.NoError(t, err)

	// 6. Verify workflow is killed
	wf, err = svc.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusKilled, wf.Status)
	assert.True(t, wf.Status.IsTerminal())

	// 7. Verify lease is revoked
	lease, err := svc.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, execution.LeaseStatusRevoked, lease.Status)

	// 8. Verify no further operations work on terminal workflow
	_, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, execution.NewSuccessResult())
	assert.Error(t, err, "should not register attempt on killed workflow")
	assert.ErrorIs(t, err, workflowrun.ErrTerminalWorkflow)
}

// TestRetryBudgetExhausted exercises:
// submit → route → policy (allow) → start (running) → register failures until budget exhausted → failed.
func TestRetryBudgetExhausted(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit (low risk, allow path)
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "Fix flaky test",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	// 2-4. Route → policy → start
	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)
	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusRunning, wf.Status)

	// 5. Register failure attempts until budget exhausted (default max retries = 3)
	for i := 0; i < 3; i++ {
		failResult, fErr := execution.NewFailureResult(shared.FailureStageRuntime, "test/failure", true)
		require.NoError(t, fErr)
		wf, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, failResult)
		require.NoError(t, err)
	}

	// 6. After 3 failures (maxRetries=3), workflow should be quarantined (budget exhausted)
	assert.Equal(t, domainworkflow.StatusQuarantined, wf.Status)
	assert.True(t, wf.Status.IsTerminal())

	// 7. Verify lease is exhausted
	lease, err := svc.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, execution.LeaseStatusExhausted, lease.Status)
	assert.Equal(t, 3, lease.AttemptsUsed)

	// 8. Verify no further attempts are allowed
	_, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, execution.NewSuccessResult())
	assert.Error(t, err, "should not register attempt on failed workflow")
}

// TestDecompose_RoutesToDecomposeStrategy verifies that a refactor+repo task
// routes to the decompose strategy (subtask creation is Phase 2).
func TestDecompose_RoutesToDecomposeStrategy(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit refactor + repo scope → medium risk → decompose strategy
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeRefactor,
		Title:    "Refactor persistence layer",
		Scope:    task.ScopeRepo,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)
	assert.Equal(t, shared.RiskLevelMedium, tk.RiskLevel())

	// 2. Route — should select decompose
	rd, err := svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)
	require.NotNil(t, rd)

	// Verify the routing decision selected decompose
	assert.Equal(t, "decompose", string(rd.SelectedStrategy()))

	// 3. Verify the workflow was created and is in routed state
	wf, err := svc.wfRepo.FindByTaskID(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusRouted, wf.Status)
}

// TestRegisterAttempt_FailureTelemetry verifies that failure telemetry fields
// survive the roundtrip through audit context JSONB.
func TestRegisterAttempt_FailureTelemetry(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit + route + policy + start (allow path)
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "Fix timeout in shell executor",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)
	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)
	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)

	// 2. Register failure with full telemetry
	failResult, fErr := execution.NewFailureResult(shared.FailureStageRuntime, "tool/shell_timeout", true)
	require.NoError(t, fErr)
	failResult = failResult.WithToolName("shell").WithStrategy("direct").WithAgentRole("implementer")

	wf, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, failResult)
	require.NoError(t, err)
	// Should still be running (has retry budget remaining)
	assert.Equal(t, domainworkflow.StatusRunning, wf.Status)

	// 3. Query audit entries for the attempt_registered action
	action := "attempt_registered"
	wfID := wf.ID
	entries, _, err := svc.auditQuery.QueryAuditTrail(ctx, outbound.AuditFilter{
		WorkflowRunID: &wfID,
		Action:        &action,
		Limit:         10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, entries, "expected at least one attempt_registered audit entry")

	// 4. Verify the telemetry fields survived the JSONB roundtrip
	auditCtx := entries[0].Context()
	assert.Equal(t, "runtime", auditCtx["failure_stage"])
	assert.Equal(t, "tool/shell_timeout", auditCtx["failure_code"])
	assert.Equal(t, true, auditCtx["retryable"])
	assert.Equal(t, "shell", auditCtx["tool_name"])
	assert.Equal(t, "direct", auditCtx["strategy_used"])
	assert.Equal(t, "implementer", auditCtx["agent_role"])

	// Verify lease state telemetry
	assert.NotNil(t, auditCtx["attempts_used"])
	assert.NotNil(t, auditCtx["retry_budget"])
}

// TestMemoryContextProvider_Degradation verifies the system works end-to-end
// with the stub memory provider (no memory-engine available).
func TestMemoryContextProvider_Degradation(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit — stub provider logs warning but does not fail
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "Implement new feature X",
		Scope:    task.ScopeModule,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)
	require.NotNil(t, tk)

	// 2. Route — routing should complete with neutral memory scores
	rd, err := svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)
	require.NotNil(t, rd)

	// 3. Verify a routing decision exists and the workflow advanced
	wf, err := svc.wfRepo.FindByTaskID(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusRouted, wf.Status)

	// 4. Continue through the rest of the pipeline to prove full degradation works
	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)

	wf, err = svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	// Medium risk + module scope → allow_with_constraints (no explicit rule, falls through)
	// The system should handle this regardless
	assert.False(t, wf.Status.IsTerminal(), "workflow should not be terminal after start")
}
