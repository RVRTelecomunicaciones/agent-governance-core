-- +goose Up
CREATE TABLE workflow_runs (
    id                   VARCHAR(26) PRIMARY KEY,
    task_id              VARCHAR(26) NOT NULL REFERENCES tasks(id),
    status               TEXT NOT NULL,
    routing_decision_id  VARCHAR(26) REFERENCES routing_decisions(id),
    policy_decision_id   VARCHAR(26) REFERENCES policy_decisions(id),
    current_step_index   INTEGER NOT NULL DEFAULT 0,
    transitions          JSONB NOT NULL DEFAULT '[]',
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_workflow_runs_task ON workflow_runs(task_id);
CREATE INDEX idx_workflow_runs_status ON workflow_runs(status);

-- +goose Down
DROP TABLE IF EXISTS workflow_runs;
