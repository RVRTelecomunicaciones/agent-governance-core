//go:build integration

package quarantine_test

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

	// Audit recorder
	auditRecorder := appaudit.NewRecordAuditService(auditRepo, &gen, clk)

	// Application services
	submitTaskSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeTaskSvc := routing.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, nil)
	evalPolicySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier, nil, nil)
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

// submitRouteStartWorkflow is a helper that submits a task, routes it, evaluates
// policy, and starts the workflow. Returns the task and running workflow.
func submitRouteStartWorkflow(t *testing.T, ctx context.Context, svc *testServices, title, action string) (*task.Task, *domainworkflow.WorkflowRun) {
	t.Helper()

	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    title,
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)

	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), action)
	require.NoError(t, err)

	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)

	return tk, wf
}

// TestQuarantineFlow_BudgetExhausted_QuarantinedInDB exercises the full pipeline:
// submit → route → policy (allow) → start (running) → 3 failures → quarantined.
func TestQuarantineFlow_BudgetExhausted_QuarantinedInDB(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Submit → route → policy (allow) → start workflow (running)
	_, wf := submitRouteStartWorkflow(t, ctx, svc, "Fix null pointer in handler", "file_write")
	assert.Equal(t, domainworkflow.StatusRunning, wf.Status)

	// 2. Register 3 failure attempts (default retry budget = 3)
	var err error
	for i := 0; i < 3; i++ {
		failResult, fErr := execution.NewFailureResult(shared.FailureStageRuntime, "tool/shell_timeout", true)
		require.NoError(t, fErr)
		failResult = failResult.WithStrategy("direct").WithAgentRole("implementer")
		wf, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, failResult)
		require.NoError(t, err)
	}

	// 3. Assert: workflow is quarantined (NOT failed)
	assert.Equal(t, domainworkflow.StatusQuarantined, wf.Status)
	assert.True(t, wf.Status.IsTerminal())

	// 4. Reload from DB — verify persistence roundtrip
	reloaded, err := svc.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusQuarantined, reloaded.Status)

	// 5. Query audit entries for this workflow
	wfID := wf.ID
	entries, _, err := svc.auditQuery.QueryAuditTrail(ctx, outbound.AuditFilter{
		WorkflowRunID: &wfID,
		Limit:         50,
	})
	require.NoError(t, err)

	// Verify audit trail contains quarantine entries
	hasQuarantineAudit := false
	hasAttemptFailure := false
	for _, e := range entries {
		if e.Action() == "workflow_quarantined" && e.Outcome() == "budget_exhausted" {
			hasQuarantineAudit = true
		}
		if e.Action() == "attempt_registered" && e.Outcome() == "failure" {
			hasAttemptFailure = true
		}
	}
	assert.True(t, hasQuarantineAudit, "missing workflow_quarantined audit entry with outcome budget_exhausted")
	assert.True(t, hasAttemptFailure, "missing attempt_registered audit entry with outcome failure")
}

// TestListWorkflows_StatusFilter_QuarantinedOnly verifies that List with
// status=quarantined returns only quarantined workflows and not completed ones.
func TestListWorkflows_StatusFilter_QuarantinedOnly(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// 1. Create a quarantined workflow (budget exhaustion)
	_, quarantinedWf := submitRouteStartWorkflow(t, ctx, svc, "Quarantine candidate", "file_write")
	var err error
	for i := 0; i < 3; i++ {
		failResult, fErr := execution.NewFailureResult(shared.FailureStageRuntime, "tool/shell_timeout", true)
		require.NoError(t, fErr)
		failResult = failResult.WithStrategy("direct").WithAgentRole("implementer")
		quarantinedWf, err = svc.workflowSvc.RegisterAttempt(ctx, quarantinedWf.ID, failResult)
		require.NoError(t, err)
	}
	require.Equal(t, domainworkflow.StatusQuarantined, quarantinedWf.Status)

	// 2. Create a normal completed workflow
	_, completedWf := submitRouteStartWorkflow(t, ctx, svc, "Normal success workflow", "file_write")
	assert.Equal(t, domainworkflow.StatusRunning, completedWf.Status)

	completedWf, err = svc.workflowSvc.RegisterAttempt(ctx, completedWf.ID, execution.NewSuccessResult())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusCompleted, completedWf.Status)

	// 3. List workflows with status=quarantined
	quarantinedStatus := domainworkflow.StatusQuarantined
	results, total, err := svc.wfRepo.List(ctx, outbound.WorkflowListFilter{
		Status: &quarantinedStatus,
		Limit:  10,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, results, 1)
	assert.Equal(t, domainworkflow.StatusQuarantined, results[0].Status)
	assert.Equal(t, quarantinedWf.ID, results[0].ID)
}

// TestPolicyDeny_GoesToFailed_NotQuarantined verifies that a policy deny
// results in StatusFailed, NOT StatusQuarantined. Quarantine only applies
// to budget exhaustion during execution.
func TestPolicyDeny_GoesToFailed_NotQuarantined(t *testing.T) {
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
	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "data_deletion")
	require.NoError(t, err)

	// 4. Start workflow → should fail (policy denied)
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusFailed, wf.Status)
	assert.True(t, wf.Status.IsTerminal())

	// 5. Verify it is NOT quarantined
	assert.NotEqual(t, domainworkflow.StatusQuarantined, wf.Status)

	// 6. Verify persistence roundtrip
	reloaded, err := svc.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, domainworkflow.StatusFailed, reloaded.Status)
}
