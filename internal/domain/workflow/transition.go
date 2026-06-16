package workflow

import "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"

// WorkflowTransition records a single state transition in a workflow run's history.
type WorkflowTransition struct {
	From      WorkflowStatus
	To        WorkflowStatus
	Reason    string
	Actor     shared.ActorID
	Timestamp shared.Timestamp
}
