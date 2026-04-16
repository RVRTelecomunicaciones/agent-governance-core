# Phase 3 Track 2.1: DLQ / Quarantine — Design Spec

**Date**: 2026-04-16
**Status**: Approved
**Scope**: Phase 3 Track 2.1
**Baseline**: v0.4.0 (Phase 1 + Track 1 OTel + Track 2 Adaptive + Track 1 Scalability)
**Stack**: Go 1.26.2, PostgreSQL 16+, pgx v5

---

## 1. Objective

Add a `quarantined` terminal state to the workflow state machine so that workflows that fail due to retry budget exhaustion are explicitly marked as requiring investigation, instead of blending into generic `failed` status.

### Problem

Currently, when a workflow exhausts its retry budget, it transitions to `failed` — the same state used for policy denials and approval rejections. This makes it impossible to distinguish operational failures (repeated execution failures) from governance decisions (denied by policy/approval). Operators cannot easily find workflows that need investigation.

### Solution

New terminal state `quarantined` with direct transition from `running` when retry budget is exhausted. Quarantined workflows are queryable, auditable, and visible for operational attention.

---

## 2. Quarantine Semantics

### What gets quarantined

ONLY workflows that fail due to **retry budget exhaustion**. This is a persistent operational failure — the system tried to execute and failed repeatedly.

### What does NOT get quarantined

| Terminal state | Cause | Quarantined? |
|---|---|---|
| `failed` (policy deny) | Governance decision — correct behavior | No |
| `failed` (approval denied) | Governance decision — correct behavior | No |
| `killed` | Intentional termination | No |
| `completed` | Success | No |
| `failed` (other) | Any future failure that is not budget exhaustion | No |

### Quarantined = terminal

- `quarantined` is a terminal state like `completed`, `failed`, `killed`
- No transitions out of `quarantined`
- Kill from quarantined is not allowed (already terminal)
- Re-route from quarantine is deferred to a future version

---

## 3. State Machine Change

### New state

```go
StatusQuarantined WorkflowStatus = "quarantined"
```

### Updated IsTerminal

```go
func (s WorkflowStatus) IsTerminal() bool {
    return s == StatusCompleted || s == StatusFailed || s == StatusKilled || s == StatusQuarantined
}
```

### New transition

```
running → quarantined   (budget exhausted in RegisterAttempt)
```

Added to the transition table:

```go
StatusRunning: {StatusCompleted, StatusFailed, StatusPaused, StatusKilled, StatusQuarantined},
```

### New method on WorkflowRun

```go
func (w *WorkflowRun) Quarantine(reason string, actor shared.ActorID, now shared.Timestamp) error {
    return w.TransitionTo(StatusQuarantined, reason, actor, now)
}
```

Same mechanics as `Fail()` — validates transition, appends to history, updates status.

---

## 4. RegisterAttempt Change

### Current behavior (budget exhausted)

```go
if !lease.HasRetryBudget() {
    wf.Fail("retry budget exhausted", actor, now)
}
```

### New behavior

```go
if !lease.HasRetryBudget() {
    wf.Quarantine("retry budget exhausted", actor, now)
}
```

Single line change in `internal/application/workflowrun/register_attempt.go`.

### Audit

RegisterAttempt already emits `action=attempt_registered` with the workflow status as outcome. With quarantine, the outcome becomes `"quarantined"` instead of `"failed"`.

Two audit entries are emitted on quarantine — in this exact order:

1. **`attempt_registered`** with `outcome="quarantined"` — the regular attempt recording, which now carries `quarantined` as its outcome instead of `failed`. This is the last attempt entry. It carries the full enriched AuditContext (failure telemetry, lease state, strategy, agent_role).

2. **`workflow_quarantined`** with `outcome="budget_exhausted"` — a dedicated complementary entry emitted **once** per effective transition to quarantined. This is the signal for operators/dashboards that a workflow entered quarantine.

