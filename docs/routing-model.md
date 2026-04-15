# Routing Model

## Purpose

Routing determines which agent role or execution mode handles a task, and which strategy to use.

## Invariants

- A routing decision must reference a task.
- A routing decision must specify the selected strategy.
- A routing decision must specify the selected agent role or execution mode.
- A routing decision must be explainable.

## Routing decision structure

- Task ID
- Available strategies evaluated
- Selected strategy (with reason)
- Selected agent role or execution mode
- Constraints (optional)
- Timestamp

## Strategies

Strategies are pluggable. Examples:

| Strategy         | Description                              |
|------------------|------------------------------------------|
| `direct`         | Single agent, direct execution           |
| `decompose`      | Break into subtasks, route individually  |
| `collaborate`    | Multiple agents coordinate               |
| `escalate`       | Route to human or senior agent           |

## Flow

```
Task → Evaluate strategies → Select best → Assign agent/mode → Audit
```
