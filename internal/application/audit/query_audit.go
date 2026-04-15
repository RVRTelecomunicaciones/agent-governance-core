package audit

import (
	"context"

	domainaudit "github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// QueryAuditService provides read access to the audit trail.
type QueryAuditService struct {
	repo outbound.AuditEntryRepository
}

// NewQueryAuditService creates a new QueryAuditService.
func NewQueryAuditService(repo outbound.AuditEntryRepository) *QueryAuditService {
	return &QueryAuditService{repo: repo}
}

// QueryAuditTrail retrieves audit entries matching the given filter.
func (s *QueryAuditService) QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*domainaudit.AuditEntry, int, error) {
	return s.repo.Query(ctx, filter)
}
