// Package bootstrap wires all application dependencies together.
// This is pure composition — no business logic lives here.
package bootstrap

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	// adapters
	httpAdapter "github.com/russellcxl/agent-governance-core/internal/adapters/inbound/http"
	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/sdk"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/events"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/persistence"

	// application
	appaudit "github.com/russellcxl/agent-governance-core/internal/application/audit"
	"github.com/russellcxl/agent-governance-core/internal/application/intake"
	"github.com/russellcxl/agent-governance-core/internal/application/policyeval"
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
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/clock"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/idgen"
)

// App holds the wired application components.
type App struct {
	HTTPServer *httpAdapter.Server
	Facade     *sdk.GovernanceFacade
	Notifier   *events.CallbackNotifier
}

// Wire creates and connects all application dependencies.
func Wire(pool *pgxpool.Pool, logger *slog.Logger) *App {
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

	// Outbound adapters
	memProvider := memory.NewStubMemoryContextProvider(logger)
	notifier := events.NewCallbackNotifier()

	// Transversal audit service
	auditRecorder := appaudit.NewRecordAuditService(auditRepo, &gen, clk)

	// Application services
	submitTaskSvc := intake.NewSubmitTaskService(taskRepo, &gen, clk, auditRecorder, memProvider)
	routeTaskSvc := routing.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider)
	evalPolicySvc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, &gen, clk, auditRecorder)
	workflowSvc := workflowrun.NewWorkflowRunService(wfRepo, leaseRepo, taskRepo, routingRepo, policyRepo, approvalRepo, &gen, clk, auditRecorder, notifier)
	approvalSvc := approvals.NewApprovalService(approvalRepo, wfRepo, leaseRepo, &gen, clk, auditRecorder, notifier)
	queryAuditSvc := appaudit.NewQueryAuditService(auditRepo)

	// ProcessTask coordinator
	processTaskSvc := intake.NewProcessTaskService(submitTaskSvc, routeTaskSvc, evalPolicySvc, workflowSvc)

	// Adapter structs that implement inbound port interfaces
	govSvc := &governanceServiceAdapter{
		submit:   submitTaskSvc,
		process:  processTaskSvc,
		route:    routeTaskSvc,
		policy:   evalPolicySvc,
		workflow: workflowSvc,
	}
	querySvc := &queryServiceAdapter{
		taskRepo:    taskRepo,
		workflowSvc: workflowSvc,
		auditQuery:  queryAuditSvc,
	}

	// HTTP Server + SDK Facade
	httpServer := httpAdapter.NewServer(govSvc, workflowSvc, approvalSvc, querySvc)
	facade := sdk.NewGovernanceFacade(govSvc, workflowSvc, approvalSvc, querySvc)

	return &App{
		HTTPServer: httpServer,
		Facade:     facade,
		Notifier:   notifier,
	}
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
	taskRepo    outbound.TaskRepository
	workflowSvc *workflowrun.WorkflowRunService
	auditQuery  *appaudit.QueryAuditService
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

// Compile-time check.
var _ inbound.QueryService = (*queryServiceAdapter)(nil)

// Compile-time checks for services that directly implement inbound ports.
var _ inbound.WorkflowControl = (*workflowrun.WorkflowRunService)(nil)
var _ inbound.ApprovalService = (*approvals.ApprovalService)(nil)
