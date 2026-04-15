package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.TaskRepository = (*PgTaskRepository)(nil)

// PgTaskRepository implements TaskRepository using PostgreSQL.
type PgTaskRepository struct {
	pool *pgxpool.Pool
}

// NewPgTaskRepository creates a new PgTaskRepository.
func NewPgTaskRepository(pool *pgxpool.Pool) *PgTaskRepository {
	return &PgTaskRepository{pool: pool}
}

func (r *PgTaskRepository) Save(ctx context.Context, t *task.Task) error {
	metadata, err := json.Marshal(t.Metadata())
	if err != nil {
		return fmt.Errorf("marshaling task metadata: %w", err)
	}

	var parentID *string
	if p := t.ParentTaskID(); p != nil {
		s := p.String()
		parentID = &s
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO tasks (id, parent_task_id, type, title, scope, priority, risk_level, status, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		t.ID().String(),
		parentID,
		t.Type().String(),
		t.Title(),
		t.Scope().String(),
		t.Priority().String(),
		t.RiskLevel().String(),
		t.Status().String(),
		metadata,
		t.CreatedAt().Time,
		t.UpdatedAt().Time,
	)
	if err != nil {
		return fmt.Errorf("inserting task: %w", err)
	}
	return nil
}

func (r *PgTaskRepository) FindByID(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, parent_task_id, type, title, scope, priority, risk_level, status, metadata, created_at, updated_at
		FROM tasks WHERE id = $1`, id.String())
	return scanTask(row)
}

func (r *PgTaskRepository) FindByParentID(ctx context.Context, parentID shared.TaskID) ([]*task.Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, parent_task_id, type, title, scope, priority, risk_level, status, metadata, created_at, updated_at
		FROM tasks WHERE parent_task_id = $1`, parentID.String())
	if err != nil {
		return nil, fmt.Errorf("querying tasks by parent: %w", err)
	}
	defer rows.Close()

	var tasks []*task.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *PgTaskRepository) UpdateStatus(ctx context.Context, t *task.Task) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tasks SET status = $1, updated_at = $2 WHERE id = $3`,
		t.Status().String(),
		t.UpdatedAt().Time,
		t.ID().String(),
	)
	if err != nil {
		return fmt.Errorf("updating task status: %w", err)
	}
	return nil
}

// scannable is satisfied by both pgx.Row and pgx.Rows.
type scannable interface {
	Scan(dest ...any) error
}

func scanTask(row scannable) (*task.Task, error) {
	var (
		id         string
		parentID   *string
		taskType   string
		title      string
		scope      string
		priority   string
		riskLevel  string
		status     string
		metaJSON   []byte
		createdAt  shared.Timestamp
		updatedAt  shared.Timestamp
	)

	err := row.Scan(&id, &parentID, &taskType, &title, &scope, &priority, &riskLevel, &status, &metaJSON, &createdAt.Time, &updatedAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("task not found")
		}
		return nil, fmt.Errorf("scanning task: %w", err)
	}

	var metadata task.TaskMetadata
	if err := json.Unmarshal(metaJSON, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshaling task metadata: %w", err)
	}

	var pID *shared.TaskID
	if parentID != nil {
		tid := shared.TaskID(*parentID)
		pID = &tid
	}

	return task.Reconstruct(
		shared.TaskID(id),
		pID,
		task.TaskType(taskType),
		title,
		task.TaskScope(scope),
		shared.Priority(priority),
		shared.RiskLevel(riskLevel),
		task.TaskStatus(status),
		metadata,
		createdAt,
		updatedAt,
	), nil
}
