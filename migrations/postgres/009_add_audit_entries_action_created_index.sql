-- +goose Up
CREATE INDEX idx_audit_entries_action_created ON audit_entries(action, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_entries_action_created;
