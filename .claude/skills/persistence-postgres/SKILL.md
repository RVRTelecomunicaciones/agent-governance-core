# Skill: Persistence (PostgreSQL)

## When to use

Use this skill when implementing or modifying database schemas, migrations, repository adapters, or query logic.

## Rules

1. Repositories are defined as outbound port interfaces in `internal/ports/outbound/`.
2. PostgreSQL adapters implement those interfaces in `internal/adapters/outbound/persistence/`.
3. Domain entities must not contain SQL or database annotations.
4. Migrations live in `migrations/postgres/` and are versioned.
5. Use parameterized queries — no string concatenation for SQL.
6. Transactions must be explicit when spanning multiple operations.

## Naming

- Migration files: `NNNN_description.up.sql` / `NNNN_description.down.sql`
- Repository interfaces: `TaskRepository`, `WorkflowRunRepository`, etc.
- Adapter structs: `pgTaskRepository`, `pgWorkflowRunRepository`, etc.

## Checklist

- [ ] Repository interface lives in ports/outbound.
- [ ] Adapter lives in adapters/outbound/persistence.
- [ ] No SQL in domain layer.
- [ ] Migration has both up and down scripts.
- [ ] Queries use parameterized inputs.
- [ ] Transactions are explicit.

## References

- `docs/architecture.md`
- `docs/rules.md` §3
