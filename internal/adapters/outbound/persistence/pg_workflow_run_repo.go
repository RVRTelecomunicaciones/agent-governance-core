package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.WorkflowRunRepository = (*PgWorkflowRunRepository)(nil)

// PgWorkflowRunRepository implements WorkflowRunRepository using PostgreSQL.
type PgWorkflowRunRepository struct {
	pool *pgxpool.Pool
}

// NewPgWorkflowRunRepository creates a new PgWorkflowRunRepository.
func NewPgWorkflowRunRepository(pool *pgxpool.Pool) *PgWorkflowRunRepository {
	return &PgWorkflowRunRepository{pool: pool}
}

// transitionJSON is a helper for JSON serialization of WorkflowTransition.
type transitionJSON struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason"`
	Actor     string `json:"actor"`
	Timestamp string `json:"timestamp"`
}

func (r *PgWorkflowRunRepository) Save(ctx context.Context, wf *workflow.WorkflowRun) error {
	transitionsData, err := marshalTransitions(wf.Transitions)
	if err != nil {
		return err
	}

	var routingID, policyID *string
	if wf.RoutingDecisionID != nil {
		s := wf.RoutingDecisionID.String()
		routingID = &s
	}
	if wf.PolicyDecisionID != nil {
		s := wf.PolicyDecisionID.String()
		policyID = &s
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO workflow_runs (id, task_id, status, routing_decision_id, policy_decision_id, current_step_index, transitions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		wf.ID.String(),
		wf.TaskID.String(),
		string(wf.Status),
		routingID,
		policyID,
		wf.CurrentStepIndex,
		transitionsData,
		wf.CreatedAt.Time,
		wf.UpdatedAt.Time,
	)
	if err != nil {
		return fmt.Errorf("inserting workflow run: %w", err)
	}
	return nil
}

func (r *PgWorkflowRunRepository) FindByID(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, status, routing_decision_id, policy_decision_id, current_step_index, transitions, created_at, updated_at
		FROM workflow_runs WHERE id = $1`, id.String())
	return scanWorkflowRun(row)
}

func (r *PgWorkflowRunRepository) FindByTaskID(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, status, routing_decision_id, policy_decision_id, current_step_index, transitions, created_at, updated_at
		FROM workflow_runs WHERE task_id = $1`, taskID.String())
	return scanWorkflowRun(row)
}

