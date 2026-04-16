//go:build integration

package observability_test

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/metrics"
	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/tracing"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/events"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/persistence"
	appaudit "github.com/russellcxl/agent-governance-core/internal/application/audit"
	"github.com/russellcxl/agent-governance-core/internal/application/intake"
	"github.com/russellcxl/agent-governance-core/internal/application/policyeval"
	"github.com/russellcxl/agent-governance-core/internal/application/routing"
	"github.com/russellcxl/agent-governance-core/internal/application/workflowrun"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	policyDomain "github.com/russellcxl/agent-governance-core/internal/domain/policy"
	routingDomain "github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/clock"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/idgen"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/russellcxl/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testGovernanceAdapter implements inbound.GovernanceService by delegating to
// individual application services. This mirrors the unexported adapter in
// internal/bootstrap/wire.go.
type testGovernanceAdapter struct {
	submit   *intake.SubmitTaskService
	route    *routing.RouteTaskService
	policy   *policyeval.EvaluatePolicyService
	workflow *workflowrun.WorkflowRunService
}

var _ inbound.GovernanceService = (*testGovernanceAdapter)(nil)

func (g *testGovernanceAdapter) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	return g.submit.SubmitTask(ctx, input)
}

func (g *testGovernanceAdapter) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	// Not needed for span tree test — panic to catch accidental calls.
	panic("ProcessTask not wired in OTel integration test")
}

func (g *testGovernanceAdapter) RouteTask(ctx context.Context, taskID shared.TaskID) (*routingDomain.RoutingDecision, error) {
	return g.route.RouteTask(ctx, taskID)
}

func (g *testGovernanceAdapter) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policyDomain.PolicyDecision, error) {
	return g.policy.EvaluatePolicy(ctx, taskID, action)
}

func (g *testGovernanceAdapter) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return g.workflow.StartWorkflow(ctx, taskID)
}

