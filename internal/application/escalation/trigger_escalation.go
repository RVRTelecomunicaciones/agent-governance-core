// Package escalation contains the escalation trigger use case.
package escalation

import (
	"context"
	"fmt"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	escalationdomain "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/escalation"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

// EscalationService handles escalation trigger use cases.
type EscalationService struct {
	escalationRepo outbound.EscalationTriggerRepository
	idGen          outbound.IDGenerator
	clock          outbound.Clock
	auditRec       outbound.AuditRecorder
}

// NewEscalationService creates an EscalationService with all required dependencies.
func NewEscalationService(
	escalationRepo outbound.EscalationTriggerRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	auditRec outbound.AuditRecorder,
) *EscalationService {
	return &EscalationService{
		escalationRepo: escalationRepo,
		idGen:          idGen,
		clock:          clock,
		auditRec:       auditRec,
	}
}

// TriggerEscalation creates an escalation trigger, fires it immediately, persists, and audits.
func (s *EscalationService) TriggerEscalation(
	ctx context.Context,
	taskID shared.TaskID,
	condition escalationdomain.EscalationCondition,
	target escalationdomain.EscalationTarget,
) (*escalationdomain.EscalationTrigger, error) {
	now := s.clock.Now()

	trigger := escalationdomain.NewEscalationTrigger(
		s.idGen.NewEscalationTriggerID(),
		taskID,
		condition,
		target,
		now,
	)

	if err := trigger.Trigger(now); err != nil {
		return nil, fmt.Errorf("triggering escalation: %w", err)
	}

	if err := s.escalationRepo.Save(ctx, trigger); err != nil {
		return nil, fmt.Errorf("persisting escalation: %w", err)
	}

	tID := taskID
	_ = s.auditRec.Record(ctx, shared.ActorID("system"), "escalation_triggered", "triggered", audit.NewAuditContext(), &tID, nil)

	return trigger, nil
}
