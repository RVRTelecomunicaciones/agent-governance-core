//go:build integration

package circuitbreaker_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/events"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	appaudit "github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/intake"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/policyeval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/resilience"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/workflowrun"
	auditDomain "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/infrastructure/clock"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/infrastructure/idgen"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type breakerTestServices struct {
	submitTask      *intake.SubmitTaskService
	routeTask       *routing.RouteTaskService
	evalPolicy      *policyeval.EvaluatePolicyService
	workflowSvc     *workflowrun.WorkflowRunService
	breakerRegistry *resilience.CircuitBreakerRegistry
	auditRepo       *persistence.PgAuditEntryRepository
}

// setupBreakerServices wires the full application stack with a real
// CircuitBreakerRegistry (NOT nil) so breaker transitions are observed
// end-to-end via RegisterAttempt.
func setupBreakerServices(t *testing.T) *breakerTestServices {
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

	// Circuit breaker registry — real, with audit listener attached.
	breakerRegistry := resilience.NewCircuitBreakerRegistry()
	breakerRegistry.SetTransitionListener(resilience.ChainListeners(
		resilience.AuditListener(auditRecorder),
	))

	// Startup: emit the operational audit entry for the registry (matches
	// the production Wire() behavior). Not tied to any task/workflow.
	ctx := context.Background()
	_ = auditRecorder.Record(
		ctx,
		shared.ActorID("system"),
		"circuit_breaker_registry_started",
		"empty",
		auditDomain.NewAuditContext().Set("component", "circuit_breaker"),
		nil, // no task_id
		nil, // no workflow_run_id
	)

	// Application services — note the real breakerRegistry is passed last.
	submitTaskSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeTaskSvc := routing.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, nil)
	evalPolicySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier, nil, breakerRegistry)

	return &breakerTestServices{
		submitTask:      submitTaskSvc,
		routeTask:       routeTaskSvc,
		evalPolicy:      evalPolicySvc,
		workflowSvc:     workflowSvc,
		breakerRegistry: breakerRegistry,
		auditRepo:       auditRepo,
	}
}

// submitAndStart runs the full happy-path through to a running workflow and
// returns the workflow run ID, ready for RegisterAttempt calls.
func submitAndStart(t *testing.T, svc *breakerTestServices, ctx context.Context) shared.WorkflowRunID {
	t.Helper()

	// 1. Submit (bugfix, file scope → low risk → allow)
	tk, err := svc.submitTask.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "test",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	// 2. Route
	_, err = svc.routeTask.RouteTask(ctx, tk.ID())
	require.NoError(t, err)

	// 3. Evaluate policy (file_write → allow for low risk/file)
	_, err = svc.evalPolicy.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)

	// 4. Start workflow → running
	wf, err := svc.workflowSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)
	return wf.ID
}

// TestIntegration_BreakerTripsAfterConsecutiveFailures verifies that 3
// consecutive failures on the same (tool, agent_role) trip the breaker to
// OPEN, and that the transition produces a circuit_breaker_opened audit entry.
func TestIntegration_BreakerTripsAfterConsecutiveFailures(t *testing.T) {
	svc := setupBreakerServices(t)
	ctx := context.Background()

	wfID := submitAndStart(t, svc, ctx)

	// Register 3 failures with same tool+role — trips the breaker via consecutive.
	stage := shared.FailureStageRuntime
	code := "tool/shell_timeout"
	retryable := true
	tool := "shell"
	role := "implementer"
	failResult := execution.AttemptResult{
		Status:       execution.AttemptStatusFailure,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
		ToolName:     &tool,
		AgentRole:    &role,
	}
	for i := 0; i < 3; i++ {
		_, err := svc.workflowSvc.RegisterAttempt(ctx, wfID, failResult)
		require.NoError(t, err)
	}

	// Verify breaker state via registry.
	snap := svc.breakerRegistry.Get("shell", "implementer")
	require.NotNil(t, snap)
	assert.Equal(t, resilience.StateOpen, snap.State)
	assert.GreaterOrEqual(t, snap.ConsecutiveFailures, 3)

	// Verify audit entry was recorded.
	action := "circuit_breaker_opened"
	filter := outbound.AuditFilter{Action: &action, Limit: 100}
	entries, _, err := svc.auditRepo.Query(ctx, filter)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	found := false
	for _, e := range entries {
		actx := e.Context()
		if actx["tool_name"] == "shell" && actx["agent_role"] == "implementer" {
			found = true
			assert.Equal(t, "consecutive", e.Outcome())
			break
		}
	}
	assert.True(t, found, "expected audit entry for shell+implementer trip")
}

// TestIntegration_ListBreakersReturnsTrippedBreaker verifies that listing
// breakers from the registry honors the state filter after a trip.
func TestIntegration_ListBreakersReturnsTrippedBreaker(t *testing.T) {
	svc := setupBreakerServices(t)
	ctx := context.Background()

	wfID := submitAndStart(t, svc, ctx)

	// Trip one breaker.
	stage := shared.FailureStageRuntime
	code := "tool/shell_timeout"
	retryable := true
	tool := "shell"
	role := "implementer"
	failResult := execution.AttemptResult{
		Status:       execution.AttemptStatusFailure,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
		ToolName:     &tool,
		AgentRole:    &role,
	}
	for i := 0; i < 3; i++ {
		_, err := svc.workflowSvc.RegisterAttempt(ctx, wfID, failResult)
		require.NoError(t, err)
	}

	// Query registry directly — no filter.
	all := svc.breakerRegistry.List(resilience.BreakerFilter{})
	assert.Len(t, all, 1)
	assert.Equal(t, "shell", all[0].ToolName)
	assert.Equal(t, resilience.StateOpen, all[0].State)

	// Filter by state=open.
	openState := resilience.StateOpen
	openOnly := svc.breakerRegistry.List(resilience.BreakerFilter{State: &openState})
	assert.Len(t, openOnly, 1)

	// Filter by state=closed → empty.
	closedState := resilience.StateClosed
	closedOnly := svc.breakerRegistry.List(resilience.BreakerFilter{State: &closedState})
	assert.Empty(t, closedOnly)
}

// TestIntegration_StartupAuditEntry verifies the operational audit entry
// emitted at registry startup is persisted and has no task/workflow binding.
func TestIntegration_StartupAuditEntry(t *testing.T) {
	svc := setupBreakerServices(t)
	ctx := context.Background()

	// Setup emitted the startup audit entry manually.
	action := "circuit_breaker_registry_started"
	filter := outbound.AuditFilter{Action: &action, Limit: 10}
	entries, _, err := svc.auditRepo.Query(ctx, filter)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "empty", e.Outcome())
	assert.Nil(t, e.TaskID(), "startup event has no task_id")
	assert.Nil(t, e.WorkflowRunID(), "startup event has no workflow_run_id")
	assert.Equal(t, "circuit_breaker", e.Context()["component"])
}