// TestOTel_HappyPath_SpanTree wires the full application stack with real
// PostgreSQL and in-memory OTel exporters, then verifies:
//   - The span tree contains all expected governance + memory spans.
//   - Audit entries contain trace_id from the active OTel context.
//   - Metrics (governance.tasks.submitted) were emitted.
func TestOTel_HappyPath_SpanTree(t *testing.T) {
	// ── 1. In-memory OTel providers ──────────────────────────────────────
	spanExporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spanExporter)),
	)
	otel.SetTracerProvider(tp)

	metricReader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	otel.SetMeterProvider(mp)

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})

	tracer := tp.Tracer("governance")
	meter := mp.Meter("governance")

	// ── 2. Wire services with real PG ────────────────────────────────────
	db := testhelpers.NewTestDB(t)
	pool := db.Pool

	taskRepo := persistence.NewPgTaskRepository(pool)
	wfRepo := persistence.NewPgWorkflowRunRepository(pool)
	leaseRepo := persistence.NewPgExecutionLeaseRepository(pool)
	routingRepo := persistence.NewPgRoutingDecisionRepository(pool)
	policyRepo := persistence.NewPgPolicyDecisionRepository(pool)
	approvalRepo := persistence.NewPgApprovalRequestRepository(pool)
	auditRepo := persistence.NewPgAuditEntryRepository(pool)

	clk := clock.RealClock{}
	gen := idgen.ULIDGenerator{}
	logger := slog.Default()
	memProvider := memory.NewStubMemoryContextProvider(logger)
	notifier := events.NewCallbackNotifier()

	auditRecorder := appaudit.NewRecordAuditService(auditRepo, &gen, clk)

	// OTel instruments + lifecycle metrics
	inst, err := metrics.NewInstruments(meter)
	require.NoError(t, err)

	wfLifecycle := &workflowrun.LifecycleMetrics{
		TasksCompleted:    inst.TasksCompleted,
		WorkflowDuration:  inst.WorkflowDuration,
		ExecutionFailures: inst.ExecutionFailures,
	}

	// Application services
	submitSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeSvc := routing.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, nil)
	policySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier, wfLifecycle)

	// ── 3. Build decorated service stack ─────────────────────────────────
	govAdapter := &testGovernanceAdapter{
		submit:   submitSvc,
		route:    routeSvc,
		policy:   policySvc,
		workflow: workflowSvc,
	}

	var govSvc inbound.GovernanceService = govAdapter
	govSvc = metrics.NewMetricsGovernanceService(govSvc, inst)
	govSvc = tracing.NewTracedGovernanceService(govSvc, tracer)

	var wfCtrl inbound.WorkflowControl = workflowSvc
	wfCtrl = metrics.NewMetricsWorkflowControl(wfCtrl, inst)
	wfCtrl = tracing.NewTracedWorkflowControl(wfCtrl, tracer)

	// ── 4. Execute happy-path pipeline ───────────────────────────────────
	ctx := context.Background()

	tk, err := govSvc.SubmitTask(ctx, inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "OTel test task",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})
	require.NoError(t, err)

	_, err = govSvc.RouteTask(ctx, tk.ID())
	require.NoError(t, err)

	_, err = govSvc.EvaluatePolicy(ctx, tk.ID(), "file_write")
	require.NoError(t, err)

	wf, err := govSvc.StartWorkflow(ctx, tk.ID())
	require.NoError(t, err)

	_, err = wfCtrl.RegisterAttempt(ctx, wf.ID, execution.NewSuccessResult())
	require.NoError(t, err)

	// ── 5. Force flush spans ─────────────────────────────────────────────
	require.NoError(t, tp.ForceFlush(ctx))

	// ── 6. Verify span tree ──────────────────────────────────────────────
	spans := spanExporter.GetSpans()
	spanNames := make([]string, 0, len(spans))
	for _, s := range spans {
		spanNames = append(spanNames, s.Name)
	}

	t.Logf("Collected %d spans: %v", len(spans), spanNames)

	// Decorator spans (5 governance + 1 workflow control)
	assert.Contains(t, spanNames, "GovernanceService.SubmitTask")
	assert.Contains(t, spanNames, "GovernanceService.RouteTask")
	assert.Contains(t, spanNames, "GovernanceService.EvaluatePolicy")
	assert.Contains(t, spanNames, "GovernanceService.StartWorkflow")
	assert.Contains(t, spanNames, "WorkflowControl.RegisterAttempt")

	// Inline span from the stub memory provider (child of RouteTask and SubmitTask)
	assert.Contains(t, spanNames, "MemoryContextProvider.GetRelevantContext")

	// At least 6 spans total (5 decorator + 1 memory, possibly more inline)
	assert.GreaterOrEqual(t, len(spans), 6, "expected at least 6 spans")

	// ── 7. Verify trace_id in audit entries ──────────────────────────────
	taskID := tk.ID()
	entries, total, err := auditRepo.Query(ctx, outbound.AuditFilter{
		TaskID: &taskID,
		Limit:  100,
	})
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	t.Logf("Found %d audit entries (total: %d)", len(entries), total)

	hasTraceID := false
	for _, entry := range entries {
		if traceID, ok := entry.Context()["trace_id"]; ok {
			t.Logf("Audit entry %q has trace_id=%v", entry.Action(), traceID)
			hasTraceID = true
			break
		}
	}
	assert.True(t, hasTraceID, "at least one audit entry should contain trace_id")

	// ── 8. Verify metrics ────────────────────────────────────────────────
	var rm metricdata.ResourceMetrics
	err = metricReader.Collect(ctx, &rm)
	require.NoError(t, err)

	foundTasksSubmitted := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			t.Logf("Metric: %s", m.Name)
			if m.Name == "governance.tasks.submitted" {
				foundTasksSubmitted = true
			}
		}
	}
	assert.True(t, foundTasksSubmitted, "governance.tasks.submitted metric should be emitted")
}
