# Skill: Escalation Modeling

## When to use

Use this skill when implementing escalation rules, escalation triggers, or escalation-to-human pathways.

## Rules

1. Escalation must be an explicit action, not an implicit side effect.
2. Escalation triggers must be defined (timeout, repeated failure, policy denial, budget exhaustion).
3. Escalation target must be specified (human, senior agent, team).
4. Escalation must produce an audit entry.
5. Escalated tasks must not continue executing without resolution.

## Domain entities

- `EscalationRule` — entity
- `EscalationTrigger` — value object
- `EscalationTarget` — value object

## Checklist

- [ ] Escalation trigger is one of the defined types.
- [ ] Escalation target is explicit.
- [ ] Escalated workflow is paused or transitioned appropriately.
- [ ] Escalation produces an audit entry.
- [ ] Resolution path exists (human response, timeout, etc.).

## References

- `docs/rules.md` §8
- `docs/workflow-model.md`
