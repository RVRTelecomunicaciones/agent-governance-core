# Phase 3 Track 2.2: Circuit Breaker — Design Spec

**Date**: 2026-04-16
**Status**: Approved
**Scope**: Phase 3 Track 2.2
**Baseline**: v0.5.0 (Phase 1 + Track 1 OTel + Track 2 Adaptive + Track 1 Scalability + Track 2.1 DLQ)
**Stack**: Go 1.26.2, PostgreSQL 16+, pgx v5, sync/atomic, no new deps

---

## 1. Objective

Add a circuit breaker mechanism that **observes failure patterns** per `(tool_name, agent_role)` combination and exposes an operational state (`CLOSED`, `OPEN`, `HALF_OPEN`) to consumers and operators.

### Purpose: signal, not enforcement

The v1 circuit breaker is an **observable signal**, not a hard gate:
- It does NOT reject attempts
- It does NOT quarantine workflows
- It does NOT change routing decisions
- It DOES track state transitions and expose them via audit, metrics, tracing, HTTP, and SDK
- Consumers/orchestrators decide what to do when a breaker is OPEN (wait, change tool, change strategy, manual quarantine, etc.)

### Alignment with industry practice

This design aligns with the [OWASP Top 10 for Agentic Applications 2026 (ASI08: Cascading Failures)](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/) recommendation for circuit breakers + SLO enforcement. The 3-state model matches `sony/gobreaker`, the de-facto standard in Go.

---

## 2. Scope

### What gets a breaker

One breaker per unique `(tool_name, agent_role)` combination. Breakers are created on first observation of that combination.

### What does NOT get a breaker in v1

- Strategy-level breakers (deferred — would require coupling with routing)
- Hybrid (tool + strategy) — deferred
- Per-runtime-component breakers — deferred (runtime-adapters not yet stable)
- Per-task breakers — would be too granular

### When the breaker is skipped

If an `AttemptResult` arrives without a `tool_name` (nil), the breaker registry is NOT updated for that attempt. The breaker only tracks identifiable `(tool, agent_role)` combinations.

---

## 3. State Machine

### States

```
CLOSED     — Normal operation. All attempts tracked.
OPEN       — Trip condition met. Signal raised. No probe control.
HALF_OPEN  — Cooldown elapsed. Observing natural traffic for recovery.
```

### Trip condition: CLOSED → OPEN

The breaker opens if **either** condition is met:

1. **Consecutive failures**: `consecutive_failures >= 3`
2. **Error ratio**: `failure_rate > 0.5` over the last 20 attempts AND `sample_size >= 5`

Both conditions are evaluated after each `RegisterAttempt` update. First condition met wins.

### Recovery: OPEN → HALF_OPEN

After **60 seconds** in OPEN state (measured from `opened_at`), the next observation of that `(tool, agent_role)` combination transitions the breaker to HALF_OPEN. The transition is lazy — it happens on the next attempt, not via a timer.

### HALF_OPEN → CLOSED

In HALF_OPEN, the breaker observes natural traffic. **3 consecutive successes for the same `(tool, agent_role)`** → CLOSED. The breaker does NOT generate or control probes; it observes.

### HALF_OPEN → OPEN

In HALF_OPEN, **a single failure** → OPEN + reset cooldown (new `opened_at = now`).

### No traffic in HALF_OPEN

If no attempts arrive for that `(tool, agent_role)` during HALF_OPEN, the state persists indefinitely. This is acceptable — no traffic means no evidence to decide.

### Constants (Go code, configurable later)

```go
const (
    ConsecutiveFailuresThreshold = 3
    RatioFailureThreshold        = 0.5
    RatioWindowSize              = 20
    RatioMinSamples              = 5
    OpenCooldown                 = 60 * time.Second
    HalfOpenProbeSuccesses       = 3
)
```

---

## 4. Storage

### CircuitBreakerRegistry

A single shared component that holds breaker state for all `(tool, agent_role)` combinations:

```go
type CircuitBreakerRegistry struct {
    mu       sync.Mutex
    breakers map[BreakerKey]*CircuitBreaker
}

type BreakerKey struct {
    ToolName  string
    AgentRole string
}

type CircuitBreaker struct {
    Key                 BreakerKey
    State               BreakerState  // CLOSED, OPEN, HALF_OPEN
    OpenedAt            *time.Time    // nil unless OPEN or HALF_OPEN
    ConsecutiveFailures int
    ConsecutiveSuccesses int           // used during HALF_OPEN
    RingBuffer          []bool        // last 20 attempts: true=failure, false=success
    UpdatedAt           time.Time
}
```

### Implementation choice: map + mutex

v1 uses `sync.Mutex` around a `map[BreakerKey]*CircuitBreaker`. Not `sync.Map` — the overhead is acceptable and contention is not expected under normal load. Revisit if profiling shows bottleneck.

