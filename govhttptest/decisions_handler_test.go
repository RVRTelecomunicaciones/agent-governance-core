package govhttptest_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/govhttptest"
)

// These tests are the cross-module drift lock: they boot the REAL chi
// /governance/v1 facade wired to the REAL govdecisions.DecisionsService over
// in-memory repos and assert the three orchestator-facing routes serve the
// production DTOs. A consumer in another module (sophia-orchestator) relies on
// this exported seam, so the assertions here mirror the contract it depends on.

func bootHandler(t *testing.T) http.Handler {
	t.Helper()
	h := govhttptest.NewInMemoryDecisionsHandler()
	require.NotNil(t, h, "constructor must return a wired handler")
	return h
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestInMemoryHandler_DecisionPhase_DefaultAllow(t *testing.T) {
	h := bootHandler(t)

	rec := doJSON(t, h, http.MethodPost, "/governance/v1/decisions/phase", map[string]any{
		"change_id":  "01KRJB5JRS86FCC9Y7DCDZP7X3",
		"phase_type": "explore",
		"sensitive":  false,
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "allow", resp["decision"])
	assert.Equal(t, "team-lead", resp["agent_role"])
	assert.Equal(t, "M-E0 default-allow policy", resp["reason"])
}

func TestInMemoryHandler_DecisionSensitive_DefaultAllow(t *testing.T) {
	h := bootHandler(t)

	rec := doJSON(t, h, http.MethodPost, "/governance/v1/decisions/sensitive", map[string]any{
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

func TestInMemoryHandler_ApprovalStatus_GrantedByDefault(t *testing.T) {
	h := bootHandler(t)

	rec := doJSON(t, h, http.MethodGet,
		"/governance/v1/approvals/01KRJB5JRS86FCC9Y7DCDZP7X3/01PHASEXXXXXXXXXXXXXXXXXXX/status", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "granted", resp["status"])
}

func TestInMemoryHandler_RecordsDecisionDeterministically(t *testing.T) {
	// The in-memory wiring uses a seedable id/clock (no ulid.Make/time.Now), so
	// two independently constructed stores record identical, stable ids. This
	// keeps the seam hermetic and reproducible for contract tests.
	s1 := govhttptest.NewInMemoryStore()
	s2 := govhttptest.NewInMemoryStore()

	id1 := s1.IDGen.NewPhaseDecisionID()
	id2 := s2.IDGen.NewPhaseDecisionID()
	assert.Equal(t, id1, id2, "deterministic id generator must produce stable ids across stores")
	assert.NotEmpty(t, id1)

	// Clock is fixed (no time.Now): the same instant across constructions.
	assert.Equal(t, s1.Clock.NowUTC(), s2.Clock.NowUTC())

	// Driving a real decision through a handler built on the store records a row.
	h := govhttptest.NewInMemoryDecisionsHandlerFromStore(s1)
	rec := doJSON(t, h, http.MethodPost, "/governance/v1/decisions/phase", map[string]any{
		"change_id":  "01KRJB5JRS86FCC9Y7DCDZP7X3",
		"phase_type": "explore",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, s1.Decisions.Saved(), 1, "in-memory repo must record the decision row")
}
