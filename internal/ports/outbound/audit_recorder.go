package outbound

import (
	"context"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// AuditRecorder is a transversal service port injected into all use cases.
// It provides the mechanism for use cases to emit audit entries without
// depending on AuditEntryRepository directly.
type AuditRecorder interface {
	Record(ctx context.Context, actor shared.ActorID, action, outcome string, auditCtx audit.AuditContext, taskID *shared.TaskID, wfID *shared.WorkflowRunID) error
}
