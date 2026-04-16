package http

import (
	"encoding/json"
	"net/http"

	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
)

type breakerResponse struct {
	ToolName            string  `json:"tool_name"`
	AgentRole           string  `json:"agent_role"`
	State               string  `json:"state"`
	OpenedAt            *string `json:"opened_at,omitempty"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	FailureRate         float64 `json:"failure_rate"`
	SampleSize          int     `json:"sample_size"`
	UpdatedAt           string  `json:"updated_at"`
}

func (s *Server) handleListBreakers(w http.ResponseWriter, r *http.Request) {
	filter := resilience.BreakerFilter{}

	if stateParam := r.URL.Query().Get("state"); stateParam != "" {
		validStates := map[string]bool{"closed": true, "open": true, "half_open": true}
		if !validStates[stateParam] {
			writeError(w, http.StatusBadRequest, "INVALID_STATE", "state must be one of: closed, open, half_open")
			return
		}
		bs := resilience.BreakerState(stateParam)
		filter.State = &bs
	}
	if tool := r.URL.Query().Get("tool_name"); tool != "" {
		filter.ToolName = &tool
	}
	if role := r.URL.Query().Get("agent_role"); role != "" {
		filter.AgentRole = &role
	}

	snapshots, err := s.queries.ListBreakers(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	items := make([]breakerResponse, 0, len(snapshots))
	for _, s := range snapshots {
		resp := breakerResponse{
			ToolName:            s.ToolName,
			AgentRole:           s.AgentRole,
			State:               string(s.State),
			ConsecutiveFailures: s.ConsecutiveFailures,
			FailureRate:         s.FailureRate,
			SampleSize:          s.SampleSize,
			UpdatedAt:           s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if s.OpenedAt != nil {
			openedStr := s.OpenedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
			resp.OpenedAt = &openedStr
		}
		items = append(items, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"total": len(items),
	})
}
