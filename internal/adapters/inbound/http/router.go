package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

// Server is the HTTP adapter that maps REST endpoints to inbound port operations.
type Server struct {
	router     chi.Router
	governance inbound.GovernanceService
	control    inbound.WorkflowControl
	approvals  inbound.ApprovalService
	queries    inbound.QueryService
	escalation inbound.EscalationPort
	db         DBPinger
}

// NewServer creates a configured HTTP server with all routes registered.
//
// db is used exclusively by the /ready readiness probe and may be nil in tests
// that do not exercise that endpoint; in that case /ready will report
// "degraded".
func NewServer(
	governance inbound.GovernanceService,
	control inbound.WorkflowControl,
	approvals inbound.ApprovalService,
	queries inbound.QueryService,
	escalation inbound.EscalationPort,
	db DBPinger,
) *Server {
	s := &Server{
		router:     chi.NewRouter(),
		governance: governance,
		control:    control,
		approvals:  approvals,
		queries:    queries,
		escalation: escalation,
		db:         db,
	}
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.SetHeader("Content-Type", "application/json"))

	s.routes()
	return s
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Liveness and readiness probes — mounted at the root, outside /api/v1,
	// so probes are independent of business endpoints and require no auth path.
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/ready", s.handleReady)

	s.router.Route("/api/v1", func(r chi.Router) {
		// Tasks
		r.Post("/tasks", s.handleSubmitTask)
		r.Get("/tasks/{taskID}", s.handleGetTask)
		r.Post("/tasks/{taskID}/route", s.handleRouteTask)
		r.Post("/tasks/{taskID}/evaluate-policy", s.handleEvaluatePolicy)
		r.Post("/tasks/{taskID}/start-workflow", s.handleStartWorkflow)
		r.Post("/tasks/{taskID}/process", s.handleProcessTask)
		r.Post("/tasks/{taskID}/escalate", s.handleTriggerEscalation)

		// Workflows
		r.Get("/workflows", s.handleListWorkflows)
		r.Get("/workflows/{workflowID}", s.handleGetWorkflowStatus)
		r.Post("/workflows/{workflowID}/kill", s.handleKillWorkflow)
		r.Post("/workflows/{workflowID}/pause", s.handlePauseWorkflow)
		r.Post("/workflows/{workflowID}/resume", s.handleResumeWorkflow)
		r.Post("/workflows/{workflowID}/attempts", s.handleRegisterAttempt)

		// Approvals
		r.Get("/approvals/pending", s.handleGetPendingApprovals)
		r.Post("/approvals/{approvalID}/resolve", s.handleResolveApproval)

		// Audit
		r.Get("/audit", s.handleQueryAuditTrail)

		// Circuit breakers
		r.Get("/breakers", s.handleListBreakers)
	})
}