func (r *PgWorkflowRunRepository) Update(ctx context.Context, wf *workflow.WorkflowRun) error {
	transitionsData, err := marshalTransitions(wf.Transitions)
	if err != nil {
		return err
	}

	var routingID, policyID *string
	if wf.RoutingDecisionID != nil {
		s := wf.RoutingDecisionID.String()
		routingID = &s
	}
	if wf.PolicyDecisionID != nil {
		s := wf.PolicyDecisionID.String()
		policyID = &s
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = $1, routing_decision_id = $2, policy_decision_id = $3,
		    current_step_index = $4, transitions = $5, updated_at = $6
		WHERE id = $7`,
		string(wf.Status),
		routingID,
		policyID,
		wf.CurrentStepIndex,
		transitionsData,
		wf.UpdatedAt.Time,
		wf.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("updating workflow run: %w", err)
	}
	return nil
}

func (r *PgWorkflowRunRepository) List(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}
	if filter.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argIdx))
		args = append(args, filter.TaskID.String())
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limitClause := ""
	if filter.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
		args = append(args, filter.Limit, filter.Offset)
	}

	query := fmt.Sprintf(`
		SELECT id, task_id, status, routing_decision_id, policy_decision_id,
		       current_step_index, transitions, created_at, updated_at,
		       COUNT(*) OVER() AS total_count
		FROM workflow_runs %s
		ORDER BY updated_at DESC
		%s`, whereClause, limitClause)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing workflow runs: %w", err)
	}
	defer rows.Close()

	var results []*workflow.WorkflowRun
	var total int
	for rows.Next() {
		var (
			id               string
			taskID           string
			status           string
			routingID        *string
			policyID         *string
			currentStepIndex int
			transitionsJSON  []byte
			createdAt        shared.Timestamp
			updatedAt        shared.Timestamp
			totalCount       int
		)

		if err := rows.Scan(&id, &taskID, &status, &routingID, &policyID, &currentStepIndex, &transitionsJSON, &createdAt.Time, &updatedAt.Time, &totalCount); err != nil {
			return nil, 0, fmt.Errorf("scanning workflow run: %w", err)
		}

		if total == 0 {
			total = totalCount
		}

		transitions, err := unmarshalTransitions(transitionsJSON)
		if err != nil {
			return nil, 0, err
		}

		var rdID *shared.RoutingDecisionID
		if routingID != nil {
			rid := shared.RoutingDecisionID(*routingID)
			rdID = &rid
		}

		var pdID *shared.PolicyDecisionID
		if policyID != nil {
			pid := shared.PolicyDecisionID(*policyID)
			pdID = &pid
		}

		results = append(results, workflow.ReconstructWorkflowRun(
			shared.WorkflowRunID(id),
			shared.TaskID(taskID),
			workflow.WorkflowStatus(status),
			rdID,
			pdID,
			currentStepIndex,
			transitions,
			createdAt,
			updatedAt,
		))
	}

	return results, total, nil
}

func scanWorkflowRun(row scannable) (*workflow.WorkflowRun, error) {
	var (
		id               string
		taskID           string
		status           string
		routingID        *string
		policyID         *string
		currentStepIndex int
		transitionsJSON  []byte
		createdAt        shared.Timestamp
		updatedAt        shared.Timestamp
	)

	err := row.Scan(&id, &taskID, &status, &routingID, &policyID, &currentStepIndex, &transitionsJSON, &createdAt.Time, &updatedAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("workflow run not found")
		}
		return nil, fmt.Errorf("scanning workflow run: %w", err)
	}

	transitions, err := unmarshalTransitions(transitionsJSON)
	if err != nil {
		return nil, err
	}

	var rdID *shared.RoutingDecisionID
	if routingID != nil {
		rid := shared.RoutingDecisionID(*routingID)
		rdID = &rid
	}

	var pdID *shared.PolicyDecisionID
	if policyID != nil {
		pid := shared.PolicyDecisionID(*policyID)
		pdID = &pid
	}

	return workflow.ReconstructWorkflowRun(
		shared.WorkflowRunID(id),
		shared.TaskID(taskID),
		workflow.WorkflowStatus(status),
		rdID,
		pdID,
		currentStepIndex,
		transitions,
		createdAt,
		updatedAt,
	), nil
}

func marshalTransitions(transitions []workflow.WorkflowTransition) ([]byte, error) {
	items := make([]transitionJSON, len(transitions))
	for i, t := range transitions {
		items[i] = transitionJSON{
			From:      string(t.From),
			To:        string(t.To),
			Reason:    t.Reason,
			Actor:     string(t.Actor),
			Timestamp: t.Timestamp.Time.Format("2006-01-02T15:04:05.999999999Z07:00"),
		}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("marshaling transitions: %w", err)
	}
	return data, nil
}

func unmarshalTransitions(data []byte) ([]workflow.WorkflowTransition, error) {
	var items []transitionJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshaling transitions: %w", err)
	}

	transitions := make([]workflow.WorkflowTransition, len(items))
	for i, item := range items {
		t, err := shared.NewTimestamp(mustParseTime(item.Timestamp))
		if err != nil {
			return nil, fmt.Errorf("parsing transition timestamp: %w", err)
		}
		transitions[i] = workflow.WorkflowTransition{
			From:      workflow.WorkflowStatus(item.From),
			To:        workflow.WorkflowStatus(item.To),
			Reason:    item.Reason,
			Actor:     shared.ActorID(item.Actor),
			Timestamp: t,
		}
	}
	return transitions, nil
}