### Lifecycle

- **Construction**: Registry created empty at application startup
- **Population**: Breakers created on first `RegisterAttempt` observation for each combination
- **Updates**: Each `RegisterAttempt` with a non-nil `tool_name` acquires the mutex, updates the corresponding breaker, evaluates state transitions, releases the mutex
- **Startup reset**: On app restart, the registry is rebuilt from scratch. Previous state is not persisted or rehydrated. **This is a deliberate v1 choice, not a limitation.**

---

## 5. Behavior: Signal Only

### What changes in RegisterAttempt

The `RegisterAttempt` use case adds a **single call** to the breaker registry after the existing work:

```go
// After audit recording + notifier + lifecycle metrics
if result.ToolName != nil && result.AgentRole != nil {
    s.breakerRegistry.Observe(ctx, *result.ToolName, *result.AgentRole, result.Status == execution.AttemptStatusSuccess)
}
```

The call:
1. Acquires the registry mutex
2. Finds or creates the breaker for `(tool, role)`
3. Updates `ConsecutiveFailures`, `ConsecutiveSuccesses`, `RingBuffer`
4. Evaluates state transitions
5. If a transition occurs: emits audit entry, increments metric counter, sets span attributes

### What does NOT change

- `RegisterAttempt` still completes normally
- The workflow state is NOT affected by the breaker
- Attempts are NOT rejected based on breaker state
- Consumers see normal responses

The breaker is a pure side effect — its only output is observable state.

---

## 6. Observability

Five surfaces, all for v1:

### 6.1 Audit entries (on transitions only)

Every state transition emits an audit entry:

| Action | Outcome | AuditContext |
|---|---|---|
| `circuit_breaker_opened` | `consecutive` or `ratio` (trip reason) | `tool_name`, `agent_role`, `consecutive_failures`, `failure_rate`, `sample_size` |
| `circuit_breaker_half_opened` | `cooldown_elapsed` | `tool_name`, `agent_role`, `opened_duration_ms` |
| `circuit_breaker_closed` | `probes_successful` | `tool_name`, `agent_role`, `consecutive_successes` |

Also at startup:

| Action | Outcome | AuditContext |
|---|---|---|
| `circuit_breaker_registry_started` | `empty` | `component: "circuit_breaker"` |

**Classification:** This is an **operational event of the circuit breaker component** — not a business event of any workflow. It is emitted **once per application startup**. It carries NO `task_id` and NO `workflow_run_id` (both nil) because it is not tied to any domain entity. It sits in the audit trail alongside workflow events to make the deliberate in-memory state reset visible to operators, but consumers filtering audit by `task_id`/`workflow_run_id` will not see it unless they explicitly query by `action=circuit_breaker_registry_started`.

### 6.2 Metrics (OTel)

| Metric | Type | Labels | Description |
|---|---|---|---|
| `governance.circuit_breaker.transitions` | Counter | `tool_name`, `agent_role`, `from`, `to` | Count of state transitions |
| `governance.circuit_breaker.trips` | Counter | `tool_name`, `agent_role`, `trip_reason` | Count of CLOSED→OPEN transitions by cause |

No continuous gauge of current state in v1 — only counters. Cardinality note: `tool_name` is bounded by Sophia's tool set (documented assumption; no hashing/capping in v1).

### 6.3 Tracing

Breaker attributes are emitted on the `RegisterAttempt` span **only when a state transition actually occurs**. When the breaker observes an attempt without changing state, no breaker attributes are added to the span (keeps traces clean and signal-rich).

| Attribute | Type | When present |
|---|---|---|
| `governance.breaker_state_changed` | bool | Only when changed (true) |
| `governance.breaker_from_state` | string | Only when changed |
| `governance.breaker_to_state` | string | Only when changed |
| `governance.breaker_tool` | string | Only when changed |
| `governance.breaker_role` | string | Only when changed |

Rationale: omitting the boolean `false` case on every `RegisterAttempt` avoids noise in traces. The absence of breaker attributes already implies no transition occurred — that's the expected default.

### 6.4 HTTP query endpoint

```
GET /api/v1/breakers
GET /api/v1/breakers?state=open
```

Returns JSON array with current state of all breakers:

```json
{
  "items": [
    {
      "tool_name": "shell",
      "agent_role": "implementer",
      "state": "open",
      "opened_at": "2026-04-16T12:00:00Z",
      "consecutive_failures": 3,
      "failure_rate": 0.65,
      "sample_size": 20
    }
  ],
  "total": 1
}
```

**Explicit behavior:** These endpoints query **current in-memory state only**. They do NOT consult the audit trail or any persisted store. After an app restart, all breakers report `CLOSED` until traffic rebuilds their state. This is deliberate (see section 4).

