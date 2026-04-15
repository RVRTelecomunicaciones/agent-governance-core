# Phase 2 Track 1: Observability + Telemetry — Design Spec

**Date**: 2026-04-15
**Status**: Approved
**Scope**: Phase 2 Track 1
**Baseline**: Phase 1 MVP (20 commits on main, 488 tests, zero drift)
**Stack**: Go 1.26.2, OTel SDK (go.opentelemetry.io/otel v1.43+), OTLP exporter

---

## 1. Objective

Add comprehensive observability to `agent-governance-core`:

1. **Distributed tracing** — end-to-end spans across the governance pipeline, cross-boundary to memory-engine
2. **Operational metrics** — counters and histograms for dashboards (latency, failure rates, throughput)
3. **Telemetry foundation for routing adaptativo** — structured failure data queryable by strategy/agent/tool

### Priority Order

1. Diagnosis (tracing) — investigate specific workflows, find where failures/latency occur
2. Dashboards (metrics) — operational visibility for trends and alerting
3. Routing data — failure patterns usable by Track 2 (routing adaptativo)

### Scope Boundaries

- Instruments `agent-governance-core` internally
- Propagates trace context cross-boundary to `memory-engine` via the `MemoryContextProvider` adapter
- Does NOT instrument `runtime-adapters` (not ready yet)
- Does NOT add a custom metrics storage layer (uses OTel exporters)

---

## 2. OTel Configuration

### Single Control Flag

| Variable | Default | Description |
|---|---|---|
| `OTEL_ENABLED` | `false` | Enable/disable OTel instrumentation |

All other OTel configuration uses the standard environment variables defined by the OTel specification:

- `OTEL_SERVICE_NAME` — defaults to `agent-governance-core`
- `OTEL_EXPORTER_OTLP_ENDPOINT` — OTLP collector endpoint
- `OTEL_EXPORTER_OTLP_PROTOCOL` — grpc or http/protobuf
- `OTEL_RESOURCE_ATTRIBUTES` — additional resource attributes
- Any other standard OTel env vars

### Behavior

- `OTEL_ENABLED=false` — no-op TracerProvider and MeterProvider. Zero functional impact. Zero performance cost. Backwards compatible with Phase 1.
- `OTEL_ENABLED=true` — initialize TracerProvider and MeterProvider with OTLP exporter configured from standard env vars. Graceful shutdown registered.

### Setup Function

```
infrastructure/observability/otel.go

SetupOTel(ctx, enabled bool, serviceName string) → (shutdown func(context.Context) error, err error)
```

If `enabled=false`, returns a no-op shutdown. If `enabled=true`, configures:
- TracerProvider with OTLP exporter + resource (service name, version)
- MeterProvider with OTLP exporter + resource
- Registers both as global providers
- Returns a shutdown function that flushes and closes both

---

## 3. Instrumentation Model: Hybrid

### UC-Level Spans — Decorators

Each inbound port gets a tracing decorator that wraps the real implementation:

| Port | Decorator | Spans created |
|---|---|---|
| GovernanceService | TracedGovernanceService | SubmitTask, ProcessTask, RouteTask, EvaluatePolicy, StartWorkflow |
| WorkflowControl | TracedWorkflowControl | KillWorkflow, PauseWorkflow, ResumeWorkflow, RegisterAttempt |
| ApprovalService | TracedApprovalService | ResolveApproval, GetPendingApprovals |
| EscalationPort | TracedEscalationPort | TriggerEscalation |
| QueryService | TracedQueryService | GetTask, GetWorkflowStatus, GetWorkflowByTask, QueryAuditTrail |

Each decorator:
1. Creates a child span with name `{Port}.{Method}` (e.g. `GovernanceService.SubmitTask`)
2. Sets relevant attributes on span start (input data)
3. Passes the new context (with span) to the wrapped implementation
4. On success: sets outcome attributes
5. On error: calls `span.RecordError(err)` and `span.SetStatus(codes.Error, ...)`
6. Defers `span.End()`

### Detail Spans — Inline

Added only in operations that can be slow or fail:

| Location | Span name | Why |
|---|---|---|
| MemoryContextProvider adapter | `MemoryContextProvider.GetRelevantContext` | Cross-boundary call, latency diagnosis |
| CallbackNotifier | `GovernanceNotifier.{method}` | Consumer callback could block/fail |
| Decompose subtask creation (RouteTask) | `RouteTask.CreateSubtasks` | Multiple DB writes, could fail |
| ExecutionLease creation (StartWorkflow) | `StartWorkflow.CreateLease` | Important lifecycle event |

Detail spans are created inline in the adapter/service code using the `ctx` that already carries the parent span from the decorator.

