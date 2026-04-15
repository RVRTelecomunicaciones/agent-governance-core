-- +goose Up
CREATE TABLE policy_decisions (
    id                    VARCHAR(26) PRIMARY KEY,
    task_id               VARCHAR(26) NOT NULL REFERENCES tasks(id),
    evaluated_action      TEXT NOT NULL,
    outcome               TEXT NOT NULL,
    constraints           JSONB NOT NULL DEFAULT '[]',
    approval_requirement  JSONB,
    rules_evaluated       JSONB NOT NULL DEFAULT '[]',
    reason                TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_policy_decisions_task ON policy_decisions(task_id);
CREATE INDEX idx_policy_decisions_outcome ON policy_decisions(outcome);

-- +goose Down
DROP TABLE IF EXISTS policy_decisions;
