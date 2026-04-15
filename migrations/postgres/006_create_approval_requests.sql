-- +goose Up
CREATE TABLE approval_requests (
    id                VARCHAR(26) PRIMARY KEY,
    task_id           VARCHAR(26) NOT NULL REFERENCES tasks(id),
    workflow_run_id   VARCHAR(26) NOT NULL REFERENCES workflow_runs(id),
    reason            TEXT NOT NULL,
    required_approver JSONB NOT NULL,
    status            TEXT NOT NULL,
    resolution        JSONB,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_approval_requests_task ON approval_requests(task_id);
CREATE INDEX idx_approval_requests_status ON approval_requests(status);
CREATE INDEX idx_approval_requests_pending ON approval_requests(status) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS approval_requests;
