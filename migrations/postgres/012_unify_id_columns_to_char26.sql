-- +goose Up
-- Unify ULID-bearing columns from VARCHAR(26) to CHAR(26) so the schema
-- matches sophia-orchestator's encoding (audit amarillo #2, 2026-05-14).
--
-- Rationale:
--   - All ID values are ULIDs: exactly 26 lowercase Crockford-base32 chars.
--   - CHAR(26) is more explicit about the fixed length and matches the
--     encoding used by sophia-orchestator across all its migrations.
--   - PostgreSQL preserves bind-parameter semantics across CHAR/VARCHAR for
--     equality predicates; pgx-driven repos require no code changes.
--
-- Coverage: 24 columns across 10 tables. ALTERs are wrapped in goose's
-- implicit transaction so a partial failure rolls back the whole change.
--
-- Tables touched (in dependency order to keep FK pairs in sync):
--   tasks, routing_decisions, policy_decisions, workflow_runs,
--   execution_leases, approval_requests, escalation_triggers,
--   audit_entries, phase_decisions, phase_approvals.

ALTER TABLE tasks                ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE tasks                ALTER COLUMN parent_task_id  TYPE CHAR(26);

ALTER TABLE routing_decisions    ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE routing_decisions    ALTER COLUMN task_id         TYPE CHAR(26);

ALTER TABLE policy_decisions     ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE policy_decisions     ALTER COLUMN task_id         TYPE CHAR(26);

ALTER TABLE workflow_runs        ALTER COLUMN id                  TYPE CHAR(26);
ALTER TABLE workflow_runs        ALTER COLUMN task_id             TYPE CHAR(26);
ALTER TABLE workflow_runs        ALTER COLUMN routing_decision_id TYPE CHAR(26);
ALTER TABLE workflow_runs        ALTER COLUMN policy_decision_id  TYPE CHAR(26);

ALTER TABLE execution_leases     ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE execution_leases     ALTER COLUMN workflow_run_id TYPE CHAR(26);

ALTER TABLE approval_requests    ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE approval_requests    ALTER COLUMN task_id         TYPE CHAR(26);
ALTER TABLE approval_requests    ALTER COLUMN workflow_run_id TYPE CHAR(26);

ALTER TABLE escalation_triggers  ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE escalation_triggers  ALTER COLUMN task_id         TYPE CHAR(26);

ALTER TABLE audit_entries        ALTER COLUMN id              TYPE CHAR(26);
ALTER TABLE audit_entries        ALTER COLUMN task_id         TYPE CHAR(26);
ALTER TABLE audit_entries        ALTER COLUMN workflow_run_id TYPE CHAR(26);

ALTER TABLE phase_decisions      ALTER COLUMN id        TYPE CHAR(26);
ALTER TABLE phase_decisions      ALTER COLUMN change_id TYPE CHAR(26);

ALTER TABLE phase_approvals      ALTER COLUMN change_id TYPE CHAR(26);
ALTER TABLE phase_approvals      ALTER COLUMN phase_id  TYPE CHAR(26);

-- +goose Down
-- Reversibility: revert every column back to VARCHAR(26).
ALTER TABLE tasks                ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE tasks                ALTER COLUMN parent_task_id  TYPE VARCHAR(26);

ALTER TABLE routing_decisions    ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE routing_decisions    ALTER COLUMN task_id         TYPE VARCHAR(26);

ALTER TABLE policy_decisions     ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE policy_decisions     ALTER COLUMN task_id         TYPE VARCHAR(26);

ALTER TABLE workflow_runs        ALTER COLUMN id                  TYPE VARCHAR(26);
ALTER TABLE workflow_runs        ALTER COLUMN task_id             TYPE VARCHAR(26);
ALTER TABLE workflow_runs        ALTER COLUMN routing_decision_id TYPE VARCHAR(26);
ALTER TABLE workflow_runs        ALTER COLUMN policy_decision_id  TYPE VARCHAR(26);

ALTER TABLE execution_leases     ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE execution_leases     ALTER COLUMN workflow_run_id TYPE VARCHAR(26);

ALTER TABLE approval_requests    ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE approval_requests    ALTER COLUMN task_id         TYPE VARCHAR(26);
ALTER TABLE approval_requests    ALTER COLUMN workflow_run_id TYPE VARCHAR(26);

ALTER TABLE escalation_triggers  ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE escalation_triggers  ALTER COLUMN task_id         TYPE VARCHAR(26);

ALTER TABLE audit_entries        ALTER COLUMN id              TYPE VARCHAR(26);
ALTER TABLE audit_entries        ALTER COLUMN task_id         TYPE VARCHAR(26);
ALTER TABLE audit_entries        ALTER COLUMN workflow_run_id TYPE VARCHAR(26);

ALTER TABLE phase_decisions      ALTER COLUMN id        TYPE VARCHAR(26);
ALTER TABLE phase_decisions      ALTER COLUMN change_id TYPE VARCHAR(26);

ALTER TABLE phase_approvals      ALTER COLUMN change_id TYPE VARCHAR(26);
ALTER TABLE phase_approvals      ALTER COLUMN phase_id  TYPE VARCHAR(26);
