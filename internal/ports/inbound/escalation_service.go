package inbound

import (
	"context"

	escalationdomain "github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// EscalationPort defines the escalation trigger interface.
type EscalationPort interface {
	TriggerEscalation(
		ctx context.Context,
		taskID shared.TaskID,
		condition escalationdomain.EscalationCondition,
		target escalationdomain.EscalationTarget,
	) (*escalationdomain.EscalationTrigger, error)
}
