-- +goose Up
CREATE TABLE execution_leases (
    id                VARCHAR(26) PRIMARY KEY,
    workflow_run_id   VARCHAR(26) NOT NULL REFERENCES workflow_runs(id),
    timeout_budget_ms BIGINT NOT NULL,
    retry_budget      INTEGER NOT NULL,
    attempts_used     INTEGER NOT NULL DEFAULT 0,
    time_elapsed_ms   BIGINT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX idx_execution_leases_workflow ON execution_leases(workflow_run_id);

-- +goose Down
DROP TABLE IF EXISTS execution_leases;
