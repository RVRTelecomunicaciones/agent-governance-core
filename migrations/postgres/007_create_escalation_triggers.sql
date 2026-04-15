-- +goose Up
CREATE TABLE escalation_triggers (
    id            VARCHAR(26) PRIMARY KEY,
    task_id       VARCHAR(26) NOT NULL REFERENCES tasks(id),
    condition     JSONB NOT NULL,
    target        TEXT NOT NULL,
    status        TEXT NOT NULL,
    triggered_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_escalation_triggers_task ON escalation_triggers(task_id);
CREATE INDEX idx_escalation_triggers_status ON escalation_triggers(status);

-- +goose Down
DROP TABLE IF EXISTS escalation_triggers;
