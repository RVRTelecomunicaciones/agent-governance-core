package http

import (
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	govmw "github.com/russellcxl/agent-governance-core/internal/adapters/inbound/http/middleware"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

// Server is the HTTP adapter that maps REST endpoints to inbound port operations.
type Server struct {
	router         chi.Router
	governance     inbound.GovernanceService
	control        inbound.WorkflowControl
	approvals      inbound.ApprovalService
	queries        inbound.QueryService
	escalation     inbound.EscalationPort
	phaseDecisions PhaseDecisionsService
	db             DBPinger
	rand           io.Reader
	logger         *slog.Logger
}

// NewServer creates a configured HTTP server with all routes registered.
//
// db is used exclusively by the /ready readiness probe and may be nil in tests
// that do not exercise that endpoint; in that case /ready will report
// "degraded".
//
// Uses crypto/rand.Reader and slog.Default() for the W3C trace middleware.
// Tests that need deterministic IDs or log capture should use NewServerWithObs.
func NewServer(
	governance inbound.GovernanceService,
	control inbound.WorkflowControl,
	approvals inbound.ApprovalService,
	queries inbound.QueryService,
	escalation inbound.EscalationPort,
	db DBPinger,
) *Server {
	return NewServerWithObs(governance, control, approvals, queries, escalation, db, rand.Reader, slog.Default())
}

// NewServerWithObs is the test-friendly constructor that lets callers inject a
// deterministic rand source and a captured *slog.Logger. Behaviour is otherwise
// identical to NewServer (ADR-0005 P2.2d).
func NewServerWithObs(
	governance inbound.GovernanceService,
	control inbound.WorkflowControl,
	approvals inbound.ApprovalService,
	queries inbound.QueryService,
	escalation inbound.EscalationPort,
	db DBPinger,
	randSrc io.Reader,
	logger *slog.Logger,
) *Server {
	if randSrc == nil {
		randSrc = rand.Reader
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		router:     chi.NewRouter(),
		governance: governance,
		control:    control,
		approvals:  approvals,
		queries:    queries,
		escalation: escalation,
		db:         db,
		rand:       randSrc,
		logger:     logger,
	}
	// TraceW3C MUST be first: it injects the W3C Trace into context so all
	// subsequent middlewares (Logger, Recoverer) and handlers see trace_id.
	s.router.Use(govmw.TraceW3C(s.rand, s.logger))
	s.router.Use(chimw.Logger)
	s.router.Use(chimw.Recoverer)
	s.router.Use(chimw.SetHeader("Content-Type", "application/json"))

	s.routes()
	return s
}

// WithPhaseDecisions wires the M-E0 orchestator-facing governance facade. When
// the service is nil the /governance/v1/... endpoints reply 503. Kept as a
// setter (not a constructor arg) to preserve existing call sites and let the
// Sprint 3 unification consolidate the surface without another breaking churn.
func (s *Server) WithPhaseDecisions(svc PhaseDecisionsService) *Server {
	s.phaseDecisions = svc
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

	// M-E0 orchestator-facing governance facade. Shares the same middleware
	// chain (W3C trace, logger, recoverer, JSON content-type) as /api/v1 —
	// internal-network only, no auth (governance is reachable only inside the
	// compose network). See decisions_handler.go.
	s.router.Route("/governance/v1", func(r chi.Router) {
		r.Post("/decisions/phase", s.handleDecisionPhase)
		r.Post("/decisions/sensitive", s.handleDecisionSensitive)
		r.Get("/approvals/{change_id}/{phase_id}/status", s.handleApprovalStatus)
	})
}
