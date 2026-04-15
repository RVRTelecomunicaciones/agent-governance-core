-- +goose Up
CREATE TABLE tasks (
    id              VARCHAR(26) PRIMARY KEY,
    parent_task_id  VARCHAR(26) REFERENCES tasks(id),
    type            TEXT NOT NULL,
    title           TEXT NOT NULL,
    scope           TEXT NOT NULL,
    priority        TEXT NOT NULL,
    risk_level      TEXT NOT NULL,
    status          TEXT NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_parent ON tasks(parent_task_id) WHERE parent_task_id IS NOT NULL;
CREATE INDEX idx_tasks_type ON tasks(type);

-- +goose Down
DROP TABLE IF EXISTS tasks;