### Span Attributes

Standard attributes on every governance span:

| Attribute | Key | Type | Present when |
|---|---|---|---|
| Task ID | `governance.task_id` | string | Always (if available) |
| Workflow Run ID | `governance.workflow_run_id` | string | When workflow exists |
| Action name | `governance.action` | string | Always |
| Outcome | `governance.outcome` | string | On span end |

Additional per-operation attributes:

| Operation | Additional attributes |
|---|---|
| SubmitTask | `governance.task_type`, `governance.task_scope`, `governance.risk_level` |
| RouteTask | `governance.strategy`, `governance.strategy_overridden`, `governance.agent_role` |
| EvaluatePolicy | `governance.policy_outcome` |
| StartWorkflow | `governance.workflow_status` |
| RegisterAttempt | `governance.attempt_status`, `governance.failure_stage`, `governance.failure_category` |
| KillWorkflow | `governance.kill_reason` |
| MemoryContext | `governance.memory_available` |

**Rule: omit attributes that don't apply.** Do NOT emit empty string attributes. If `failure_stage` doesn't apply (success attempt), don't set it.

### Span Events (not child spans)

For instantaneous occurrences within a span:

| Event name | Where | Attributes |
|---|---|---|
| `policy.rule_evaluated` | EvaluatePolicy span | `rule_id`, `passed`, `outcome` |
| `workflow.transition` | StartWorkflow/Kill/Pause/Resume spans | `from`, `to`, `reason` |

---

## 4. Metrics

### Instruments

| Metric name | Type | Labels | Description |
|---|---|---|---|
| `governance.tasks.submitted` | Counter | `type`, `scope`, `risk_level` | Tasks ingested |
| `governance.tasks.completed` | Counter | `type`, `strategy`, `final_status` | Tasks reaching terminal state |
| `governance.routing.duration_ms` | Histogram | `strategy`, `overridden` | Routing evaluation latency |
| `governance.policy.duration_ms` | Histogram | `outcome` | Policy evaluation latency |
| `governance.policy.outcomes` | Counter | `outcome` | Distribution of policy outcomes |
| `governance.workflow.duration_ms` | Histogram | `final_status` | Total workflow lifetime |
| `governance.workflow.transitions` | Counter | `from`, `to` | Transition frequency |
| `governance.approval.wait_ms` | Histogram | `resolution` | Time approval was pending |
| `governance.execution.attempts` | Histogram | `strategy`, `agent_role` | Attempts per execution |
| `governance.execution.failures` | Counter | `failure_stage`, `failure_category`, `retryable` | Failure distribution |
| `governance.memory.duration_ms` | Histogram | `available` | Memory-engine query latency |
| `governance.memory.degraded` | Counter | — | Times memory-engine was unavailable |

### Cardinality Control

**`governance.execution.failures`** uses `failure_category` (extracted from `failure_code` as the part before `/`), NOT the full `failure_code`. Maximum label values:

- `failure_stage`: 8 (enum)
- `failure_category`: 6 (tool, runtime, external_api, memory, governance, agent)
- `retryable`: 2 (true, false)

Maximum cardinality: 8 x 6 x 2 = 96 series. Controlled.

Full `failure_code` detail lives in:
- Span attributes (traces)
- AuditContext (audit trail)

### Metric Emission Location

**General rule:**
- **Decorators** for per-call latencies, simple counters, and metrics that can be computed from a single request/response
- **Inline in use cases** for lifecycle metrics that require aggregate timestamps or accumulated state

**Decorator-emitted (per-call):**
- `governance.tasks.submitted` — in MetricsGovernanceService.SubmitTask
- `governance.routing.duration_ms` — in MetricsGovernanceService.RouteTask
- `governance.policy.duration_ms` — in MetricsGovernanceService.EvaluatePolicy
- `governance.policy.outcomes` — in MetricsGovernanceService.EvaluatePolicy
- `governance.workflow.transitions` — in MetricsWorkflowControl (Kill/Pause/Resume/RegisterAttempt)
- `governance.execution.attempts` — in MetricsWorkflowControl.RegisterAttempt
- `governance.memory.duration_ms` — inline in MemoryContextProvider adapter

**Inline-emitted (lifecycle — require aggregate timestamps):**
- `governance.tasks.completed` — emitted when a workflow reaches terminal state, in: RegisterAttempt (success/budget exhausted), KillWorkflow, ResolveApproval (denied), StartWorkflow (deny outcome). Computed from task data available in the UC.
- `governance.workflow.duration_ms` — emitted when workflow reaches terminal state, calculated as `now - workflow.CreatedAt`. Same emission points as tasks.completed.
- `governance.approval.wait_ms` — emitted in ResolveApproval, calculated as `now - approvalRequest.CreatedAt`.
- `governance.execution.failures` — emitted in RegisterAttempt (needs AttemptResult fields)
- `governance.memory.degraded` — emitted inline in MemoryContextProvider adapter

