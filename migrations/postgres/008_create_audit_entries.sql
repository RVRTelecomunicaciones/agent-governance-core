-- +goose Up
CREATE TABLE audit_entries (
    id              VARCHAR(26) PRIMARY KEY,
    task_id         VARCHAR(26),
    workflow_run_id VARCHAR(26),
    actor           TEXT NOT NULL,
    action          TEXT NOT NULL,
    outcome         TEXT NOT NULL,
    context         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_audit_entries_task ON audit_entries(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_audit_entries_workflow ON audit_entries(workflow_run_id) WHERE workflow_run_id IS NOT NULL;
CREATE INDEX idx_audit_entries_actor ON audit_entries(actor);
CREATE INDEX idx_audit_entries_created ON audit_entries(created_at);

-- +goose Down
DROP TABLE IF EXISTS audit_entries;
