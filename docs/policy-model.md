# Policy Model

## Purpose

Policy evaluation determines whether a governance action is allowed, constrained, requires approval, or is denied.

## Outcomes

| Outcome                | Effect                                          |
|------------------------|-------------------------------------------------|
| `allow`                | Action proceeds without restriction             |
| `allow_with_constraints`| Action proceeds with runtime constraints       |
| `require_approval`     | Action pauses until approval is granted          |
| `deny`                 | Action is terminal — does not proceed            |

## Invariants

- Policy outcomes must be explicit.
- A denied action must not proceed.
- Sensitive actions must be identifiable.
- Policy must be auditable.

## Policy decision structure

A policy decision must include:

- Referenced task ID
- Evaluated action
- Outcome
- Constraints (if `allow_with_constraints`)
- Approval requirement (if `require_approval`)
- Reason
- Timestamp

## Sensitive actions

Actions classified as sensitive require policy evaluation before execution. Classification is explicit, not inferred.

## Evaluation flow

```
Action → Classify sensitivity → Evaluate rules → Produce decision → Audit
```