**Why inline for lifecycle metrics:** These metrics span multiple UC calls. A decorator only sees one call at a time and has no access to aggregate creation timestamps. The UC that produces the terminal/resolutive state is the only place where both the aggregate (with CreatedAt) and the current time are available.

Labels omitted (not set to empty string) when the attribute doesn't apply.

### Metrics Decorator Pattern

Separate decorator from tracing. Each inbound port gets a metrics wrapper:

| Port | Decorator |
|---|---|
| GovernanceService | MetricsGovernanceService |
| WorkflowControl | MetricsWorkflowControl |

Not all ports need metrics decorators — QueryService and ApprovalService.GetPendingApprovals are read-only and don't warrant counters in Track 1.

---

## 5. Wrapping Order

Decorators are layered around the use case implementation. The **outermost** layer handles the request first.

**Call flow:**
```
HTTP Request
  → TracedGovernanceService (creates span, passes ctx with span)
    → MetricsGovernanceService (records metrics using same ctx)
      → UseCase implementation (creates inline detail spans if any)
        → AuditRecorder (WithTraceInfo extracts trace_id from ctx)
```

**Bootstrap construction (inside-out):**
```go
var govSvc inbound.GovernanceService = &governanceServiceAdapter{...}

if cfg.OTelEnabled {
    govSvc = metrics.NewMetricsGovernanceService(govSvc, meter)   // inner
    govSvc = tracing.NewTracedGovernanceService(govSvc, tracer)   // outer
}
```

The tracing decorator is outermost so:
1. The span covers the full duration including metric recording
2. The `ctx` with span is propagated to metrics decorator and use case
3. Any error recorded in the span reflects the true outcome

---

## 6. Correlation

Three correlation axes:

| Axis | Source | Where visible |
|---|---|---|
| `trace_id` | OTel trace context in `ctx` | Traces, AuditContext |
| `task_id` | Domain — governance pipeline | Span attributes, AuditEntry, metrics labels |
| `workflow_run_id` | Domain — workflow lifecycle | Span attributes, AuditEntry |

**Already implemented in Phase 1:**
- `AuditContext.WithTraceInfo(ctx)` extracts `trace_id` and `span_id` from the OTel span in context
- `AuditRecorder.Record()` calls `WithTraceInfo(ctx)` automatically
- All use cases propagate `ctx` correctly

**Track 1 activates this:** When `OTEL_ENABLED=true` and a TracerProvider is configured, `WithTraceInfo` automatically populates `trace_id` and `span_id` in every audit entry. No code changes needed in use cases.

**Cross-boundary to memory-engine:** The `MemoryContextProvider` adapter receives `ctx` with trace context. When memory-engine SDK supports OTel (future), the `ctx` propagation already works. The inline span in the adapter creates a child span visible in the trace.

---

## 7. File Structure

### New files

```
internal/
  infrastructure/
    observability/
      otel.go                         — SetupOTel function
      otel_test.go                    — Verify setup/teardown, no-op when disabled
  adapters/
    inbound/
      tracing/
        governance_traced.go          — TracedGovernanceService
        governance_traced_test.go
        workflow_traced.go            — TracedWorkflowControl
        workflow_traced_test.go
        approval_traced.go            — TracedApprovalService
        escalation_traced.go          — TracedEscalationPort
        query_traced.go               — TracedQueryService
      metrics/
        instruments.go                — Meter creation + all instrument definitions
        governance_metrics.go         — MetricsGovernanceService
        governance_metrics_test.go
        workflow_metrics.go           — MetricsWorkflowControl
        workflow_metrics_test.go
```

### Modified files

| File | Change |
|---|---|
| `internal/infrastructure/config/config.go` | Add `OTelEnabled bool` from `OTEL_ENABLED` env var |
| `internal/bootstrap/wire.go` | OTel setup + conditional decorator wrapping |
| `cmd/agent-governance-core/main.go` | Call OTel shutdown in graceful shutdown |
| `internal/adapters/outbound/memory/stub_provider.go` | Add inline span for GetRelevantContext |
| `internal/adapters/outbound/events/callback_notifier.go` | Add inline spans for each callback |
| `go.mod` | Add OTel SDK dependencies (sdktrace, sdkmetric, otlptracehttp/grpc, etc.) |

### Minimally modified (inline lifecycle metrics only)

