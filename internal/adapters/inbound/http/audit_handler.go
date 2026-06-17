package http

import (
	"net/http"
	"strconv"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

func (s *Server) handleQueryAuditTrail(w http.ResponseWriter, r *http.Request) {
	var filter outbound.AuditFilter

	if v := r.URL.Query().Get("task_id"); v != "" {
		taskID, err := shared.NewTaskID(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
			return
		}
		filter.TaskID = &taskID
	}

	if v := r.URL.Query().Get("workflow_run_id"); v != "" {
		wfID, err := shared.NewWorkflowRunID(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_WORKFLOW_ID", err.Error())
			return
		}
		filter.WorkflowRunID = &wfID
	}

	if v := r.URL.Query().Get("actor"); v != "" {
		actor, err := shared.NewActorID(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ACTOR", err.Error())
			return
		}
		filter.Actor = &actor
	}

	if v := r.URL.Query().Get("action"); v != "" {
		filter.Action = &v
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil || limit < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be a non-negative integer")
			return
		}
		filter.Limit = limit
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err := strconv.Atoi(v)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_OFFSET", "offset must be a non-negative integer")
			return
		}
		filter.Offset = offset
	}

	entries, total, err := s.queries.QueryAuditTrail(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	resp := make([]auditEntryResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, auditEntryToResponse(e))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": resp,
		"total":   total,
	})
}