The filter shape supports future expansion with `tool_name` and `agent_role` query parameters (v1 surfaces only `state`).

### 6.5 SDK programmatic access

Added to `GovernanceFacade`:

```go
func (f *GovernanceFacade) ListBreakers(ctx context.Context, filter BreakerFilter) ([]BreakerSnapshot, error)
func (f *GovernanceFacade) GetBreakerState(ctx context.Context, tool, agentRole string) (*BreakerSnapshot, error)
```

Same explicit behavior as HTTP: these query **current in-memory state only**. Not historical, not durable. Reset to empty on restart.

```go
type BreakerFilter struct {
    State     *BreakerState  // filter by state
    ToolName  *string        // reserved for future
    AgentRole *string        // reserved for future
}

type BreakerSnapshot struct {
    ToolName             string
    AgentRole            string
    State                BreakerState
    OpenedAt             *time.Time
    ConsecutiveFailures  int
    FailureRate          float64
    SampleSize           int
    UpdatedAt            time.Time
}
```

---

## 7. Startup

On application startup:

1. `CircuitBreakerRegistry` is constructed empty
2. A log line is emitted at INFO level: `"circuit breaker registry started (empty state, all breakers begin CLOSED)"`
3. An audit entry is appended (`action=circuit_breaker_registry_started`, `outcome=empty`) as part of the normal audit stream

This makes the reset deliberate and visible in both logs and audit trail.

---

## 8. File Structure

### New files

```
internal/application/resilience/
  breaker.go                    — CircuitBreaker struct, state machine methods
  breaker_test.go                — State machine unit tests
  registry.go                    — CircuitBreakerRegistry with map+mutex
  registry_test.go               — Registry tests + concurrent access tests
  observe.go                     — Observe() method: the hook called from RegisterAttempt
internal/adapters/inbound/http/
  breaker_handler.go             — handleListBreakers handler
test/integration/
  circuitbreaker/
    breaker_test.go              — End-to-end: RegisterAttempt trips breaker, audit+metrics verified
```

### Modified files

| File | Change |
|---|---|
| `internal/application/workflowrun/service.go` | Add `breakerRegistry *resilience.CircuitBreakerRegistry` dependency |
| `internal/application/workflowrun/register_attempt.go` | Call `breakerRegistry.Observe(...)` after lifecycle metrics |
| `internal/ports/inbound/query_service.go` | Add `ListBreakers`, `GetBreakerState` (if we expose via QueryService) — alternatively, a separate `ResilienceQueryService` port |
| `internal/adapters/inbound/http/router.go` | Add `GET /api/v1/breakers` route |
| `internal/adapters/inbound/sdk/facade.go` | Add `ListBreakers`, `GetBreakerState` methods |
| `internal/adapters/inbound/metrics/instruments.go` | Add `CircuitBreakerTransitions`, `CircuitBreakerTrips` counters |
| `internal/adapters/inbound/tracing/workflow_traced.go` | Add breaker attributes on RegisterAttempt span (optional — the UC itself can add them) |
| `internal/bootstrap/wire.go` | Construct registry, wire into WorkflowRunService, emit startup audit |

### Port placement decision

`ListBreakers` / `GetBreakerState` can live on:
- **Option A:** `QueryService` (existing port) — simplest, consistent with `ListWorkflows` added in T2.1
- **Option B:** New `ResilienceQueryService` port — cleaner separation of concerns

**Choice for v1: Option A.** Adds 2 methods to `QueryService`. Same pattern as `ListWorkflows`. Future refactoring is cheap if the port grows too large.

### NOT modified

- Domain layer — totally untouched
- Persistence layer — no migrations, no new tables, no repo changes
- Routing / policy / workflow aggregates — untouched
- Audit repo — untouched (uses existing Append)

---

## 9. Testing Strategy

### Unit tests — breaker state machine

| Test | What it verifies |
|---|---|
| `TestBreaker_InitialState_Closed` | New breaker starts CLOSED |
| `TestBreaker_ConsecutiveFailuresTrip` | 3 consecutive failures → OPEN |
| `TestBreaker_RatioTrip` | 11/20 failures → OPEN (ratio > 0.5 with sample >= 5) |
| `TestBreaker_NoTripBelowMinSamples` | 3/4 failures (below min_samples=5) → still CLOSED via ratio; 3 consecutive still trips via consecutive rule |
| `TestBreaker_OpenToHalfOpen_AfterCooldown` | After 60s, next observation transitions to HALF_OPEN |
| `TestBreaker_OpenToHalfOpen_NeedsObservation` | Before an attempt arrives post-cooldown, state stays OPEN |
| `TestBreaker_HalfOpenToClosed_ThreeSuccesses` | 3 consecutive successes → CLOSED |
| `TestBreaker_HalfOpenToOpen_OnFailure` | 1 failure in HALF_OPEN → OPEN, cooldown resets |
| `TestBreaker_SuccessResetsConsecutiveFailures` | CLOSED state: success clears consecutive failure counter |