- `internal/application/workflowrun/register_attempt.go` — emit `tasks.completed`, `workflow.duration_ms`, `execution.failures` on terminal state
- `internal/application/workflowrun/kill_workflow.go` — emit `tasks.completed`, `workflow.duration_ms` on kill
- `internal/application/workflowrun/start_workflow.go` — emit `tasks.completed`, `workflow.duration_ms` on deny
- `internal/application/approvals/resolve_approval.go` — emit `approval.wait_ms`, and `tasks.completed` + `workflow.duration_ms` on denied

These changes are metric emission calls only — no business logic changes. The OTel MeterProvider is injected as a dependency (added to service constructors).

### NOT modified

- `internal/domain/` — domain untouched
- `internal/ports/` — port interfaces untouched (MeterProvider passed via constructor, not via port interface)

---

## 8. Testing Strategy

### Decorator Unit Tests

Each tracing/metrics decorator tested with OTel's in-memory test exporters:

- Verify span created with correct name and attributes
- Verify error recorded on span when use case returns error
- Verify span includes outcome attributes on success
- Verify metric counter incremented with correct labels
- Verify histogram records duration
- Verify attributes omitted (not empty string) when not applicable

### OTel Setup Tests

- `OTEL_ENABLED=false` → no-op provider, shutdown is no-op
- `OTEL_ENABLED=true` → provider initialized (mock endpoint or in-memory)
- Shutdown flushes without error

### Inline Span Tests

- MemoryContextProvider: span created, `governance.memory_available` attribute set
- CallbackNotifier: spans created for each callback type

### Integration Test (optional but recommended)

- Wire full pipeline with in-memory OTel exporters
- Execute ProcessTask happy path
- Verify span tree structure: ProcessTask → SubmitTask → RouteTask → EvaluatePolicy → StartWorkflow
- Verify trace_id appears in AuditContext of audit entries

### Test approach

Tests use OTel SDK's in-memory exporters to capture spans and metrics without needing a real collector. The spec does not prescribe specific test helper type names — use whatever the OTel Go SDK provides for test instrumentation (typically `sdktrace/tracetest` and `sdkmetric/metricdata`).

---

## 9. Implementation Blocks for Subagent Execution

| Block | Depends on | Scope |
|---|---|---|
| **B1: OTel setup** | Nothing | `infrastructure/observability/`, `config.go` |
| **B2: Tracing decorators** | B1 | `adapters/inbound/tracing/` (5 files + tests) |
| **B3: Metrics instruments + decorators** | B1 | `adapters/inbound/metrics/` (4 files + tests) |
| **B4: Inline spans** | B1 | Modify memory adapter + callback notifier |
| **B5: Wiring** | B1-B4 | `bootstrap/wire.go` + `main.go` |
| **B6: Integration test** | B1-B5 | OTel span tree + audit correlation test |

### Parallelization

```
B1 (OTel setup)
 ├── B2 (tracing decorators) ─── parallel
 ├── B3 (metrics decorators) ─── parallel
 └── B4 (inline spans) ───────── parallel
         │
         B5 (wiring)
         │
         B6 (integration test)
```

B2, B3, B4 can run in parallel after B1. B5 and B6 are sequential.

---

## 10. Phase 1 Baseline Invariants (must not break)

- All 488 existing tests continue to pass
- `OTEL_ENABLED=false` (default) produces identical behavior to Phase 1
- No changes to domain or application layer
- No changes to port interfaces
- Audit trail continues to work (now enriched with real trace_id when OTel active)
- `go build ./...` succeeds
- Binary starts and works without OTel collector configured

---

## Appendix: Decisions Summary

| # | Decision | Rationale |
|---|---|---|
| D1 | Diagnosis first, dashboards second, routing data third | Need to trust telemetry before using it for adaptive routing |
| D2 | Cross-boundary with memory-engine, not full distributed | runtime-adapters not ready |
| D3 | Hybrid instrumentation: decorator + inline | UC spans as decorators keep use cases clean; detail spans inline for diagnostic value |
| D4 | OTEL_ENABLED single flag, standard env vars for the rest | No config duplication, no lock-in |
| D5 | failure_category (not failure_code) in metric labels | Cardinality control: max 96 series |
| D6 | Tracing outer, metrics inner in decorator stack | Span covers full duration; ctx with span propagates correctly |
| D7 | Omit labels when not applicable | No empty string attributes in spans or metrics |
| D8 | Use cases and domain untouched | Decorator pattern preserves Phase 1 code |
| D9 | Lifecycle metrics emitted inline, not in decorators | workflow.duration_ms, approval.wait_ms, tasks.completed require aggregate timestamps only available in the UC that produces the terminal state |
