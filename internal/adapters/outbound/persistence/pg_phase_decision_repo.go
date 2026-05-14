package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.PhaseDecisionRepository = (*PgPhaseDecisionRepository)(nil)

// PgPhaseDecisionRepository writes phase_decisions rows. M-E0 facade — see
// outbound.PhaseDecisionRepository docs and migration 010.
type PgPhaseDecisionRepository struct {
	pool *pgxpool.Pool
}

// NewPgPhaseDecisionRepository constructs the repo.
func NewPgPhaseDecisionRepository(pool *pgxpool.Pool) *PgPhaseDecisionRepository {
	return &PgPhaseDecisionRepository{pool: pool}
}

// Save persists a phase decision row.
func (r *PgPhaseDecisionRepository) Save(ctx context.Context, rec outbound.PhaseDecisionRecord) error {
	var capability any
	if rec.Capability != "" {
		capability = rec.Capability
	}
	var agentRole any
	if rec.AgentRole != "" {
		agentRole = rec.AgentRole
	}
	var strategy any
	if rec.Strategy != "" {
		strategy = rec.Strategy
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO phase_decisions
			(id, change_id, phase_type, capability, sensitive, decision, agent_role, strategy, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rec.ID,
		rec.ChangeID,
		rec.PhaseType,
		capability,
		rec.Sensitive,
		rec.Decision,
		agentRole,
		strategy,
		rec.Reason,
		rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting phase decision: %w", err)
	}
	return nil
}
