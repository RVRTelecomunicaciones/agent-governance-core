package govhttptest

import (
	"net/http"

	govhttp "github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/inbound/http"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/govdecisions"
)

// NewInMemoryDecisionsHandler returns the REAL /governance/v1/* decision facade
// wired to the REAL govdecisions.DecisionsService over a fresh in-memory store.
//
// The returned handler serves the exact production routes and DTOs:
//   - POST /governance/v1/decisions/phase
//   - POST /governance/v1/decisions/sensitive
//   - GET  /governance/v1/approvals/{change_id}/{phase_id}/status
//
// It is hermetic: no PostgreSQL, no network, deterministic id/clock. Use it to
// stand up the facade in-process for cross-module contract tests.
func NewInMemoryDecisionsHandler() http.Handler {
	return NewInMemoryDecisionsHandlerFromStore(NewInMemoryStore())
}

// NewInMemoryDecisionsHandlerFromStore wires the facade onto a caller-provided
// store, so tests can inspect recorded decision rows or seed approval gates and
// then exercise the same handler.
func NewInMemoryDecisionsHandlerFromStore(store *Store) http.Handler {
	svc := govdecisions.NewDecisionsService(
		store.Decisions,
		store.Approvals,
		store.IDGen,
		store.Clock,
		nil, // noop logger inside the service
	)

	// The five inbound services and the DB pinger are unused by the
	// /governance/v1 facade, so nil is safe here — only WithPhaseDecisions is
	// exercised. NewServer applies the production middleware chain and routes.
	srv := govhttp.NewServer(nil, nil, nil, nil, nil, nil).
		WithPhaseDecisions(svc)

	return srv
}
