package http

import (
	"encoding/json"
	"net/http"
	"time"

	escalationdomain "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/escalation"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleTriggerEscalation(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "taskID")
	taskID, err := shared.NewTaskID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
		return
	}

	var req triggerEscalationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	if req.ConditionType == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CONDITION_TYPE", "condition_type is required")
		return
	}

	target, err := escalationdomain.NewEscalationTarget(req.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ESCALATION_TARGET", err.Error())
		return
	}

	condition := escalationdomain.EscalationCondition{
		Type:       req.ConditionType,
		Parameters: req.ConditionParams,
	}

	trigger, err := s.escalation.TriggerEscalation(r.Context(), taskID, condition, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ESCALATION_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, escalationTriggerToResponse(trigger))
}

// --- Request/Response DTOs ---

type triggerEscalationRequest struct {
	ConditionType   string         `json:"condition_type"`
	ConditionParams map[string]any `json:"condition_params,omitempty"`
	Target          string         `json:"target"`
}

type escalationTriggerResponse struct {
	ID          string  `json:"id"`
	TaskID      string  `json:"task_id"`
	Condition   string  `json:"condition_type"`
	Target      string  `json:"target"`
	Status      string  `json:"status"`
	TriggeredAt *string `json:"triggered_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func escalationTriggerToResponse(et *escalationdomain.EscalationTrigger) escalationTriggerResponse {
	resp := escalationTriggerResponse{
		ID:        et.ID().String(),
		TaskID:    et.TaskID().String(),
		Condition: et.Condition().Type,
		Target:    et.Target().String(),
		Status:    et.Status().String(),
		CreatedAt: et.CreatedAt().Format(time.RFC3339),
	}
	if et.TriggeredAt() != nil {
		s := et.TriggeredAt().Format(time.RFC3339)
		resp.TriggeredAt = &s
	}
	return resp
}
