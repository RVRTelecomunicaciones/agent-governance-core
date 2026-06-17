package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapter "github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/inbound/http"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/govdecisions"
)

// --- Mock PhaseDecisionsService ---

type mockPhaseDecisions struct {
	phaseFn     func(ctx context.Context, in govdecisions.PhaseDecisionInput) (*govdecisions.Decision, error)
	sensitiveFn func(ctx context.Context, in govdecisions.SensitiveDecisionInput) (*govdecisions.Decision, error)
	statusFn    func(ctx context.Context, changeID, phaseID string) (govdecisions.ApprovalStatus, error)
}

func (m *mockPhaseDecisions) EvaluatePhase(ctx context.Context, in govdecisions.PhaseDecisionInput) (*govdecisions.Decision, error) {
	if m.phaseFn != nil {
		return m.phaseFn(ctx, in)
	}
	return nil, nil
}
func (m *mockPhaseDecisions) EvaluateSensitive(ctx context.Context, in govdecisions.SensitiveDecisionInput) (*govdecisions.Decision, error) {
	if m.sensitiveFn != nil {
		return m.sensitiveFn(ctx, in)
	}
	return nil, nil
}
func (m *mockPhaseDecisions) ApprovalStatusFor(ctx context.Context, changeID, phaseID string) (govdecisions.ApprovalStatus, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, changeID, phaseID)
	}
	return "", nil
}

func newServerWithDecisions(decisions *mockPhaseDecisions) http.Handler {
	srv := newTestServer(nil, nil, nil, nil)
	srv.WithPhaseDecisions(decisions)
	return srv
}

// --- Phase decision tests ---

func TestDecisionPhase_HappyPath_DefaultAllow(t *testing.T) {
	called := false
	decisions := &mockPhaseDecisions{
		phaseFn: func(_ context.Context, in govdecisions.PhaseDecisionInput) (*govdecisions.Decision, error) {
			called = true
			assert.Equal(t, "01KRJB5JRS86FCC9Y7DCDZP7X3", in.ChangeID)
			assert.Equal(t, "explore", in.PhaseType)
			assert.False(t, in.Sensitive)
			return &govdecisions.Decision{
				Decision:  govdecisions.DecisionAllow,
				AgentRole: "team-lead",
				Reason:    "M-E0 default-allow policy",
			}, nil
		},
	}
	srv := newServerWithDecisions(decisions)

	rec := doRequest(srv, "POST", "/governance/v1/decisions/phase", map[string]any{
		"change_id":  "01KRJB5JRS86FCC9Y7DCDZP7X3",
		"phase_type": "explore",
		"sensitive":  false,
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, called, "EvaluatePhase should be invoked")
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "allow", resp["decision"])
	assert.Equal(t, "team-lead", resp["agent_role"])
	assert.Equal(t, "M-E0 default-allow policy", resp["reason"])
}

