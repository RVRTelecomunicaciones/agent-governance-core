package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/govdecisions"
)

// PhaseDecisionsService is the inbound port the /governance/v1 facade calls.
// Defined here (not in ports/inbound) because the facade is orchestator-facing
// and Sprint 3 may merge it into the existing policy port; keeping the
// interface local avoids premature stabilization.
type PhaseDecisionsService interface {
	EvaluatePhase(ctx context.Context, in govdecisions.PhaseDecisionInput) (*govdecisions.Decision, error)
	EvaluateSensitive(ctx context.Context, in govdecisions.SensitiveDecisionInput) (*govdecisions.Decision, error)
	ApprovalStatusFor(ctx context.Context, changeID, phaseID string) (govdecisions.ApprovalStatus, error)
}

// --- request/response DTOs (orchestator wire contract) ---

type decisionPhaseRequest struct {
	ChangeID        string `json:"change_id"`
	PhaseType       string `json:"phase_type"`
	TaskDescription string `json:"task_description,omitempty"`
	Sensitive       bool   `json:"sensitive,omitempty"`
}

type decisionSensitiveRequest struct {
	ChangeID   string `json:"change_id"`
	Capability string `json:"capability"`
	Payload    []byte `json:"payload,omitempty"`
}

type approvalGatePayload struct {
	URL string `json:"url"`
}

type decisionPayload struct {
	Decision    string               `json:"decision"`
	AgentRole   string               `json:"agent_role,omitempty"`
	Strategy    string               `json:"strategy,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	Constraints map[string]any       `json:"constraints,omitempty"`
	Approval    *approvalGatePayload `json:"approval,omitempty"`
}

type approvalStatusPayload struct {
	Status string `json:"status"`
}

func toDecisionPayload(d *govdecisions.Decision) decisionPayload {
	return decisionPayload{
		Decision:    string(d.Decision),
		AgentRole:   d.AgentRole,
		Strategy:    d.Strategy,
		Reason:      d.Reason,
		Constraints: d.Constraints,
	}
}

// --- handlers ---

func (s *Server) handleDecisionPhase(w http.ResponseWriter, r *http.Request) {
	if s.phaseDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED", "phase decisions service not wired")
		return
	}
	var req decisionPhaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	dec, err := s.phaseDecisions.EvaluatePhase(r.Context(), govdecisions.PhaseDecisionInput{
		ChangeID:        req.ChangeID,
		PhaseType:       req.PhaseType,
		TaskDescription: req.TaskDescription,
		Sensitive:       req.Sensitive,
	})
	if err != nil {
		if errors.Is(err, govdecisions.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "DECISION_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDecisionPayload(dec))
}

func (s *Server) handleDecisionSensitive(w http.ResponseWriter, r *http.Request) {
	if s.phaseDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED", "phase decisions service not wired")
		return
	}
	var req decisionSensitiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	dec, err := s.phaseDecisions.EvaluateSensitive(r.Context(), govdecisions.SensitiveDecisionInput{
		ChangeID:   req.ChangeID,
		Capability: req.Capability,
		Payload:    req.Payload,
	})
	if err != nil {
		if errors.Is(err, govdecisions.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "DECISION_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDecisionPayload(dec))
}

func (s *Server) handleApprovalStatus(w http.ResponseWriter, r *http.Request) {
	if s.phaseDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED", "phase decisions service not wired")
		return
	}
	changeID := chi.URLParam(r, "change_id")
	phaseID := chi.URLParam(r, "phase_id")
	status, err := s.phaseDecisions.ApprovalStatusFor(r.Context(), changeID, phaseID)
	if err != nil {
		if errors.Is(err, govdecisions.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "APPROVAL_LOOKUP_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, approvalStatusPayload{Status: string(status)})
}
