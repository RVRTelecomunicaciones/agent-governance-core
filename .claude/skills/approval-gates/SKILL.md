# Skill: Approval Gates

## When to use

Use this skill when implementing or modifying approval requests, approval resolution, or approval-related workflow transitions.

## Rules

1. An ApprovalRequest must reference a Task or WorkflowRun.
2. An ApprovalRequest must have: status, reason.
3. Resolved approvals must carry resolution metadata (who, when, outcome).
4. Approval resolution must be auditable.
5. Approval timeout and escalation must be possible.
6. A pending approval blocks execution — no side effects until resolved.

## Domain entities

- `ApprovalRequest` — aggregate root
- `ApprovalStatus` — value object (enum: Pending, Approved, Denied, Expired)
- `ApprovalResolution` — value object

## Checklist

- [ ] Request references a valid task or workflow run.
- [ ] Status transitions are explicit (Pending → Approved | Denied | Expired).
- [ ] Resolution includes actor, timestamp, and outcome.
- [ ] Resolution produces an audit entry.
- [ ] Timeout/escalation path exists.

## References

- `docs/domain-invariants.md` §ApprovalRequest
- `docs/rules.md` §6
