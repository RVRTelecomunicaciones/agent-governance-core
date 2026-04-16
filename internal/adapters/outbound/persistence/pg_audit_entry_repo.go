package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.AuditEntryRepository = (*PgAuditEntryRepository)(nil)

// PgAuditEntryRepository implements AuditEntryRepository using PostgreSQL.
// Audit entries are append-only — no Update or Delete.
type PgAuditEntryRepository struct {
	pool *pgxpool.Pool
}

// NewPgAuditEntryRepository creates a new PgAuditEntryRepository.
func NewPgAuditEntryRepository(pool *pgxpool.Pool) *PgAuditEntryRepository {
	return &PgAuditEntryRepository{pool: pool}
}

func (r *PgAuditEntryRepository) Append(ctx context.Context, entry *audit.AuditEntry) error {
	contextJSON, err := json.Marshal(entry.Context())
	if err != nil {
		return fmt.Errorf("marshaling audit context: %w", err)
	}

	var taskID, workflowRunID *string
	if tid := entry.TaskID(); tid != nil {
		s := tid.String()
		taskID = &s
	}
	if wid := entry.WorkflowRunID(); wid != nil {
		s := wid.String()
		workflowRunID = &s
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO audit_entries (id, task_id, workflow_run_id, actor, action, outcome, context, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ID().String(),
		taskID,
		workflowRunID,
		entry.Actor().String(),
		entry.Action(),
		entry.Outcome(),
		contextJSON,
		entry.CreatedAt().Time,
	)
	if err != nil {
		return fmt.Errorf("appending audit entry: %w", err)
	}
	return nil
}

func (r *PgAuditEntryRepository) Query(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
	// Build dynamic WHERE clause
	var conditions []string
	var args []any
	argIdx := 1

	if filter.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argIdx))
		args = append(args, filter.TaskID.String())
		argIdx++
	}
	if filter.WorkflowRunID != nil {
		conditions = append(conditions, fmt.Sprintf("workflow_run_id = $%d", argIdx))
		args = append(args, filter.WorkflowRunID.String())
		argIdx++
	}
	if filter.Actor != nil {
		conditions = append(conditions, fmt.Sprintf("actor = $%d", argIdx))
		args = append(args, filter.Actor.String())
		argIdx++
	}
	if filter.Action != nil {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, *filter.Action)
		argIdx++
	}
	if filter.CreatedAfter != nil {
		conditions = append(conditions, fmt.Sprintf("created_at > $%d", argIdx))
		args = append(args, *filter.CreatedAfter)
		argIdx++
	}
	if filter.CreatedBefore != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", argIdx))
		args = append(args, *filter.CreatedBefore)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Build single query with COUNT(*) OVER() window function
	limitClause := ""
	if filter.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
		args = append(args, filter.Limit, filter.Offset)
	}

	query := fmt.Sprintf(`
		SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
		       COUNT(*) OVER() AS total_count
		FROM audit_entries %s
		ORDER BY created_at DESC
		%s`, whereClause, limitClause)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying audit entries: %w", err)
	}
	defer rows.Close()

	var entries []*audit.AuditEntry
	var total int
	for rows.Next() {
		var (
			id            string
			taskID        *string
			workflowRunID *string
			actor         string
			action        string
			outcome       string
			contextJSON   []byte
			createdAt     shared.Timestamp
			totalCount    int
		)

		if err := rows.Scan(&id, &taskID, &workflowRunID, &actor, &action, &outcome, &contextJSON, &createdAt.Time, &totalCount); err != nil {
			return nil, 0, fmt.Errorf("scanning audit entry: %w", err)
		}

		if total == 0 {
			total = totalCount
		}

		var actx audit.AuditContext
		if err := json.Unmarshal(contextJSON, &actx); err != nil {
			return nil, 0, fmt.Errorf("unmarshaling audit context: %w", err)
		}

		var tid *shared.TaskID
		if taskID != nil {
			t := shared.TaskID(*taskID)
			tid = &t
		}

		var wid *shared.WorkflowRunID
		if workflowRunID != nil {
			w := shared.WorkflowRunID(*workflowRunID)
			wid = &w
		}

		entries = append(entries, audit.ReconstructAuditEntry(
			shared.AuditEntryID(id),
			tid,
			wid,
			shared.ActorID(actor),
			action,
			outcome,
			actx,
			createdAt,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating audit entries: %w", err)
	}

	return entries, total, nil
}
