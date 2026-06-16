package http

import (
	"encoding/json"
	"net/http"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req submitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	taskType, err := task.NewTaskType(req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_TYPE", err.Error())
		return
	}
	taskScope, err := task.NewTaskScope(req.Scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_SCOPE", err.Error())
		return
	}
	priority, err := shared.NewPriority(req.Priority)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PRIORITY", err.Error())
		return
	}

	result, err := s.governance.SubmitTask(r.Context(), inbound.SubmitTaskInput{
		Type:     taskType,
		Title:    req.Title,
		Scope:    taskScope,
		Priority: priority,
		Metadata: task.TaskMetadata(req.Metadata),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SUBMIT_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, taskToResponse(result))
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "taskID")
	taskID, err := shared.NewTaskID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
		return
	}

	result, err := s.queries.GetTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(result))
}

func (s *Server) handleRouteTask(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "taskID")
	taskID, err := shared.NewTaskID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
		return
	}

	result, err := s.governance.RouteTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ROUTE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, routingDecisionToResponse(result))
}

func (s *Server) handleEvaluatePolicy(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "taskID")
	taskID, err := shared.NewTaskID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
		return
	}

	var req evaluatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if req.Action == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ACTION", "action is required")
		return
	}

	result, err := s.governance.EvaluatePolicy(r.Context(), taskID, req.Action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVALUATE_POLICY_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, policyDecisionToResponse(result))
}

func (s *Server) handleStartWorkflow(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "taskID")
	taskID, err := shared.NewTaskID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
		return
	}

	result, err := s.governance.StartWorkflow(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "START_WORKFLOW_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, workflowRunToResponse(result))
}

func (s *Server) handleProcessTask(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "taskID")
	taskID, err := shared.NewTaskID(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_ID", err.Error())
		return
	}
	_ = taskID // ProcessTask uses SubmitTaskInput, not taskID directly

	var req submitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	taskType, err := task.NewTaskType(req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_TYPE", err.Error())
		return
	}
	taskScope, err := task.NewTaskScope(req.Scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TASK_SCOPE", err.Error())
		return
	}
	priority, err := shared.NewPriority(req.Priority)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PRIORITY", err.Error())
		return
	}

	if req.Action == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ACTION", "action is required for process")
		return
	}

	result, err := s.governance.ProcessTask(r.Context(), inbound.SubmitTaskInput{
		Type:     taskType,
		Title:    req.Title,
		Scope:    taskScope,
		Priority: priority,
		Metadata: task.TaskMetadata(req.Metadata),
	}, req.Action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PROCESS_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, processTaskResultToResponse(result))
}
