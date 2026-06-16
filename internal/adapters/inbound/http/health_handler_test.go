package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adapter "github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/inbound/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fakes for the DBPinger interface ---

type fakePinger struct {
	err   error
	sleep time.Duration
}

func (f *fakePinger) Ping(ctx context.Context) error {
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

// newHealthServer builds a Server with only the DB pinger wired — the other
// dependencies are irrelevant to /health and /ready and use zero-value mocks.
func newHealthServer(db adapter.DBPinger) *adapter.Server {
	return adapter.NewServer(
		&mockGovernanceService{},
		&mockWorkflowControl{},
		&mockApprovalService{},
		&mockQueryService{},
		&mockEscalationService{},
		db,
	)
}

// --- /health ---

func TestHealth_AlwaysOK_NoDBConfigured(t *testing.T) {
	srv := newHealthServer(nil)

	rec := doRequest(srv, http.MethodGet, "/health", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestHealth_AlwaysOK_EvenWhenDBDown(t *testing.T) {
	// Liveness MUST NOT reflect downstream health — that's what /ready is for.
	srv := newHealthServer(&fakePinger{err: errors.New("db kaputt")})

	rec := doRequest(srv, http.MethodGet, "/health", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

// --- /ready ---

func TestReady_OK_WhenDBUp(t *testing.T) {
	srv := newHealthServer(&fakePinger{})

	rec := doRequest(srv, http.MethodGet, "/ready", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ready", body.Status)
	assert.Equal(t, "ok", body.Checks["db"])
}

func TestReady_503_WhenDBDown(t *testing.T) {
	srv := newHealthServer(&fakePinger{err: errors.New("connection refused")})

	rec := doRequest(srv, http.MethodGet, "/ready", nil)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "degraded", body.Status)
	assert.Contains(t, body.Checks["db"], "connection refused")
}

func TestReady_503_WhenDBPingTimesOut(t *testing.T) {
	// Sleep longer than the 2s probe budget so the context times out first.
	srv := newHealthServer(&fakePinger{sleep: 3 * time.Second})

	start := time.Now()
	rec := doRequest(srv, http.MethodGet, "/ready", nil)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	// Must fail in roughly 2s (the handler's bounded timeout), not 3s.
	assert.Less(t, elapsed, 2500*time.Millisecond,
		"readiness probe must enforce its own timeout, took %s", elapsed)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "degraded", body.Status)
	assert.Contains(t, body.Checks["db"], "context deadline exceeded")
}

func TestReady_503_WhenDBNotConfigured(t *testing.T) {
	srv := newHealthServer(nil)

	rec := doRequest(srv, http.MethodGet, "/ready", nil)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "degraded", body.Status)
	assert.Equal(t, "not configured", body.Checks["db"])
}

// guard that the timeout constant is what we expect — if someone bumps it the
// test above stops being meaningful.
func TestReady_TimeoutBudget(t *testing.T) {
	srv := newHealthServer(&fakePinger{sleep: 100 * time.Millisecond})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "short ping should comfortably fit in the timeout")
}