### Unit tests — registry

| Test | What it verifies |
|---|---|
| `TestRegistry_CreatesBreakerOnFirstObserve` | First call creates entry |
| `TestRegistry_ReusesExistingBreaker` | Subsequent calls for same key reuse state |
| `TestRegistry_IndependentPerKey` | Different (tool, role) combinations have independent state |
| `TestRegistry_SkipsWhenToolNameEmpty` | Observe with empty tool_name → no-op |
| `TestRegistry_ConcurrentAccess` | Parallel observes on different keys don't race (uses `-race` flag) |
| `TestRegistry_List` | Returns all breakers, filterable by state |
| `TestRegistry_Get` | Returns specific breaker by key, nil if absent |

### Integration tests

| Test | What it verifies |
|---|---|
| `TestIntegration_BreakerTripsAfterConsecutiveFailures` | 3 RegisterAttempts with same tool+role+failure trips the breaker; audit entry `circuit_breaker_opened` appears |
| `TestIntegration_BreakerRecovers` | After cooldown + 3 successes, breaker closes; audit entry `circuit_breaker_closed` appears |
| `TestIntegration_ListBreakersHTTP` | GET /api/v1/breakers returns current state |
| `TestIntegration_StartupAuditEntry` | On startup, `circuit_breaker_registry_started` audit entry is present |

---

## 10. Implementation Blocks

| Block | Depends on | Scope |
|---|---|---|
| **B1: Breaker state machine (domain of resilience)** | Nothing | `breaker.go` + tests — pure logic, no deps |
| **B2: Registry + Observe** | B1 | `registry.go`, `observe.go` + tests |
| **B3: Integration with RegisterAttempt** | B2 | Modify service.go + register_attempt.go + existing tests |
| **B4: Observability — audit + metrics** | B2 | Audit events in registry transitions + new metric instruments |
| **B5: Query surface — HTTP + SDK** | B2 | QueryService additions, HTTP handler, facade methods |
| **B6: Wiring + startup** | B3+B4+B5 | wire.go updates, startup log + audit |
| **B7: Integration test** | All | End-to-end with real PG |

Sequential dependency: B1 → B2 → (B3, B4, B5 in parallel) → B6 → B7

```
B1 (breaker state machine)
 │
 B2 (registry + observe hook)
 ├── B3 (RegisterAttempt integration) ──┐
 ├── B4 (audit + metrics) ──────────────┤
 └── B5 (HTTP + SDK query) ─────────────┤
                                         │
                                        B6 (wiring + startup)
                                         │
                                        B7 (integration test)
```

---

## 11. Baseline Invariants

- All existing tests continue to pass
- `ADAPTIVE_ROUTING_ENABLED` behavior unchanged
- RegisterAttempt semantics unchanged (no new error paths, no new quarantine triggers from breaker)
- Policy deny / approval denied / kill / quarantined paths unaffected
- No database schema changes
- No new migrations
- `go build ./...` succeeds
- Binary works identically when breaker is in CLOSED state (normal flow)
- On restart, all breakers reset to CLOSED (deliberate — documented + startup audit)

---

## Appendix: Decisions Summary

| # | Decision | Rationale |
|---|---|---|
| D1 | Breaker per `(tool_name, agent_role)` | OWASP ASI08 alignment, most operational value |
| D2 | Signal only, not enforcement | governance-core doesn't execute tools; safer first iteration |
| D3 | Hybrid trip: consecutive OR ratio | Balance reactivity (consecutive) with stability (ratio) |
| D4 | 60s cooldown, 3 consecutive successes in HALF_OPEN | Standard gobreaker pattern |
| D5 | Observes natural traffic, no controlled probes | Coherent with signal-only philosophy |
| D6 | Separate CircuitBreakerRegistry, not derived from FailureStats | Real-time reaction required (0 latency); different consumer needs |
| D7 | map + mutex, not sync.Map | Sufficient for v1 load; simpler |
| D8 | Skip when tool_name absent | Breaker requires identifiable combination |
| D9 | Observability: audit + metrics + tracing + HTTP + SDK | Complete visibility for signal-based design |
| D10 | In-memory only, reset on restart (deliberate) | Coherent with FailureStats; industry standard; future (c) bootstrap from audit |
| D11 | Startup emits log + audit of empty registry | Makes reset visible in operational stream |
| D12 | Cardinality accepted under bounded-tool assumption | No cap/hash in v1; documented |
| D13 | Query surface returns current in-memory state only | Not historical, not durable — explicit |
| D14 | ListBreakers/GetBreakerState on existing QueryService | Simplest, consistent with ListWorkflows pattern |
