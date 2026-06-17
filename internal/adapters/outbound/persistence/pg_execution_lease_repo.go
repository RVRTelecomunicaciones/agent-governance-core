package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ outbound.ExecutionLeaseRepository = (*PgExecutionLeaseRepository)(nil)

// PgExecutionLeaseRepository implements ExecutionLeaseRepository using PostgreSQL.
type PgExecutionLeaseRepository struct {
	pool *pgxpool.Pool
}

// NewPgExecutionLeaseRepository creates a new PgExecutionLeaseRepository.
func NewPgExecutionLeaseRepository(pool *pgxpool.Pool) *PgExecutionLeaseRepository {
	return &PgExecutionLeaseRepository{pool: pool}
}

func (r *PgExecutionLeaseRepository) Save(ctx context.Context, lease *execution.ExecutionLease) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO execution_leases (id, workflow_run_id, timeout_budget_ms, retry_budget, attempts_used, time_elapsed_ms, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		lease.ID.String(),
		lease.WorkflowRunID.String(),
		lease.TimeoutBudget.Milliseconds(),
		lease.RetryBudget.MaxAttempts,
		lease.AttemptsUsed,
		lease.TimeElapsed.Milliseconds(),
		string(lease.Status),
		lease.CreatedAt.Time,
	)
	if err != nil {
		return fmt.Errorf("inserting execution lease: %w", err)
	}
	return nil
}

func (r *PgExecutionLeaseRepository) FindByWorkflowRunID(ctx context.Context, wfID shared.WorkflowRunID) (*execution.ExecutionLease, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, workflow_run_id, timeout_budget_ms, retry_budget, attempts_used, time_elapsed_ms, status, created_at
		FROM execution_leases WHERE workflow_run_id = $1`, wfID.String())
	return scanExecutionLease(row)
}

func (r *PgExecutionLeaseRepository) Update(ctx context.Context, lease *execution.ExecutionLease) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE execution_leases
		SET timeout_budget_ms = $1, retry_budget = $2, attempts_used = $3, time_elapsed_ms = $4, status = $5
		WHERE id = $6`,
		lease.TimeoutBudget.Milliseconds(),
		lease.RetryBudget.MaxAttempts,
		lease.AttemptsUsed,
		lease.TimeElapsed.Milliseconds(),
		string(lease.Status),
		lease.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("updating execution lease: %w", err)
	}
	return nil
}

func scanExecutionLease(row scannable) (*execution.ExecutionLease, error) {
	var (
		id              string
		workflowRunID   string
		timeoutBudgetMs int64
		retryBudget     int
		attemptsUsed    int
		timeElapsedMs   int64
		status          string
		createdAt       shared.Timestamp
	)

	err := row.Scan(&id, &workflowRunID, &timeoutBudgetMs, &retryBudget, &attemptsUsed, &timeElapsedMs, &status, &createdAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("execution lease not found")
		}
		return nil, fmt.Errorf("scanning execution lease: %w", err)
	}

	return execution.ReconstructExecutionLease(
		shared.ExecutionLeaseID(id),
		shared.WorkflowRunID(workflowRunID),
		time.Duration(timeoutBudgetMs)*time.Millisecond,
		execution.RetryBudget{MaxAttempts: retryBudget},
		attemptsUsed,
		time.Duration(timeElapsedMs)*time.Millisecond,
		execution.LeaseStatus(status),
		createdAt,
	), nil
}
