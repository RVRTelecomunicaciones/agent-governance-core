// Package bootstrap wires all application dependencies together.
// This is pure composition — no business logic lives here.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	// adapters
	httpAdapter "github.com/russellcxl/agent-governance-core/internal/adapters/inbound/http"
	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/metrics"
	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/sdk"
	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/tracing"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/events"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/persistence"

	// application
	appaudit "github.com/russellcxl/agent-governance-core/internal/application/audit"
	"github.com/russellcxl/agent-governance-core/internal/application/intake"
	"github.com/russellcxl/agent-governance-core/internal/application/policyeval"
	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
	"github.com/russellcxl/agent-governance-core/internal/application/routing"
	"github.com/russellcxl/agent-governance-core/internal/application/workflowrun"

	// domain types for interface methods
	auditDomain "github.com/russellcxl/agent-governance-core/internal/domain/audit"
	policyDomain "github.com/russellcxl/agent-governance-core/internal/domain/policy"
	routingDomain "github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"

	// infrastructure
	"github.com/russellcxl/agent-governance-core/internal/application/approvals"
	"github.com/russellcxl/agent-governance-core/internal/application/escalation"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/clock"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/config"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/idgen"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/observability"
)

// App holds the wired application components.
type App struct {
	HTTPServer   *httpAdapter.Server
	Facade       *sdk.GovernanceFacade
	Notifier     *events.CallbackNotifier
	OTelShutdown func(context.Context) error
}

// Wire creates and connects all application dependencies.
func Wire(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, cfg config.Config) (*App, error) {
	// OTel setup (no-op when disabled).
	otelShutdown, err := observability.SetupOTel(ctx, cfg.OTelEnabled, "agent-governance-core")
	if err != nil {
		return nil, fmt.Errorf("otel setup: %w", err)
	}

	// Infrastructure
	clk := clock.RealClock{}
	gen := idgen.ULIDGenerator{}

	// Repositories
	taskRepo := persistence.NewPgTaskRepository(pool)
	wfRepo := persistence.NewPgWorkflowRunRepository(pool)
	leaseRepo := persistence.NewPgExecutionLeaseRepository(pool)
	routingRepo := persistence.NewPgRoutingDecisionRepository(pool)
	policyRepo := persistence.NewPgPolicyDecisionRepository(pool)
	approvalRepo := persistence.NewPgApprovalRequestRepository(pool)
	auditRepo := persistence.NewPgAuditEntryRepository(pool)
	escalationRepo := persistence.NewPgEscalationTriggerRepository(pool)

	// Outbound adapters
	memProvider := memory.NewStubMemoryContextProvider(logger)
	notifier := events.NewCallbackNotifier()

	// Transversal audit service
	auditRecorder := appaudit.NewRecordAuditService(auditRepo, &gen, clk)

	// Lifecycle metrics (nil when OTel is disabled — backwards compatible).
	var wfLifecycle *workflowrun.LifecycleMetrics
	var approvalLifecycle *approvals.LifecycleMetrics
	var inst *metrics.Instruments

	if cfg.OTelEnabled {
		meter := otel.Meter("governance")
		inst, err = metrics.NewInstruments(meter)
		if err != nil {
			return nil, fmt.Errorf("otel instruments: %w", err)
		}

		wfLifecycle = &workflowrun.LifecycleMetrics{
			TasksCompleted:    inst.TasksCompleted,
			WorkflowDuration:  inst.WorkflowDuration,
			ExecutionFailures: inst.ExecutionFailures,
		}
		approvalLifecycle = &approvals.LifecycleMetrics{
			ApprovalWait:     inst.ApprovalWait,
			TasksCompleted:   inst.TasksCompleted,
			WorkflowDuration: inst.WorkflowDuration,
		}
	}

	// Adaptive routing (conditional)
	var statsStore *routing.FailureStatsStore
	if cfg.AdaptiveRoutingEnabled {
		statsStore = routing.NewFailureStatsStore()
		collector := routing.NewFailureStatsCollector(
			statsStore, auditRepo,
			routing.DefaultRefreshInterval, routing.DefaultStatsWindow,
			logger,
		)
		go collector.Start(ctx)
	}

	// Circuit breaker registry — in-memory, resets on restart (deliberate).
	breakerRegistry := resilience.NewCircuitBreakerRegistry()

	// Attach listeners: always audit; metrics only when OTel is enabled.
	var breakerListeners []resilience.TransitionListener
	breakerListeners = append(breakerListeners, resilience.AuditListener(auditRecorder))

	if cfg.OTelEnabled && inst != nil {
		breakerListeners = append(breakerListeners, resilience.MetricsListener(resilience.MetricsListenerDeps{
			Transitions: inst.CircuitBreakerTransitions,
			Trips:       inst.CircuitBreakerTrips,
		}))
	}
	breakerRegistry.SetTransitionListener(resilience.ChainListeners(breakerListeners...))

	// Startup: operational event for the circuit_breaker component.
	// Not a business workflow event — carries no task_id / workflow_run_id.
	logger.Info("circuit breaker registry started (empty state, all breakers begin CLOSED)")
	_ = auditRecorder.Record(
		ctx,
		shared.ActorID("system"),
		"circuit_breaker_registry_started",
		"empty",
		auditDomain.NewAuditContext().Set("component", "circuit_breaker"),
		nil, // no task_id
		nil, // no workflow_run_id
	)

	// Application services
	submitTaskSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeTaskSvc := routing.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, statsStore)
	evalPolicySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier, wfLifecycle, breakerRegistry)
	approvalSvc := approvals.NewApprovalService(approvalRepo, wfRepo, leaseRepo, &gen, clk, auditRecorder, notifier, approvalLifecycle)
	queryAuditSvc := appaudit.NewQueryAuditService(auditRepo)
	escalationSvc := escalation.NewEscalationService(escalationRepo, &gen, clk, auditRecorder)

	// ProcessTask coordinator
	processTaskSvc := intake.NewProcessTaskService(submitTaskSvc, routeTaskSvc, evalPolicySvc, workflowSvc)

	// Adapter structs that implement inbound port interfaces
	govAdapter := &governanceServiceAdapter{
		submit:   submitTaskSvc,
		process:  processTaskSvc,
		route:    routeTaskSvc,
		policy:   evalPolicySvc,
		workflow: workflowSvc,
	}
	queryAdapter := &queryServiceAdapter{
		taskRepo:        taskRepo,
		workflowSvc:     workflowSvc,
		auditQuery:      queryAuditSvc,
		breakerRegistry: breakerRegistry,
	}

	// Inbound port interfaces — wrapped with decorators when OTel is enabled.
	var govSvc inbound.GovernanceService = govAdapter
	var wfCtrl inbound.WorkflowControl = workflowSvc
	var approvalPort inbound.ApprovalService = approvalSvc
	var escalationPort inbound.EscalationPort = escalationSvc
	var queryPort inbound.QueryService = queryAdapter

	if cfg.OTelEnabled {
		tracer := otel.Tracer("governance")

		// Wrapping order: metrics inner, tracing outer.
		govSvc = metrics.NewMetricsGovernanceService(govSvc, inst)
		govSvc = tracing.NewTracedGovernanceService(govSvc, tracer)

		wfCtrl = metrics.NewMetricsWorkflowControl(wfCtrl, inst)
		wfCtrl = tracing.NewTracedWorkflowControl(wfCtrl, tracer)

		approvalPort = tracing.NewTracedApprovalService(approvalPort, tracer)

		escalationPort = tracing.NewTracedEscalationPort(escalationPort, tracer)

		queryPort = tracing.NewTracedQueryService(queryPort, tracer)
	}

	// HTTP Server + SDK Facade
	httpServer := httpAdapter.NewServer(govSvc, wfCtrl, approvalPort, queryPort, escalationPort)
	facade := sdk.NewGovernanceFacade(govSvc, wfCtrl, approvalPort, queryPort, escalationPort)

	return &App{
		HTTPServer:   httpServer,
		Facade:       facade,
		Notifier:     notifier,
		OTelShutdown: otelShutdown,
	}, nil
}

