package audit

import (
	"context"

	domainaudit "github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.AuditRecorder = (*RecordAuditService)(nil)

// RecordAuditService is a transversal application service injected into all use cases
// via the AuditRecorder port. It creates AuditEntry aggregates and persists them.
type RecordAuditService struct {
	repo  outbound.AuditEntryRepository
	idGen outbound.IDGenerator
	clock outbound.Clock
}

// NewRecordAuditService creates a new RecordAuditService.
func NewRecordAuditService(repo outbound.AuditEntryRepository, idGen outbound.IDGenerator, clock outbound.Clock) *RecordAuditService {
	return &RecordAuditService{repo: repo, idGen: idGen, clock: clock}
}

// Record creates an AuditEntry and appends it to the repository.
func (s *RecordAuditService) Record(ctx context.Context, actor shared.ActorID, action, outcome string, auditCtx domainaudit.AuditContext, taskID *shared.TaskID, wfID *shared.WorkflowRunID) error {
	auditCtx = auditCtx.WithTraceInfo(ctx)

	entry, err := domainaudit.NewAuditEntry(s.idGen.NewAuditEntryID(), actor, action, outcome, auditCtx, s.clock.Now())
	if err != nil {
		return err
	}
	if taskID != nil {
		entry = entry.WithTaskID(*taskID)
	}
	if wfID != nil {
		entry = entry.WithWorkflowRunID(*wfID)
	}
	return s.repo.Append(ctx, entry)
}
