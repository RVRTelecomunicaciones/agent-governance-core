package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

func (s *Server) handleGetWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "workflowID")
	wfID, err := shared.NewWorkflowRunID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WORKFLOW_ID", err.Error())
		return
	}

	result, err := s.queries.GetWorkflowStatus(r.Context(), wfID)
	if err != nil {
		writeError(w, http.StatusNotFound, "WORKFLOW_NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, workflowRunToResponse(result))
}

func (s *Server) handleKillWorkflow(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "workflowID")
	wfID, err := shared.NewWorkflowRunID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WORKFLOW_ID", err.Error())
		return
	}

	var req workflowActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	actor, err := shared.NewActorID(req.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACTOR", err.Error())
		return
	}

	if err := s.control.KillWorkflow(r.Context(), wfID, req.Reason, actor); err != nil {
		writeError(w, http.StatusInternalServerError, "KILL_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
}

func (s *Server) handlePauseWorkflow(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "workflowID")
	wfID, err := shared.NewWorkflowRunID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WORKFLOW_ID", err.Error())
		return
	}

	var req workflowActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	actor, err := shared.NewActorID(req.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACTOR", err.Error())
		return
	}

	if err := s.control.PauseWorkflow(r.Context(), wfID, req.Reason, actor); err != nil {
		writeError(w, http.StatusInternalServerError, "PAUSE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeWorkflow(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "workflowID")
	wfID, err := shared.NewWorkflowRunID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WORKFLOW_ID", err.Error())
		return
	}

	var req workflowActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	actor, err := shared.NewActorID(req.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACTOR", err.Error())
		return
	}

	if err := s.control.ResumeWorkflow(r.Context(), wfID, req.Reason, actor); err != nil {
		writeError(w, http.StatusInternalServerError, "RESUME_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *Server) handleRegisterAttempt(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "workflowID")
	wfID, err := shared.NewWorkflowRunID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WORKFLOW_ID", err.Error())
		return
	}

	var req registerAttemptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	// Build AttemptResult based on status
	var attemptResult execution.AttemptResult

	switch execution.AttemptStatus(req.Status) {
	case execution.AttemptStatusSuccess:
		attemptResult = execution.NewSuccessResult()

	case execution.AttemptStatusFailure, execution.AttemptStatusRetry:
		if req.FailureStage == nil {
			writeError(w, http.StatusBadRequest, "MISSING_FAILURE_STAGE", "failure_stage is required for failure/retry status")
			return
		}
		stage, err := shared.NewFailureStage(*req.FailureStage)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_FAILURE_STAGE", err.Error())
			return
		}
		if req.FailureCode == nil || *req.FailureCode == "" {
			writeError(w, http.StatusBadRequest, "MISSING_FAILURE_CODE", "failure_code is required for failure/retry status")
			return
		}
		if req.Retryable == nil {
			writeError(w, http.StatusBadRequest, "MISSING_RETRYABLE", "retryable is required for failure/retry status")
			return
		}

		if execution.AttemptStatus(req.Status) == execution.AttemptStatusFailure {
			attemptResult, err = execution.NewFailureResult(stage, *req.FailureCode, *req.Retryable)
		} else {
			attemptResult, err = execution.NewRetryResult(stage, *req.FailureCode)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ATTEMPT", err.Error())
			return
		}

	default:
		writeError(w, http.StatusBadRequest, "INVALID_ATTEMPT_STATUS", "status must be success, failure, or retry")
		return
	}

	// Apply optional fields
	if req.ToolName != nil {
		attemptResult = attemptResult.WithToolName(*req.ToolName)
	}
	if req.StrategyUsed != nil {
		attemptResult = attemptResult.WithStrategy(*req.StrategyUsed)
	}
	if req.AgentRole != nil {
		attemptResult = attemptResult.WithAgentRole(*req.AgentRole)
	}
	if req.Detail != nil {
		attemptResult = attemptResult.WithDetail(*req.Detail)
	}

	result, err := s.control.RegisterAttempt(r.Context(), wfID, attemptResult)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "REGISTER_ATTEMPT_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, workflowRunToResponse(result))
}
