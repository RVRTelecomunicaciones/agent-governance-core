# Skill: Resilience Controls

## When to use

Use this skill when implementing timeout budgets, retry budgets, kill switches, or circuit breaker logic.

## Rules

1. Every execution path must have an explicit timeout budget.
2. Every retryable path must have an explicit retry budget.
3. Attempts cannot exceed the retry budget.
4. Kill switch is terminal — no recovery.
5. Timeout expiration must trigger a defined behavior (fail, escalate, kill).
6. Circuit breakers are phase 2 but the domain model must be ready for them.

## Domain entities

- `ExecutionLease` — aggregate root
- `TimeoutBudget` — value object
- `RetryBudget` — value object
- `KillSwitch` — domain service / value object

## Checklist

- [ ] Execution lease defines both timeout and retry budgets.
- [ ] Retry attempts are tracked and compared against budget.
- [ ] Timeout is enforceable (not just advisory).
- [ ] Kill switch transitions workflow to Killed state.
- [ ] Budget exhaustion produces an explicit failure.
- [ ] All budget events produce audit entries.

## References

- `docs/domain-invariants.md` §ExecutionLease
- `docs/rules.md` §8
