package resilience

import (
	"context"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// AuditListener returns a TransitionListener that writes audit entries.
// Each transition becomes one audit entry, not tied to any task or workflow.
// The background context is used to avoid tying audit writes to the original
// RegisterAttempt request lifecycle.
func AuditListener(recorder outbound.AuditRecorder) TransitionListener {
	return func(t StateTransition) {
		ctx := context.Background()
		action, outcome := actionAndOutcome(t)

		actx := audit.NewAuditContext().
			Set("tool_name", t.Key.ToolName).
			Set("agent_role", t.Key.AgentRole).
			Set("from_state", string(t.From)).
			Set("to_state", string(t.To))

		if t.From == StateClosed && t.To == StateOpen {
			actx = actx.
				Set("consecutive_failures", t.ConsecutiveFailures).
				Set("failure_rate", t.FailureRate).
				Set("sample_size", t.SampleSize).
				Set("trip_reason", string(t.TripReason))
		}
		if t.To == StateClosed {
			actx = actx.Set("consecutive_successes", t.ConsecutiveSuccesses)
		}

		_ = recorder.Record(ctx, shared.ActorID("system"), action, outcome, actx, nil, nil)
	}
}

func actionAndOutcome(t StateTransition) (string, string) {
	switch {
	case t.From == StateClosed && t.To == StateOpen:
		return "circuit_breaker_opened", string(t.TripReason)
	case t.From == StateOpen && t.To == StateHalfOpen:
		return "circuit_breaker_half_opened", "cooldown_elapsed"
	case t.From == StateHalfOpen && t.To == StateClosed:
		return "circuit_breaker_closed", "probes_successful"
	case t.From == StateHalfOpen && t.To == StateOpen:
		return "circuit_breaker_opened", "probe_failed"
	default:
		return "circuit_breaker_transition", fmt.Sprintf("%s_to_%s", t.From, t.To)
	}
}
