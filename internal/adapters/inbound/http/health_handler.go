package http

import (
	"context"
	"net/http"
	"time"
)

// DBPinger is the minimal contract the readiness handler needs from the database
// pool. *pgxpool.Pool already satisfies this — keeping it as an interface lets
// tests inject fakes without dragging the pgx dependency into the HTTP adapter.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// readyPingTimeout bounds the DB check so a hung database cannot stall the
// readiness probe past kube-style probe budgets.
const readyPingTimeout = 2 * time.Second

// handleHealth is the liveness probe.
//
// It always returns 200 with no I/O — its sole purpose is to signal that the
// process is alive and serving HTTP. It deliberately does NOT check downstream
// dependencies; that is what /ready is for. Liveness failures should restart
// the pod, so reporting a transient DB hiccup here would be wrong.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady is the readiness probe.
//
// 200 + {"status":"ready","checks":{"db":"ok"}}        — DB ping succeeded
// 503 + {"status":"degraded","checks":{"db":"<error>"}} — DB ping failed or timed out
//
// A 2s timeout bounds the ping so a wedged DB does not stall the probe.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	status := "ready"
	httpStatus := http.StatusOK

	if s.db == nil {
		checks["db"] = "not configured"
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), readyPingTimeout)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			checks["db"] = err.Error()
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
		} else {
			checks["db"] = "ok"
		}
	}

	writeJSON(w, httpStatus, map[string]any{
		"status": status,
		"checks": checks,
	})
}
