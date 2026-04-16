# Phase 3.5.A: Load Baseline — Design Spec

**Date**: 2026-04-16
**Status**: Approved
**Scope**: Phase 3.5 — Hardening, Sub-track A (Load baseline)
**Baseline**: v0.6.0 (MVP + OTel + Adaptive + Scalability + DLQ + Circuit Breaker)
**Stack**: k6, docker-compose, PostgreSQL 16+, Go 1.26.2
**Purpose**: DISCOVERY (measure real behaviour) — NOT VALIDATION (no SLO targets yet)

---

## 1. Objective

Measure real baseline behaviour of `agent-governance-core` v0.6.0 under sustained load, per critical flow, in a reproducible docker-compose environment. Produce numbers that feed Phase 3.5.B (SLO definition).

### Explicit non-goal

This track does **NOT** validate against targets. There are no targets yet. The output is a **measured baseline document** that makes future SLOs realistic.

---

## 2. Scope

### Flows measured (sequentially, separate runs each)

1. **Happy path** — `submit → route → evaluate_policy → start_workflow → N successful attempts → complete`
2. **DLQ flow** — `submit → route → evaluate_policy → start_workflow → retries until budget exhaustion → quarantined`
3. **Breaker flow** — `submit → route → evaluate_policy → start_workflow → consecutive failures on same (tool, role) → breaker trip observed`

### Runs per flow

Each flow runs three intensity levels in this order:

- **Smoke** — 1 minute — detect immediate crash / config error
- **Load** — 5 minutes sustained — measure steady-state throughput, latency, pool saturation
- **Soak** — 30 minutes — detect memory leaks, goroutine growth, connection leaks, cumulative degradation

### Dependencies (D4: stubs)

- `memory-engine` → stub (the same stub used in integration tests)
- `notifier` → callback stub (no network)
- PostgreSQL → real, in docker-compose
- Internal modules (audit, resilience, workflowrun, etc.) → real

Rationale: measure governance-core latency and throughput, not network to third parties. Real dependencies come back in Phase 3.5.C (chaos).

---

## 3. Environment (D2: docker-compose tuned)

### Target layout

```
test/load/
├── docker-compose.yaml          # pg + governance-core
├── pg-tuning.conf               # tuned postgresql.conf
├── scripts/
│   ├── happy_path.js            # k6 script
│   ├── dlq_flow.js
│   └── breaker_flow.js
├── runner.sh                    # entrypoint: ./runner.sh <flow> <intensity>
└── results/                     # committed baseline outputs
    └── .gitkeep
```

### docker-compose services

- `pg` — Postgres 16, tuned: shared_buffers=256MB, effective_cache_size=1GB, max_connections=100, work_mem=16MB
- `governance` — the binary built from `cmd/server` with stub memory-engine + stub notifier, OTel exporter → stdout (cheap, no collector dep)

No Grafana / Prometheus in 3.5.A — the dashboards are Phase 3.5.B. Numbers come from k6 output + pg stats + governance logs.

### Hardware baseline

- Developer Mac M-series (single host): constants explicit — cores, RAM, pg in same docker daemon.
- Write host specs into each result file (cores, RAM, OS) so numbers are comparable later.

---

## 4. Test harness (D1: per-flow, clean baseline)

### k6 scripts

Each flow is a dedicated k6 script targeting the HTTP API (`/api/v1/...`) via the facade. No SDK shortcut — measure the wire.

**Happy path** — `scripts/happy_path.js`:
- VU ramps: smoke 10 VU / load 50 VU / soak 50 VU
- Each iteration:
  1. `POST /api/v1/tasks` (submit)
  2. `POST /api/v1/tasks/{id}/route`
  3. `POST /api/v1/tasks/{id}/evaluate-policy`
  4. `POST /api/v1/workflows` (start)
  5. `POST /api/v1/workflows/{id}/attempts` with success status × 2

**DLQ flow** — `scripts/dlq_flow.js`:
- VU ramps: smoke 5 VU / load 25 VU / soak 25 VU
- Each iteration forces retries with retryable failures until the retry budget is exhausted (workflow quarantines)

**Breaker flow** — `scripts/breaker_flow.js`:
- VU ramps: smoke 5 VU / load 25 VU / soak 25 VU
- Each iteration generates 3 consecutive failures on same (tool, agent_role) to trip the breaker
- Subsequent iterations observe the tripped state

### k6 output

- Summary to stdout
- JSON to `test/load/results/<flow>-<intensity>-<timestamp>.json`
- Thresholds: **none** — this is discovery. Failing thresholds would bias the run.

---

## 5. Metrics collected

### From k6 (per flow × intensity)

