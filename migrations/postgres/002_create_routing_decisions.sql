-- +goose Up
CREATE TABLE routing_decisions (
    id                   VARCHAR(26) PRIMARY KEY,
    task_id              VARCHAR(26) NOT NULL REFERENCES tasks(id),
    evaluated_strategies JSONB NOT NULL,
    selected_strategy    TEXT NOT NULL,
    selected_agent_role  TEXT NOT NULL,
    reason               TEXT NOT NULL,
    constraints          JSONB NOT NULL DEFAULT '[]',
    created_at           TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_routing_decisions_task ON routing_decisions(task_id);

-- +goose Down
DROP TABLE IF EXISTS routing_decisions;
