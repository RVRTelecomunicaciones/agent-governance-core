# Skill: Audit Trail

## When to use

Use this skill when implementing audit entries, audit persistence, or traceability for governance decisions.

## Rules

1. Audit is append-only — never update or delete.
2. Every audit entry must include: actor, action, outcome, timestamp.
3. Every governance-relevant action must produce audit data.
4. Audit entries must be traceable to a task or workflow run.
5. Audit must support querying by task, workflow, time range, and actor.

## Domain entities

- `AuditEntry` — entity (append-only)
- `AuditActor` — value object
- `AuditAction` — value object
- `AuditOutcome` — value object

## Checklist

- [ ] Entry has all four required fields (actor, action, outcome, timestamp).
- [ ] Entry references the relevant task or workflow.
- [ ] No mutation or deletion of existing entries.
- [ ] Persistence adapter enforces append-only.
- [ ] Query interface supports filtering.

## References

- `docs/domain-invariants.md` §AuditEntry
- `docs/rules.md` §7