func TestDecisionPhase_MissingChangeID_400(t *testing.T) {
	decisions := &mockPhaseDecisions{
		phaseFn: func(_ context.Context, in govdecisions.PhaseDecisionInput) (*govdecisions.Decision, error) {
			return nil, fmt.Errorf("%w: change_id is required", govdecisions.ErrInvalidInput)
		},
	}
	srv := newServerWithDecisions(decisions)

	rec := doRequest(srv, "POST", "/governance/v1/decisions/phase", map[string]any{
		"phase_type": "explore",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_INPUT", errResp.Code)
}

func TestDecisionPhase_InvalidJSON_400(t *testing.T) {
	decisions := &mockPhaseDecisions{}
	srv := newServerWithDecisions(decisions)

	req := httptest.NewRequest("POST", "/governance/v1/decisions/phase",
		bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_JSON", errResp.Code)
}

func TestDecisionPhase_ServiceNotWired_503(t *testing.T) {
	srv := newTestServer(nil, nil, nil, nil) // no .WithPhaseDecisions
	rec := doRequest(srv, "POST", "/governance/v1/decisions/phase", map[string]any{
		"change_id":  "01KRJB5JRS86FCC9Y7DCDZP7X3",
		"phase_type": "explore",
	})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestDecisionPhase_SensitiveFlagPropagates(t *testing.T) {
	var seen bool
	decisions := &mockPhaseDecisions{
		phaseFn: func(_ context.Context, in govdecisions.PhaseDecisionInput) (*govdecisions.Decision, error) {
			seen = in.Sensitive
			return &govdecisions.Decision{Decision: govdecisions.DecisionAllow, Reason: "ok"}, nil
		},
	}
	srv := newServerWithDecisions(decisions)
	rec := doRequest(srv, "POST", "/governance/v1/decisions/phase", map[string]any{
		"change_id":  "01KRJB5JRS86FCC9Y7DCDZP7X3",
		"phase_type": "apply",
		"sensitive":  true,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, seen, "sensitive flag should reach the service")
}

// --- Sensitive decision tests ---

func TestDecisionSensitive_HappyPath_DefaultAllow(t *testing.T) {
	decisions := &mockPhaseDecisions{
		sensitiveFn: func(_ context.Context, in govdecisions.SensitiveDecisionInput) (*govdecisions.Decision, error) {
			assert.Equal(t, "01KRJB5JRS86FCC9Y7DCDZP7X3", in.ChangeID)
			assert.Equal(t, "shell.exec@v1", in.Capability)
			return &govdecisions.Decision{Decision: govdecisions.DecisionAllow, Reason: "M-E0 default-allow sensitive policy"}, nil
		},
	}
	srv := newServerWithDecisions(decisions)

	// payload is []byte on the wire — Go marshals []byte as base64. Send the
	// raw orchestator shape (omit when not needed).
	rec := doRequest(srv, "POST", "/governance/v1/decisions/sensitive", map[string]any{
		"change_id":  "01KRJB5JRS86FCC9Y7DCDZP7X3",
		"capability": "shell.exec@v1",
		"payload":    []byte("ls -la"),
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "allow", resp["decision"])
	assert.Equal(t, "M-E0 default-allow sensitive policy", resp["reason"])
}

func TestDecisionSensitive_MissingCapability_400(t *testing.T) {
	decisions := &mockPhaseDecisions{
		sensitiveFn: func(_ context.Context, in govdecisions.SensitiveDecisionInput) (*govdecisions.Decision, error) {
			return nil, fmt.Errorf("%w: capability is required", govdecisions.ErrInvalidInput)
		},
	}
	srv := newServerWithDecisions(decisions)

	rec := doRequest(srv, "POST", "/governance/v1/decisions/sensitive", map[string]any{
		"change_id": "01KRJB5JRS86FCC9Y7DCDZP7X3",
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "INVALID_INPUT", errResp.Code)
}

// --- Approval status tests ---

func TestApprovalStatus_NoRow_GrantedByDefault(t *testing.T) {
	decisions := &mockPhaseDecisions{
		statusFn: func(_ context.Context, changeID, phaseID string) (govdecisions.ApprovalStatus, error) {
			assert.Equal(t, "01KRJB5JRS86FCC9Y7DCDZP7X3", changeID)
			assert.Equal(t, "01PHASEXXXXXXXXXXXXXXXXXXX", phaseID)
			return govdecisions.ApprovalGranted, nil
		},
	}
	srv := newServerWithDecisions(decisions)

	rec := doRequest(srv, "GET",
		"/governance/v1/approvals/01KRJB5JRS86FCC9Y7DCDZP7X3/01PHASEXXXXXXXXXXXXXXXXXXX/status", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "granted", resp["status"])
}

func TestApprovalStatus_PendingRow(t *testing.T) {
	decisions := &mockPhaseDecisions{
		statusFn: func(_ context.Context, _, _ string) (govdecisions.ApprovalStatus, error) {
			return govdecisions.ApprovalPending, nil
		},
	}
	srv := newServerWithDecisions(decisions)

	rec := doRequest(srv, "GET",
		"/governance/v1/approvals/01KRJB5JRS86FCC9Y7DCDZP7X3/01PHASEXXXXXXXXXXXXXXXXXXX/status", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "pending", resp["status"])
}

func TestApprovalStatus_LookupError_500(t *testing.T) {
	decisions := &mockPhaseDecisions{
		statusFn: func(_ context.Context, _, _ string) (govdecisions.ApprovalStatus, error) {
			return "", errors.New("pg down")
		},
	}
	srv := newServerWithDecisions(decisions)

	rec := doRequest(srv, "GET",
		"/governance/v1/approvals/01KRJB5JRS86FCC9Y7DCDZP7X3/01PHASEXXXXXXXXXXXXXXXXXXX/status", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	errResp := parseErrorResponse(t, rec)
	assert.Equal(t, "APPROVAL_LOOKUP_FAILED", errResp.Code)
}

// Compile-time check the mock implements the inbound interface.
var _ adapter.PhaseDecisionsService = (*mockPhaseDecisions)(nil)
