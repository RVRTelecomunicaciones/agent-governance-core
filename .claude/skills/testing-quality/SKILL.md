# Skill: Testing & Quality

## When to use

Use this skill for EVERY non-trivial change. Required by project rules.

## Rules

1. Every domain entity must have unit tests for invariants.
2. Every use case must have unit tests with mocked ports.
3. Integration tests validate adapter behavior against real dependencies (testcontainers for PostgreSQL).
4. E2E tests validate full flows through the HTTP/SDK interface.
5. Tests must be deterministic — no flaky tests allowed.
6. Table-driven tests are preferred for Go.
7. Test names follow: `Test<Function>_<scenario>_<expected>`.

## Test locations

| Type        | Location               | Dependencies          |
|-------------|------------------------|-----------------------|
| Unit        | Same package `_test.go`| None (mocks only)     |
| Integration | `test/integration/`    | Testcontainers        |
| E2E         | `test/e2e/`            | Running service       |
| Fixtures    | `test/fixtures/`       | N/A                   |

## Checklist

- [ ] Domain invariant tests exist.
- [ ] Use case tests mock outbound ports.
- [ ] Invalid inputs are tested.
- [ ] Error paths are tested.
- [ ] Tests are deterministic.
- [ ] No test depends on execution order.

## References

- `docs/rules.md` §2
- `AGENTS.md` — testing-quality is required for every non-trivial change
