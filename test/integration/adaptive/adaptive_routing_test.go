//go:build integration

package adaptive_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/events"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/approvals"
	appaudit "github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/intake"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/policyeval"
	approuting "github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/workflowrun"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	domainrouting "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/infrastructure/clock"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/infrastructure/idgen"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServices struct {
	submitTask  *intake.SubmitTaskService
	routeTask   *approuting.RouteTaskService
	evalPolicy  *policyeval.EvaluatePolicyService
	workflowSvc *workflowrun.WorkflowRunService
	approvalSvc *approvals.ApprovalService
	auditQuery  *appaudit.QueryAuditService

	collector   *approuting.FailureStatsCollector
	routingRepo *persistence.PgRoutingDecisionRepository

	// repos for direct verification
	taskRepo *persistence.PgTaskRepository
	wfRepo   *persistence.PgWorkflowRunRepository
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

	// Failure stats store + collector
	statsStore := approuting.NewFailureStatsStore()
	collector := approuting.NewFailureStatsCollector(
		statsStore, auditRepo, 5*time.Minute, 48*time.Hour, logger,
	)

	// Application services
	submitTaskSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeTaskSvc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, statsStore)
	evalPolicySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier, nil, nil)
	approvalSvc := approvals.NewApprovalService(approvalRepo, wfRepo, leaseRepo, &gen, clk, auditRecorder, notifier, nil)
	auditQuery := appaudit.NewQueryAuditService(auditRepo)

	return &testServices{
		submitTask:  submitTaskSvc,
		routeTask:   routeTaskSvc,
		evalPolicy:  evalPolicySvc,
		workflowSvc: workflowSvc,
		approvalSvc: approvalSvc,
		auditQuery:  auditQuery,
		collector:   collector,
		routingRepo: routingRepo,
		taskRepo:    taskRepo,
		wfRepo:      wfRepo,
	}
}

// executeTask runs a task through the full pipeline: submit -> route -> policy -> start -> register attempt.
func executeTask(
	t *testing.T,
	ctx context.Context,
	svc *testServices,
	taskType task.TaskType,
	scope task.TaskScope,
	action string,
	attemptResult execution.AttemptResult,
) {
	t.Helper()

	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     taskType,
		Title:    "test task",
		Scope:    scope,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)

	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), action)
	require.NoError(t, err)

	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)

	_, err = svc.workflowSvc.RegisterAttempt(ctx, wf.ID, attemptResult)
	require.NoError(t, err)
}

// TestAdaptiveRouting_PenalizesHighFailureStrategy verifies that adaptive routing
// penalizes a strategy with a high failure rate by applying a negative adjustment
// to its score in the routing decision.
func TestAdaptiveRouting_PenalizesHighFailureStrategy(t *testing.T) {
	svc := setupServices(t)
	ctx := context.Background()

	// --- Phase 1: Build failure history ---
	// 3 successes for direct strategy (with strategy metadata so collector counts them)
	for i := 0; i < 3; i++ {
		successResult := execution.NewSuccessResult().WithStrategy("direct").WithAgentRole("implementer")
		executeTask(t, ctx, svc,
			task.TypeBugfix, task.ScopeFile, "file_write",
			successResult,
		)
	}

	// 7 failures for direct strategy with tool/shell_timeout
	for i := 0; i < 7; i++ {
		failResult, err := execution.NewFailureResult(shared.FailureStageRuntime, "tool/shell_timeout", true)
		require.NoError(t, err)
		failResult = failResult.WithToolName("shell").WithStrategy("direct").WithAgentRole("implementer")

		executeTask(t, ctx, svc,
			task.TypeBugfix, task.ScopeFile, "file_write",
			failResult,
		)
	}

	// --- Phase 2: Refresh stats snapshot ---
	svc.collector.Refresh(ctx)

	// --- Phase 3: Route a new task and verify adaptive adjustment ---
	newTask, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "new task for adaptive test",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	_, err = svc.routeTask.RouteTask(ctx, newTask.ID())
	require.NoError(t, err)

	// Load the routing decision from the DB
	rd, err := svc.routingRepo.FindByTaskID(ctx, newTask.ID())
	require.NoError(t, err)

	// Verify the reason mentions adaptive
	assert.Contains(t, rd.Reason(), "[adaptive]")

	// Check evaluations for adaptive adjustment on direct strategy
	foundAdaptive := false
	for _, eval := range rd.EvaluatedStrategies() {
		if eval.Strategy == domainrouting.StrategyDirect && eval.AdaptiveAdjustment != nil {
			foundAdaptive = true
			assert.Less(t, eval.AdjustedScore, eval.Score, "direct should be penalized")
			assert.True(t, eval.AdaptiveAdjustment.FinalAdjustment < 0, "adjustment should be negative")
			// The cascade hits Level 1 (strategy+role+category) with the 7 failure
			// entries that carry full metadata (role + category). Success entries
			// lack a failure_code so they have no category and only count at Level 3.
			assert.GreaterOrEqual(t, eval.AdaptiveAdjustment.SampleSize, 5)
			assert.Equal(t, "48h0m0s", eval.AdaptiveAdjustment.Window)
			break
		}
	}
	assert.True(t, foundAdaptive, "direct strategy should have adaptive adjustment")
}
