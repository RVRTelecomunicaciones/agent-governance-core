package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.EscalationTriggerRepository = (*PgEscalationTriggerRepository)(nil)

// PgEscalationTriggerRepository implements EscalationTriggerRepository using PostgreSQL.
type PgEscalationTriggerRepository struct {
	pool *pgxpool.Pool
}

// NewPgEscalationTriggerRepository creates a new PgEscalationTriggerRepository.
func NewPgEscalationTriggerRepository(pool *pgxpool.Pool) *PgEscalationTriggerRepository {
	return &PgEscalationTriggerRepository{pool: pool}
}

func (r *PgEscalationTriggerRepository) Save(ctx context.Context, trigger *escalation.EscalationTrigger) error {
	conditionJSON, err := json.Marshal(trigger.Condition())
	if err != nil {
		return fmt.Errorf("marshaling escalation condition: %w", err)
	}

	var triggeredAt *time.Time
	if ta := trigger.TriggeredAt(); ta != nil {
		triggeredAt = &ta.Time
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO escalation_triggers (id, task_id, condition, target, status, triggered_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		trigger.ID().String(),
		trigger.TaskID().String(),
		conditionJSON,
		trigger.Target().String(),
		trigger.Status().String(),
		triggeredAt,
		trigger.CreatedAt().Time,
	)
	if err != nil {
		return fmt.Errorf("inserting escalation trigger: %w", err)
	}
	return nil
}

func (r *PgEscalationTriggerRepository) FindByTaskID(ctx context.Context, taskID shared.TaskID) ([]*escalation.EscalationTrigger, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, task_id, condition, target, status, triggered_at, created_at
		FROM escalation_triggers WHERE task_id = $1`, taskID.String())
	if err != nil {
		return nil, fmt.Errorf("querying escalation triggers by task: %w", err)
	}
	defer rows.Close()

	var triggers []*escalation.EscalationTrigger
	for rows.Next() {
		t, err := scanEscalationTrigger(rows)
		if err != nil {
			return nil, err
		}
		triggers = append(triggers, t)
	}
	return triggers, rows.Err()
}

func (r *PgEscalationTriggerRepository) Update(ctx context.Context, trigger *escalation.EscalationTrigger) error {
	var triggeredAt *time.Time
	if ta := trigger.TriggeredAt(); ta != nil {
		triggeredAt = &ta.Time
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE escalation_triggers
		SET status = $1, triggered_at = $2
		WHERE id = $3`,
		trigger.Status().String(),
		triggeredAt,
		trigger.ID().String(),
	)
	if err != nil {
		return fmt.Errorf("updating escalation trigger: %w", err)
	}
	return nil
}

func scanEscalationTrigger(row scannable) (*escalation.EscalationTrigger, error) {
	var (
		id            string
		taskID        string
		conditionJSON []byte
		target        string
		status        string
		triggeredAt   *time.Time
		createdAt     shared.Timestamp
	)

	err := row.Scan(&id, &taskID, &conditionJSON, &target, &status, &triggeredAt, &createdAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("escalation trigger not found")
		}
		return nil, fmt.Errorf("scanning escalation trigger: %w", err)
	}

	var condition escalation.EscalationCondition
	if err := json.Unmarshal(conditionJSON, &condition); err != nil {
		return nil, fmt.Errorf("unmarshaling escalation condition: %w", err)
	}

	var trigAt *shared.Timestamp
	if triggeredAt != nil {
		ts := shared.Timestamp{Time: *triggeredAt}
		trigAt = &ts
	}

	return escalation.ReconstructEscalationTrigger(
		shared.EscalationTriggerID(id),
		shared.TaskID(taskID),
		condition,
		escalation.EscalationTarget(target),
		escalation.EscalationStatus(status),
		trigAt,
		createdAt,
	), nil
}