- `http_reqs` — total requests
- `http_req_duration` — P50, P95, P99
- `http_req_failed` — error rate
- `iterations` — business iterations/s
- `vus_max` — concurrency reached

### From governance-core (via stdout OTel exporter + custom telemetry)

- Audit write latency (already instrumented)
- Workflow state transition counts
- Breaker transition counts (expected non-zero only in breaker flow)
- Goroutine count snapshots at start + every 5 min (soak only)
- Memory RSS snapshots at start + every 5 min (soak only)

### From PostgreSQL (via `pg_stat_*` queries)

- Active connections peak vs pool size
- Connection wait events (saturation indicator)
- Longest query in each run
- Deadlocks (expect 0; any > 0 is a finding)
- WAL generation rate
- Table + index bloat snapshot (pre/post soak only)

### Derived findings to document per flow

- Sustained throughput (iterations/s) at steady state
- Knee point: VU count where P99 starts degrading non-linearly
- First saturation cause: pg connections, goroutines, CPU, or other
- Memory/goroutine drift over soak (flat vs growing → leak signal)

---

## 6. Deliverables & close criteria

### Artifacts

- `test/load/docker-compose.yaml` + `pg-tuning.conf`
- `test/load/scripts/{happy_path,dlq_flow,breaker_flow}.js`
- `test/load/runner.sh`
- `test/load/results/` populated with 9 JSON outputs (3 flows × 3 intensities); re-runs only if numbers were suspicious
- `docs/observability/baseline-v0.6.0.md` — human-readable findings

### baseline-v0.6.0.md structure

For each flow:
1. Intensity profile (VUs, duration)
2. Throughput sustained (iterations/s)
3. Latency P50/P95/P99 per endpoint
4. First saturation cause
5. Soak observations (drift, leaks, growth)
6. Raw k6 summary reference

Ends with a **"known limits" section**: single paragraph per flow stating the current ceiling observed and where it broke.

### Closes when

- All 9 runs completed and committed
- `baseline-v0.6.0.md` written
- At least one reviewer confirms the doc is understandable without reading the k6 scripts
- Any bugs discovered during runs are filed with repro (but NOT fixed in this track — document and move on unless production-blocking)

---

## 7. Out of scope (explicit)

- SLO definition — Phase 3.5.B
- Dashboards / Grafana — Phase 3.5.B
- Alerting rules — Phase 3.5.B
- Real memory-engine / notifier integration — Phase 3.5.C
- Database failure scenarios — Phase 3.5.C
- Multi-host / distributed load — not in Phase 3.5 at all
- Optimizing any bottleneck discovered — document only; fixes are a separate decision after 3.5.A closes
- CI integration of load tests — not now; manual runs first

---

## 8. Decision log

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Per-flow runs (not mixed) | Clean baseline per flow; mixing masks which flow saturates first |
| D2 | docker-compose with tuned pg | Reproducibility; local-tal-cual invalidates cross-run comparison |
| D3 | Smoke 1m → load 5m → soak 30m | Detect crashes fast, measure steady state, catch leaks |
| D4 | Stub memory-engine + notifier | Measure governance-core itself, not network paths |
| D5 | Discovery, not validation | No SLOs yet; thresholds would bias results |
| D6 | HTTP API (not SDK) | k6 targets the wire; mirrors real client |
| D7 | No fixes in this track (general rule) | Findings → next decision; avoid scope creep |
| D8 | Exception: minimal in-place fix if a critical issue invalidates measurement or breaks the harness | Otherwise the baseline is unusable; fix only what unblocks the run, document, move on |
| D9 | 1 run per (flow × intensity); repeat only if numbers look suspicious or highly variable | Avoid overkill; 9 runs total instead of 18 |

---

## 9. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| k6 on same host as pg skews CPU numbers | Document host specs; cap k6 CPU via docker limits; note as "local-host caveat" in findings |
| Soak run reveals non-deterministic bug | File as separate issue with repro; do NOT block baseline publication |
| Baseline numbers too low to be useful | Valid outcome — feeds architectural decision in Phase 4 (e.g. "need pooling changes before adaptive loop") |
| Results vary run-to-run | 1 run per (flow × intensity) first; if numbers are suspicious or highly variable, repeat that specific run — not the whole matrix |
| Critical defect breaks the harness or invalidates a run | Minimal in-place fix allowed (D8); record fix in findings so baseline has clear caveat |

---

## 10. Ready-to-plan checklist

- [x] Flows defined (3)
- [x] Intensities defined (smoke / load / soak)
- [x] Environment defined (docker-compose + pg tuning)
- [x] Dependencies decision (stubs)
- [x] Metrics list concrete
- [x] Deliverables list concrete
- [x] Close criteria explicit
- [x] Out of scope explicit

Spec ready for plan generation.