```go
// 1. Already emitted by the existing audit call with outcome = string(wf.Status) = "quarantined"

// 2. Dedicated quarantine entry — emitted once on the effective transition
if wf.Status == workflow.StatusQuarantined {
    _ = s.audit.Record(ctx, actor, "workflow_quarantined", "budget_exhausted", auditCtx, &taskID, &wfID)
}
```

These are two distinct entries, not duplicates. `attempt_registered` is the last attempt record. `workflow_quarantined` is the lifecycle event.

### Notification

`OnWorkflowTerminated` is already called when workflow reaches a terminal state. Quarantined is terminal → the callback fires. No changes to notifier interface.

**Reason format — normalized:** `"quarantined:budget_exhausted"`

The `reason` string follows the pattern `{terminal_state}:{cause}` with colon separator, no spaces. This is the same format used throughout: the notifier receives a single string that is machine-parseable (`strings.SplitN(reason, ":", 2)`) and human-readable.

### Lifecycle metrics

The existing lifecycle metric emission in RegisterAttempt already checks `wf.Status.IsTerminal()` and emits `governance.tasks.completed` and `governance.workflow.duration_ms`. With quarantined as terminal, these metrics emit automatically with `final_status=quarantined`. No changes needed.

---

## 5. Query: ListWorkflows

### New inbound port method

Add to `QueryService`:

```go
ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]*workflow.WorkflowRun, int, error)
```

### WorkflowFilter

```go
type WorkflowFilter struct {
    Status *workflow.WorkflowStatus
    TaskID *shared.TaskID
    Limit  int
    Offset int
}
```

Designed to be extensible — `Status` is the primary use case now, but the shape supports future filters (TaskID, date ranges, etc.) without breaking changes.

### New outbound port method

Add to `WorkflowRunRepository`:

```go
List(ctx context.Context, filter inbound.WorkflowFilter) ([]*workflow.WorkflowRun, int, error)
```

Note: `WorkflowFilter` lives in `ports/inbound` since it's part of the query API. The repository imports it — this is acceptable for a filter/DTO struct.

**Definitive location:** `WorkflowListFilter` lives in `ports/outbound/repositories.go`, following the same pattern as `AuditFilter`. The inbound `QueryService.ListWorkflows` imports it from outbound — same cross-import pattern already established.

```go
// In ports/outbound/repositories.go
type WorkflowListFilter struct {
    Status *workflow.WorkflowStatus
    TaskID *shared.TaskID
    Limit  int
    Offset int
}
```

### PostgreSQL implementation

```sql
SELECT id, task_id, status, routing_decision_id, policy_decision_id,
       current_step_index, transitions, created_at, updated_at,
       COUNT(*) OVER() AS total_count
FROM workflow_runs
WHERE status = $1
ORDER BY updated_at DESC
LIMIT $2 OFFSET $3
```

Uses the existing `idx_workflow_runs_status` index.

### HTTP endpoint

```
GET /api/v1/workflows?status=quarantined&limit=20&offset=0
```

Returns paginated list with total count. The handler validates the status enum at the HTTP boundary.

---

## 6. Persistence

### No schema changes

The `workflow_runs.status` column is `TEXT` — it already accepts any string. The new `"quarantined"` value persists directly. The `NewWorkflowStatus` validator is updated to accept it.

### No migration needed

The composite index on `status` already exists (`idx_workflow_runs_status`). The new value is just another string.

---

## 7. File Structure

### Modified files

| File | Change |
|---|---|
| `internal/domain/workflow/status.go` | Add `StatusQuarantined`, update `IsTerminal()`, update `NewWorkflowStatus()`, update `validTransitions` |
| `internal/domain/workflow/workflow_run.go` | Add `Quarantine()` method |
| `internal/domain/workflow/workflow_run_test.go` | Add quarantine transition tests |
| `internal/application/workflowrun/register_attempt.go` | Budget exhausted → `Quarantine` instead of `Fail`, emit `workflow_quarantined` audit |
| `internal/ports/outbound/repositories.go` | Add `WorkflowListFilter`, add `List` to `WorkflowRunRepository` |
| `internal/ports/inbound/query_service.go` | Add `ListWorkflows` to `QueryService` |
| `internal/adapters/outbound/persistence/pg_workflow_run_repo.go` | Implement `List` with status filter + COUNT(*) OVER() |
| `internal/adapters/inbound/http/router.go` | Update `GET /api/v1/workflows` to support query params |
| `internal/adapters/inbound/http/workflow_handler.go` | Add `handleListWorkflows` handler |
| `internal/adapters/inbound/sdk/facade.go` | Add `ListWorkflows` delegation |
| `internal/bootstrap/wire.go` | Update queryServiceAdapter with `ListWorkflows` |

