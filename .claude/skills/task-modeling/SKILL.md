# Skill: Task Modeling

## When to use

Use this skill when creating or modifying task entities, task lifecycle, or task intake logic.

## Rules

1. A Task must have: ID, Type, Goal/Title, Scope, Status.
2. A Task cannot skip to completed without an execution path.
3. Task status transitions must be explicit.
4. Task types are finite and known — no free-form types.
5. Task scope defines sensitivity classification.

## Domain entities

- `Task` — aggregate root
- `TaskID` — value object
- `TaskType` — value object (enum)
- `TaskScope` — value object
- `TaskStatus` — value object (enum)

## Checklist

- [ ] Task has all required fields per domain invariants.
- [ ] Status transitions are validated.
- [ ] Task creation produces an audit entry.
- [ ] Task is the starting point — no workflow/routing without a task.

## References

- `docs/domain-invariants.md` §Task
- `docs/rules.md` §2