// governanceServiceAdapter implements inbound.GovernanceService.
type governanceServiceAdapter struct {
	submit   *intake.SubmitTaskService
	process  *intake.ProcessTaskService
	route    *routing.RouteTaskService
	policy   *policyeval.EvaluatePolicyService
	workflow *workflowrun.WorkflowRunService
}

func (g *governanceServiceAdapter) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	return g.submit.SubmitTask(ctx, input)
}

func (g *governanceServiceAdapter) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	return g.process.ProcessTask(ctx, input, action)
}

func (g *governanceServiceAdapter) RouteTask(ctx context.Context, taskID shared.TaskID) (*routingDomain.RoutingDecision, error) {
	return g.route.RouteTask(ctx, taskID)
}

func (g *governanceServiceAdapter) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policyDomain.PolicyDecision, error) {
	return g.policy.EvaluatePolicy(ctx, taskID, action)
}

func (g *governanceServiceAdapter) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return g.workflow.StartWorkflow(ctx, taskID)
}

// Compile-time check.
var _ inbound.GovernanceService = (*governanceServiceAdapter)(nil)

// queryServiceAdapter implements inbound.QueryService.
type queryServiceAdapter struct {
	taskRepo        outbound.TaskRepository
	workflowSvc     *workflowrun.WorkflowRunService
	auditQuery      *appaudit.QueryAuditService
	breakerRegistry *resilience.CircuitBreakerRegistry
}

func (q *queryServiceAdapter) GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	return q.taskRepo.FindByID(ctx, id)
}

func (q *queryServiceAdapter) GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	return q.workflowSvc.GetWorkflowStatus(ctx, id)
}

func (q *queryServiceAdapter) GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return q.workflowSvc.GetWorkflowByTask(ctx, taskID)
}

func (q *queryServiceAdapter) QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*auditDomain.AuditEntry, int, error) {
	return q.auditQuery.QueryAuditTrail(ctx, filter)
}

func (q *queryServiceAdapter) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return q.workflowSvc.ListWorkflows(ctx, filter)
}

func (q *queryServiceAdapter) ListBreakers(ctx context.Context, filter resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error) {
	if q.breakerRegistry == nil {
		return nil, nil
	}
	return q.breakerRegistry.List(filter), nil
}

func (q *queryServiceAdapter) GetBreakerState(ctx context.Context, tool, agentRole string) (*resilience.BreakerSnapshot, error) {
	if q.breakerRegistry == nil {
		return nil, nil
	}
	return q.breakerRegistry.Get(tool, agentRole), nil
}

// Compile-time check.
var _ inbound.QueryService = (*queryServiceAdapter)(nil)

// Compile-time checks for services that directly implement inbound ports.
var _ inbound.WorkflowControl = (*workflowrun.WorkflowRunService)(nil)
var _ inbound.ApprovalService = (*approvals.ApprovalService)(nil)
var _ inbound.EscalationPort = (*escalation.EscalationService)(nil)
