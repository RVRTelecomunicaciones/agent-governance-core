package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

var _ outbound.PhaseApprovalRepository = (*PgPhaseApprovalRepository)(nil)

// PgPhaseApprovalRepository reads phase_approvals rows for M-E0 polling. See
// outbound.PhaseApprovalRepository docs and migration 011.
type PgPhaseApprovalRepository struct {
	pool *pgxpool.Pool
}

// NewPgPhaseApprovalRepository constructs the repo.
func NewPgPhaseApprovalRepository(pool *pgxpool.Pool) *PgPhaseApprovalRepository {
	return &PgPhaseApprovalRepository{pool: pool}
}

// Find returns the row for (changeID, phaseID) or (nil, nil) when absent.
// Absent rows mean "no explicit gate" — V1 callers treat that as granted.
func (r *PgPhaseApprovalRepository) Find(ctx context.Context, changeID, phaseID string) (*outbound.PhaseApprovalRecord, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT change_id, phase_id, status, reason, decided_by, decided_at, created_at
		FROM phase_approvals WHERE change_id = $1 AND phase_id = $2`,
		changeID, phaseID,
	)
	var (
		cid       string
		pid       string
		status    string
		reason    *string
		decidedBy *string
		decidedAt *time.Time
		createdAt time.Time
	)
	if err := row.Scan(&cid, &pid, &status, &reason, &decidedBy, &decidedAt, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning phase approval: %w", err)
	}
	rec := &outbound.PhaseApprovalRecord{
		ChangeID:  cid,
		PhaseID:   pid,
		Status:    status,
		CreatedAt: createdAt,
		DecidedAt: decidedAt,
	}
	if reason != nil {
		rec.Reason = *reason
	}
	if decidedBy != nil {
		rec.DecidedBy = *decidedBy
	}
	return rec, nil
}
