package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.RoutingDecisionRepository = (*PgRoutingDecisionRepository)(nil)

// PgRoutingDecisionRepository implements RoutingDecisionRepository using PostgreSQL.
// RoutingDecision is immutable — no Update method.
type PgRoutingDecisionRepository struct {
	pool *pgxpool.Pool
}

// NewPgRoutingDecisionRepository creates a new PgRoutingDecisionRepository.
func NewPgRoutingDecisionRepository(pool *pgxpool.Pool) *PgRoutingDecisionRepository {
	return &PgRoutingDecisionRepository{pool: pool}
}

func (r *PgRoutingDecisionRepository) Save(ctx context.Context, rd *routing.RoutingDecision) error {
	strategiesJSON, err := json.Marshal(rd.EvaluatedStrategies())
	if err != nil {
		return fmt.Errorf("marshaling evaluated strategies: %w", err)
	}

	constraintsJSON, err := json.Marshal(rd.Constraints())
	if err != nil {
		return fmt.Errorf("marshaling routing constraints: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO routing_decisions (id, task_id, evaluated_strategies, selected_strategy, selected_agent_role, reason, constraints, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rd.ID().String(),
		rd.TaskID().String(),
		strategiesJSON,
		rd.SelectedStrategy().String(),
		rd.SelectedAgentRole().String(),
		rd.Reason(),
		constraintsJSON,
		rd.CreatedAt().Time,
	)
	if err != nil {
		return fmt.Errorf("inserting routing decision: %w", err)
	}
	return nil
}

func (r *PgRoutingDecisionRepository) FindByTaskID(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, evaluated_strategies, selected_strategy, selected_agent_role, reason, constraints, created_at
		FROM routing_decisions WHERE task_id = $1`, taskID.String())
	return scanRoutingDecision(row)
}

func scanRoutingDecision(row scannable) (*routing.RoutingDecision, error) {
	var (
		id               string
		taskID           string
		strategiesJSON   []byte
		selectedStrategy string
		selectedRole     string
		reason           string
		constraintsJSON  []byte
		createdAt        shared.Timestamp
	)

	err := row.Scan(&id, &taskID, &strategiesJSON, &selectedStrategy, &selectedRole, &reason, &constraintsJSON, &createdAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("routing decision not found")
		}
		return nil, fmt.Errorf("scanning routing decision: %w", err)
	}

	var strategies []routing.StrategyEvaluation
	if err := json.Unmarshal(strategiesJSON, &strategies); err != nil {
		return nil, fmt.Errorf("unmarshaling evaluated strategies: %w", err)
	}

	var constraints []routing.RoutingConstraint
	if err := json.Unmarshal(constraintsJSON, &constraints); err != nil {
		return nil, fmt.Errorf("unmarshaling routing constraints: %w", err)
	}

	return routing.Reconstruct(
		shared.RoutingDecisionID(id),
		shared.TaskID(taskID),
		strategies,
		routing.RoutingStrategy(selectedStrategy),
		routing.AgentRole(selectedRole),
		reason,
		constraints,
		createdAt,
	), nil
}
