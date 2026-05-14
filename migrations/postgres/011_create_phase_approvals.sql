-- +goose Up
-- phase_approvals tracks approval state for an orchestator (change_id, phase_id)
-- pair. The orchestator polls /governance/v1/approvals/{cid}/{pid}/status.
-- Absent rows are interpreted as auto-granted (V1 default-allow); presence of
-- a row means the gate is explicit.
CREATE TABLE phase_approvals (
    change_id    VARCHAR(26) NOT NULL,
    phase_id     VARCHAR(26) NOT NULL,
    status       VARCHAR(30) NOT NULL CHECK (status IN ('pending','granted','denied')),
    reason       TEXT,
    decided_by   VARCHAR(100),
    decided_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (change_id, phase_id)
);
CREATE INDEX idx_phase_approvals_status ON phase_approvals(status);

-- +goose Down
DROP TABLE IF EXISTS phase_approvals;
