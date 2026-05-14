-- +goose Up
-- phase_decisions stores governance decisions evaluated for SDD phase transitions
-- and sensitive capability calls coming from the sophia-orchestator.
-- This table is intentionally NOT foreign-keyed to tasks: the orchestator owns
-- change_id and phase identity; governance is the decision facade. The "real"
-- policy engine for /api/v1/tasks/...evaluate-policy lives in policy_decisions.
-- Sprint 3 will unify both surfaces; until then this is the orchestator-facing
-- store (see M-E0 design).
CREATE TABLE phase_decisions (
    id           VARCHAR(26) PRIMARY KEY,
    change_id    VARCHAR(26) NOT NULL,
    phase_type   VARCHAR(50) NOT NULL,
    capability   VARCHAR(100),
    sensitive    BOOLEAN NOT NULL DEFAULT FALSE,
    decision     VARCHAR(30) NOT NULL CHECK (decision IN ('allow','deny','require_approval')),
    agent_role   VARCHAR(50),
    strategy     VARCHAR(50),
    reason       TEXT,
    constraints  JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_phase_decisions_change ON phase_decisions(change_id);
CREATE INDEX idx_phase_decisions_decision ON phase_decisions(decision);

-- +goose Down
DROP TABLE IF EXISTS phase_decisions;
