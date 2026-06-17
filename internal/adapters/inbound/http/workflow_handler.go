package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/go-chi/chi/v5"
)

var validWorkflowStatuses = map[string]bool{
	string(workflow.StatusCreated):          true,
	string(workflow.StatusRouted):           true,
	string(workflow.StatusPolicyChecked):    true,
	string(workflow.StatusRunning):          true,
	string(workflow.StatusAwaitingApproval): true,
	string(workflow.StatusApproved):         true,
	string(workflow.StatusPaused):           true,
	string(workflow.StatusCompleted):        true,
	string(workflow.StatusFailed):           true,
	string(workflow.StatusKilled):           true,
	string(workflow.StatusQuarantined):      true,
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	statusParam := r.URL.Query().Get("status")
	limitParam := r.URL.Query().Get("limit")
	offsetParam := r.URL.Query().Get("offset")

	filter := outbound.WorkflowListFilter{
		Limit:  20,
		Offset: 0,
	}

	if statusParam != "" {
		if !validWorkflowStatuses[statusParam] {
			writeError(w, http.StatusBadRequest, "INVALID_STATUS", fmt.Sprintf("invalid status: %s", statusParam))
			return
		}
		status := workflow.WorkflowStatus(statusParam)
		filter.Status = &status
	}
	if limitParam != "" {
		if l, err := strconv.Atoi(limitParam); err == nil && l > 0 && l <= 100 {
			filter.Limit = l
		}
	}
	if offsetParam != "" {
		if o, err := strconv.Atoi(offsetParam); err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	workflows, total, err := s.queries.ListWorkflows(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	items := make([]workflowRunResponse, 0, len(workflows))
	for _, wf := range workflows {
		items = append(items, workflowRunToResponse(wf))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

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
