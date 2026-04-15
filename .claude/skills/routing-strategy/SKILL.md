# Skill: Routing Strategy

## When to use

Use this skill when implementing or modifying routing decisions, strategy selection, or agent assignment.

## Rules

1. A routing decision must reference a Task.
2. A routing decision must specify the selected strategy.
3. A routing decision must specify the selected agent role or execution mode.
4. A routing decision must be explainable (reason field required).
5. Strategies are pluggable — new strategies can be added without modifying existing ones.

## Domain entities

- `RoutingDecision` — aggregate root
- `RoutingStrategy` — value object (enum/interface)
- `AgentRole` — value object

## Checklist

- [ ] Decision references a valid task.
- [ ] Strategy selection is deterministic given the same inputs.
- [ ] Decision carries an explanation.
- [ ] Decision produces an audit entry.
- [ ] No routing without a prior task.

## References

- `docs/domain-invariants.md` §RoutingDecision
- `docs/routing-model.md`
- `docs/rules.md` §2
