# Skill: Architecture Guardrails

## When to use

Use this skill whenever a change may affect module boundaries, dependency direction, or the hexagonal architecture structure.

## Rules

1. Domain must not depend on infrastructure.
2. Domain must not depend on adapters.
3. Application depends only on domain and ports.
4. Ports define interfaces — they never contain implementation.
5. Adapters implement ports — they are thin wrappers.
6. No circular dependencies between bounded contexts.
7. Shared kernel (`internal/domain/shared/`) contains only value objects and types used across contexts.

## Checklist

- [ ] New code respects dependency rule (inward only).
- [ ] No infrastructure imports in domain packages.
- [ ] New ports are interfaces, not concrete types.
- [ ] Adapters do not contain business logic.
- [ ] Cross-context communication goes through ports, not direct imports.
- [ ] Shared kernel remains minimal — no use-case-specific types.

## References

- `docs/architecture.md`
- `docs/rules.md` §3
