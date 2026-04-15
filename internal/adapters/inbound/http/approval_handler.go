package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

func (s *Server) handleGetPendingApprovals(w http.ResponseWriter, r *http.Request) {
	results, err := s.approvals.GetPendingApprovals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	resp := make([]approvalRequestResponse, 0, len(results))
	for _, ar := range results {
		resp = append(resp, approvalRequestToResponse(ar))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "approvalID")
	approvalID, err := shared.NewApprovalRequestID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_APPROVAL_ID", err.Error())
		return
	}

	var req resolveApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	resolvedBy, err := shared.NewActorID(req.ResolvedBy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACTOR", err.Error())
		return
	}

	result, err := s.approvals.ResolveApproval(r.Context(), inbound.ResolveApprovalInput{
		ApprovalRequestID: approvalID,
		Approved:          req.Approved,
		ResolvedBy:        resolvedBy,
		Reason:            req.Reason,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "RESOLVE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, approvalRequestToResponse(result))
}