### NOT modified

- Routing domain — untouched
- Policy domain — untouched
- Approval domain — untouched
- Execution domain — untouched (lease stays `exhausted`)
- Audit domain — untouched (new entries use existing types)
- OTel decorators — quarantined is just another outcome string
- Metrics instruments — lifecycle metrics fire automatically for terminal states
- Config — no new flags
- Migrations — no schema changes

---

## 8. Testing

### Domain unit tests

| Test | What it verifies |
|---|---|
| `running → quarantined` valid | Transition succeeds, status is quarantined |
| `quarantined` is terminal | `IsTerminal()` returns true |
| No transition from quarantined | Any `TransitionTo` from quarantined fails |
| Kill from quarantined fails | Already terminal |
| `NewWorkflowStatus("quarantined")` valid | Enum accepts the new value |

### Application tests

| Test | What it verifies |
|---|---|
| RegisterAttempt budget exhausted → quarantined | Workflow ends in quarantined (not failed) |
| RegisterAttempt budget exhausted → audit entries | Both `attempt_registered` and `workflow_quarantined` emitted |
| RegisterAttempt budget exhausted → notifier called | `OnWorkflowTerminated` fires with quarantined reason |
| RegisterAttempt success → NOT quarantined | Quarantine only on budget exhaustion |

### Integration tests

| Test | What it verifies |
|---|---|
| Full pipeline → budget exhausted → quarantined in DB | Workflow persisted with status=quarantined |
| ListWorkflows with status=quarantined | Returns only quarantined workflows |
| ListWorkflows pagination | Returns correct page + total |

---

## 9. Implementation Blocks

| Block | Depends on | Scope |
|---|---|---|
| **B1: Domain changes** | Nothing | status.go, workflow_run.go, tests |
| **B2: Application + ports** | B1 | register_attempt.go, WorkflowListFilter, QueryService, WorkflowRunRepository |
| **B3: Persistence + HTTP + wiring** | B2 | pg repo List, handler, facade, wire |
| **B4: Integration test** | B1-B3 | Quarantine flow e2e + ListWorkflows |

```
B1 (domain: quarantined state + transitions)
 │
 B2 (application: RegisterAttempt + ports)
 │
 B3 (persistence + HTTP + wiring)
 │
 B4 (integration test)
```

Sequential — each depends on the previous.

---

## 10. Baseline Invariants

- All existing tests continue to pass
- Workflows that fail for policy deny / approval denied still go to `failed` (NOT quarantined)
- Kill still goes to `killed`
- Success still goes to `completed`
- Only budget exhaustion triggers quarantine
- `go build ./...` succeeds
- Binary works identically for non-quarantine paths

---

## Appendix: Decisions Summary

| # | Decision | Rationale |
|---|---|---|
| D1 | Quarantine automática only (no re-route, no auto-escalation) | Minimum scope for v1, solves core operational problem |
| D2 | Only budget exhausted triggers quarantine | Policy deny/approval denied are correct decisions, not operational failures |
| D3 | `quarantined` as new terminal state in state machine | Explicit semantics, clean queries, consistent architecture |
| D4 | Direct transition running→quarantined | No intermediate `failed` step — simpler, no value in the extra state |
| D5 | `workflow_quarantined` audit entry complementary, emitted once | Distinct from `attempt_registered`, clear signal for investigation |
| D6 | WorkflowListFilter in outbound ports | Extensible shape (status, taskID, limit, offset), follows AuditFilter pattern |
| D7 | Policy cache permanently deferred | Policy evaluation is pure Go code without I/O — no caching benefit |
