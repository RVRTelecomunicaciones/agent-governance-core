# Workflow Model

## States

```
Created → Routed → PolicyChecked → Running → Completed
                                  ↘ AwaitingApproval → Approved → Running
                                  ↘ Paused → Running
                                  ↘ Failed
                                  ↘ Killed (terminal)
```

## Terminal states

- Completed
- Failed
- Killed

## Invariants

- A workflow run must reference a task.
- A workflow cannot be both running and awaiting approval.
- Kill is terminal — no transitions out.
- Failed/completed/killed are terminal.
- Transitions must be explicit — no ad hoc state changes.
- Retries must consume retry budget.
- Timeouts must be enforceable.

## Transition rules

| From              | To                 | Condition                        |
|-------------------|--------------------|----------------------------------|
| Created           | Routed             | Routing decision exists          |
| Routed            | PolicyChecked      | Policy decision exists           |
| PolicyChecked     | Running            | Policy allows                    |
| PolicyChecked     | AwaitingApproval   | Policy requires approval         |
| PolicyChecked     | Failed             | Policy denies                    |
| AwaitingApproval  | Approved           | Approval granted                 |
| AwaitingApproval  | Failed             | Approval denied/timeout          |
| Approved          | Running            | Always                           |
| Running           | Completed          | Execution succeeds               |
| Running           | Failed             | Execution fails (budget exhausted)|
| Running           | Paused             | Explicit pause                   |
| Running           | Killed             | Kill switch                      |
| Paused            | Running            | Resume                           |
| Paused            | Killed             | Kill switch                      |
| *any non-terminal*| Killed             | Kill switch                      |
