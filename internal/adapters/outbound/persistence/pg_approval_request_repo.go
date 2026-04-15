package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.ApprovalRequestRepository = (*PgApprovalRequestRepository)(nil)

// PgApprovalRequestRepository implements ApprovalRequestRepository using PostgreSQL.
type PgApprovalRequestRepository struct {
	pool *pgxpool.Pool
}

// NewPgApprovalRequestRepository creates a new PgApprovalRequestRepository.
func NewPgApprovalRequestRepository(pool *pgxpool.Pool) *PgApprovalRequestRepository {
	return &PgApprovalRequestRepository{pool: pool}
}

func (r *PgApprovalRequestRepository) Save(ctx context.Context, req *approval.ApprovalRequest) error {
	approverJSON, err := json.Marshal(req.RequiredApprover())
	if err != nil {
		return fmt.Errorf("marshaling required approver: %w", err)
	}

	var resolutionJSON []byte
	if res := req.Resolution(); res != nil {
		resolutionJSON, err = json.Marshal(res)
		if err != nil {
			return fmt.Errorf("marshaling resolution: %w", err)
		}
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO approval_requests (id, task_id, workflow_run_id, reason, required_approver, status, resolution, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		req.ID().String(),
		req.TaskID().String(),
		req.WorkflowRunID().String(),
		req.Reason(),
		approverJSON,
		string(req.Status()),
		resolutionJSON,
		nullableTimestamp(req.ExpiresAt()),
		req.CreatedAt().Time,
	)
	if err != nil {
		return fmt.Errorf("inserting approval request: %w", err)
	}
	return nil
}

func (r *PgApprovalRequestRepository) FindByID(ctx context.Context, id shared.ApprovalRequestID) (*approval.ApprovalRequest, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, workflow_run_id, reason, required_approver, status, resolution, expires_at, created_at
		FROM approval_requests WHERE id = $1`, id.String())
	return scanApprovalRequest(row)
}

func (r *PgApprovalRequestRepository) FindByTaskID(ctx context.Context, taskID shared.TaskID) ([]*approval.ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, task_id, workflow_run_id, reason, required_approver, status, resolution, expires_at, created_at
		FROM approval_requests WHERE task_id = $1`, taskID.String())
	if err != nil {
		return nil, fmt.Errorf("querying approval requests by task: %w", err)
	}
	defer rows.Close()

	var requests []*approval.ApprovalRequest
	for rows.Next() {
		req, err := scanApprovalRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func (r *PgApprovalRequestRepository) FindPending(ctx context.Context) ([]*approval.ApprovalRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, task_id, workflow_run_id, reason, required_approver, status, resolution, expires_at, created_at
		FROM approval_requests WHERE status = 'pending'`)
	if err != nil {
		return nil, fmt.Errorf("querying pending approval requests: %w", err)
	}
	defer rows.Close()

	var requests []*approval.ApprovalRequest
	for rows.Next() {
		req, err := scanApprovalRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func (r *PgApprovalRequestRepository) Update(ctx context.Context, req *approval.ApprovalRequest) error {
	var resolutionJSON []byte
	var err error
	if res := req.Resolution(); res != nil {
		resolutionJSON, err = json.Marshal(res)
		if err != nil {
			return fmt.Errorf("marshaling resolution: %w", err)
		}
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE approval_requests
		SET status = $1, resolution = $2
		WHERE id = $3`,
		string(req.Status()),
		resolutionJSON,
		req.ID().String(),
	)
	if err != nil {
		return fmt.Errorf("updating approval request: %w", err)
	}
	return nil
}

func scanApprovalRequest(row scannable) (*approval.ApprovalRequest, error) {
	var (
		id            string
		taskID        string
		workflowRunID string
		reason        string
		approverJSON  []byte
		status        string
		resJSON       []byte
		expiresAt     *time.Time
		createdAt     shared.Timestamp
	)

	err := row.Scan(&id, &taskID, &workflowRunID, &reason, &approverJSON, &status, &resJSON, &expiresAt, &createdAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("approval request not found")
		}
		return nil, fmt.Errorf("scanning approval request: %w", err)
	}

	var approver approval.ApproverSpec
	if err := json.Unmarshal(approverJSON, &approver); err != nil {
		return nil, fmt.Errorf("unmarshaling required approver: %w", err)
	}

	var resolution *approval.ApprovalResolution
	if len(resJSON) > 0 {
		resolution = &approval.ApprovalResolution{}
		if err := json.Unmarshal(resJSON, resolution); err != nil {
			return nil, fmt.Errorf("unmarshaling resolution: %w", err)
		}
	}

	var expAt *shared.Timestamp
	if expiresAt != nil {
		ts := shared.Timestamp{Time: *expiresAt}
		expAt = &ts
	}

	return approval.ReconstructApprovalRequest(
		shared.ApprovalRequestID(id),
		shared.TaskID(taskID),
		shared.WorkflowRunID(workflowRunID),
		reason,
		approver,
		approval.ApprovalStatus(status),
		resolution,
		expAt,
		createdAt,
	), nil
}

func nullableTimestamp(ts *shared.Timestamp) any {
	if ts == nil {
		return nil
	}
	return ts.Time
}
