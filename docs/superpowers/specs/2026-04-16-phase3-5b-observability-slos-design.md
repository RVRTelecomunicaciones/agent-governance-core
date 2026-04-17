# Phase 3.5.B: Observability SLOs + Dashboards — Design Spec

**Date**: 2026-04-16
**Status**: Approved
**Scope**: Phase 3.5 — Hardening, Sub-track B (Observability SLOs + dashboards)
**Baseline**: v0.6.0 + Phase 3.5.A load baseline (`docs/observability/baseline-v0.6.0.md`)
**Stack**: OpenTelemetry Collector + Prometheus + Grafana, all in `test/observability/docker-compose.yaml`
**Purpose**: Turn the 3.5.A baseline numbers into **explicit SLIs / SLOs**, a **single Grafana dashboard** covering governance golden signals, and **embedded alert rules** — all verifiable end-to-end locally.

---

## 1. Objective

Take governance-core v0.6.0's existing OpenTelemetry instrumentation (14 instruments — see `internal/adapters/inbound/metrics/instruments.go`) and produce:

1. A **self-contained observability stack** (docker-compose) that consumes governance OTLP and surfaces it in Grafana.
2. A **single Grafana dashboard** ("Governance Overview") with 10–14 panels covering the golden signals.
3. **SLOs anchored at 2× the measured 3.5.A baseline** for latency and common availability numbers (99.9%) for success rates.
4. **Alert rules embedded in the dashboard JSON** for SLO burn / breaker storms / DLQ growth.
5. **Written SLO document** (`docs/observability/slos.md`) with each SLI's definition, SLO target, and rationale.

### Explicit non-goals

- NOT adding a Prometheus `/metrics` endpoint to the governance binary (keep OTel-only contract).
- NOT modifying production deployment topology — the observability compose is for **local validation**.
- NOT wiring AlertManager or PagerDuty — alert rules live in Grafana only for v1.
- NOT multi-environment (staging/prod) templating — single local environment.

---

## 2. Stack topology

### Components (all in a new `test/observability/docker-compose.yaml`)

```
┌──────────────────┐     OTLP/HTTP      ┌────────────────────┐
│  governance-core │  ─────────────▶    │   OTel Collector   │
│  (OTEL_ENABLED)  │                    │  contrib 0.104+    │
└──────────────────┘                    └─────────┬──────────┘
                                                   │ scrape :8889
                                                   ▼
                                        ┌────────────────────┐
                                        │    Prometheus      │
                                        │    v2.54+          │
                                        └─────────┬──────────┘
                                                   │ query
                                                   ▼
                                        ┌────────────────────┐
                                        │      Grafana       │
                                        │     v11.2+         │
                                        └────────────────────┘
```

### Containers

| Service | Image | Host port | Purpose |
|---------|-------|-----------|---------|
| `pg` | `postgres:16-alpine` | 5434 | Governance DB (separate port from load harness's 5433) |
| `governance` | `agent-governance-core:loadtest` (reused from 3.5.A) | 8082 | App under test — `OTEL_ENABLED=true` |
| `otel-collector` | `otel/opentelemetry-collector-contrib:0.104.0` | 4318 (OTLP HTTP), 8889 (prom exposer) | Receives OTLP, exposes Prometheus-format metrics |
| `prometheus` | `prom/prometheus:v2.54.1` | 9090 | Scrapes the collector, 1-day retention |
| `grafana` | `grafana/grafana:11.2.2` | 3000 | Dashboard + alerts; provisioned datasource + dashboard on startup |

### Port strategy

All ports in 3.5.B use a **different range** than 3.5.A to allow running both stacks simultaneously if needed:
- 3.5.A: pg 5433, gov 8081
- 3.5.B: pg 5434, gov 8082, otel 4318, prom 9090, grafana 3000

---

## 3. Collector pipeline

Minimal, no-smoothing: `otlp_receiver → batch_processor → prometheus_exporter`.

- Receiver: OTLP HTTP on `0.0.0.0:4318`
- Processor: `batch` (5 s interval)
- Exporter: `prometheus` on `0.0.0.0:8889` — **exposition format**, scraped by Prometheus
- Optional: `logging` exporter at `info` level for debug visibility

No tracing export in v1 — this track focuses on metrics + SLOs. Tracing backend (Tempo / Jaeger) deferred.

---

## 4. Metrics available

From `internal/adapters/inbound/metrics/instruments.go`:

| OTel name | Prom name (after exporter) | Type | Key attributes |
|-----------|----------------------------|------|----------------|
| `governance.tasks.submitted` | `governance_tasks_submitted_total` | counter | `task_type`, `scope`, `priority` |
| `governance.tasks.completed` | `governance_tasks_completed_total` | counter | `outcome` |
| `governance.routing.duration_ms` | `governance_routing_duration_ms_*` | histogram | `strategy`, `outcome` |
| `governance.policy.duration_ms` | `governance_policy_duration_ms_*` | histogram | `outcome` |
| `governance.policy.outcomes` | `governance_policy_outcomes_total` | counter | `outcome`, `action` |
| `governance.workflow.duration_ms` | `governance_workflow_duration_ms_*` | histogram | `outcome` |
| `governance.workflow.transitions` | `governance_workflow_transitions_total` | counter | `from_state`, `to_state` |
| `governance.approval.wait_ms` | `governance_approval_wait_ms_*` | histogram | `outcome` |
| `governance.execution.attempts` | `governance_execution_attempts_*` | histogram | `outcome` |
| `governance.execution.failures` | `governance_execution_failures_total` | counter | `failure_stage`, `retryable` |
| `governance.memory.duration_ms` | `governance_memory_duration_ms_*` | histogram | `outcome` |
| `governance.memory.degraded` | `governance_memory_degraded_total` | counter | `reason` |
| `governance.circuit_breaker.transitions` | `governance_circuit_breaker_transitions_total` | counter | `tool_name`, `agent_role`, `from_state`, `to_state` |
| `governance.circuit_breaker.trips` | `governance_circuit_breaker_trips_total` | counter | `tool_name`, `agent_role`, `reason` |

Histograms emit `_bucket`, `_sum`, `_count` — enough to compute `histogram_quantile()` at P50 / P95 / P99.

---

## 5. SLO catalogue

All SLOs anchored at 2× the 3.5.A load baseline for latency; standard 99.9% for success rates. Evaluation window: **30-day rolling** (canonical), with **1-hour and 24-hour burn rates** for alerting.

### Latency SLOs

| SLI | SLO target | Baseline (3.5.A load) | Rationale |
|-----|-----------|-----------------------|-----------|
| Happy-path workflow request P99 (any of submit / route / evaluate-policy / start-workflow / register-attempt) | **< 60 ms** | 26.8 ms | 2× baseline headroom |
| DLQ flow request P99 | **< 30 ms** | 13.3 ms | 2× baseline |
| Breaker flow request P99 | **< 35 ms** | 14.5 ms | 2× baseline |
| `governance_workflow_duration_ms` P99 (end-to-end workflow lifecycle) | **< 200 ms** | not measured — anchor anyway | End-to-end is dominated by wait states; generous target |

### Success-rate SLOs

| SLI | SLO target | Rationale |
|-----|-----------|-----------|
| Task submission success | **≥ 99.9 %** | Must be near-perfect; submission is the entry point |
| Workflow progression success (workflow NOT ending in `failed` or `quarantined`) | **≥ 95 %** | Lower — quarantining is a legitimate outcome |
| Audit write success (proxy: no surge in `governance.execution.failures{failure_stage="audit"}`) | **≥ 99.9 %** | Audit loss is compliance risk |

### Availability SLOs

| SLI | SLO target | Rationale |
|-----|-----------|-----------|
| `up{job="otel-collector"}` (proxy for governance reachability via OTel heartbeat) | **≥ 99.9 %** | Collector absence = we can't measure, which is itself a failure |

### Saturation / operational SLIs (no hard SLO — informational)

- `governance_circuit_breaker_trips_total` rate — alert only
- Quarantined workflow count (derived from `governance_workflow_transitions_total{to_state="quarantined"}`) — alert only

---

## 6. Dashboard: "Governance Overview"

Single dashboard, 5 rows, 10–14 panels. Target: fits on a 1440-px screen without scrolling per row.

### Row 1 — Intake (4 panels, stat/graph)

1. **Tasks submitted rate** — `rate(governance_tasks_submitted_total[1m])` — stacked by `task_type`
2. **Tasks completed rate** — `rate(governance_tasks_completed_total[1m])` — stacked by `outcome`
3. **Submission success % (30-day)** — derived from tasks_submitted vs tasks_failed
4. **Total tasks today** — sum counter panel

### Row 2 — Routing & Policy (3 panels)

5. **Routing P50/P95/P99** — `histogram_quantile(0.50|0.95|0.99, rate(governance_routing_duration_ms_bucket[5m]))`
6. **Policy outcomes rate** — `rate(governance_policy_outcomes_total[1m])` stacked by `outcome`
7. **Policy denial % (5m)** — ratio panel

### Row 3 — Workflow & Execution (3 panels)

8. **Workflow state distribution** — `sum by (to_state) (rate(governance_workflow_transitions_total[5m]))` — stacked
9. **Execution attempts P50/P95/P99** — histogram quantile
10. **Execution failures by stage** — `rate(governance_execution_failures_total[1m])` grouped by `failure_stage`

### Row 4 — Resilience (2 panels)

11. **Circuit breaker state map** — table panel of current `governance_circuit_breaker_transitions_total{to_state="open"}` per `(tool_name, agent_role)`
12. **Breaker trips rate** — `rate(governance_circuit_breaker_trips_total[5m])` by `reason`

### Row 5 — Observability health (2 panels)

13. **Collector `up` + scrape latency** — `up{job="otel-collector"}`
14. **Governance OTel heartbeat** — time since last metric update

### Variables

- `$interval` — auto range
- `$task_type` — all / bugfix / other (populated from label values)
- `$tool_name` — all / shell / other

---

## 7. Embedded alert rules

Alerts are **defined in the dashboard JSON** (Grafana unified alerting). Each panel with a rule shows it inline. On SLO burn, alert goes to the Grafana notification channel `none-configured` (documented: wire up per-env later). Alerts are functional — they fire — even without a notification sink.

### Alert list (7 rules)

| Alert | Condition | Severity | Rationale |
|-------|-----------|----------|-----------|
| `governance-happy-path-p99-slo-burn` | happy-path P99 > 60 ms for > 5 min | warning | Fast burn on latency SLO |
| `governance-workflow-p99-slo-burn` | workflow_duration_ms P99 > 200 ms for > 5 min | warning | End-to-end latency SLO |
| `governance-task-submit-error-rate-warning` | submit error rate > 0.1 % over 15 min | warning | Early signal — entry-point degradation |
| `governance-task-submit-error-rate-critical` | submit error rate > 1 % over 15 min | critical | Hard entry-point failure |
| `governance-breaker-storm` | `rate(governance_circuit_breaker_trips_total[5m]) > 5` sustained 2 min | warning | Multiple tool/role pairs tripping |
| `governance-quarantine-growth` (experimental) | `rate(governance_workflow_transitions_total{to_state="quarantined"}[15m]) > 0.1` | warning | Quarantine rate climbing — threshold tuned from real traffic in follow-up |
| `governance-collector-down` | `up{job="otel-collector"} == 0` for > 2 min | critical | Can't observe governance |

Thresholds are anchored to the baseline + SLOs. **Tuneable**: when real traffic patterns emerge, these will be revisited. The `quarantine-growth` rule is marked **experimental** — the threshold is a guess until we see real operational traffic; adjust on first false positive.

---

## 8. Provisioning

Grafana is **fully provisioned on startup** — no manual login + configure.

- Datasource: Prometheus at `http://prometheus:9090` (provisioned via `grafana/provisioning/datasources/prometheus.yaml`)
- Dashboard: single JSON at `grafana/dashboards/governance-overview.json`, loaded via `grafana/provisioning/dashboards/dashboards.yaml`
- No auth on Grafana for local stack: `GF_AUTH_ANONYMOUS_ENABLED=true`, `GF_AUTH_ANONYMOUS_ORG_ROLE=Admin`. **Explicitly NOT apt for shared networks, staging, or production.** The README and the docker-compose.yaml both carry a comment documenting this. Any non-local deployment must replace with proper auth (OIDC, LDAP, or provisioned admin password) as a blocking prerequisite.

Prometheus is **provisioned** via `prometheus/prometheus.yml` with one scrape job: `otel-collector:8889`, 15 s interval, 1-day retention (`--storage.tsdb.retention.time=1d`).

OTel Collector is **provisioned** via `otel-collector/config.yaml`.

---

## 9. Validation flow

The stack is "working" when:

1. `docker compose -f test/observability/docker-compose.yaml up -d` → all services healthy within 60 s.
2. Governance container logs show `"server starting"` and no OTel setup errors.
3. Prometheus at `localhost:9090` shows `up{job="otel-collector"} == 1`.
4. Grafana at `localhost:3000` loads "Governance Overview" dashboard automatically.
5. Running `test/load/runner.sh happy_path smoke` **against port 8082** (observability stack's governance) produces visible data in the dashboard within 1 min.
6. All 6 alert rules are present and show a "state" (most will be `Normal` or `Pending`) in Grafana's alert rule view.

The load runner is reused — the happy path / dlq / breaker k6 scripts work unchanged. Only `BASE` URL changes to `http://localhost:8082` when driving the observability stack. Keep this as a runner variant (`runner-observability.sh`) or a `BASE_URL` env override.

---

## 10. Deliverables & close criteria

### Artifacts

```
test/observability/
├── docker-compose.yaml
├── otel-collector/
│   └── config.yaml
├── prometheus/
│   └── prometheus.yml
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yaml
│   │   └── dashboards/
│   │       └── dashboards.yaml
│   └── dashboards/
│       └── governance-overview.json
└── README.md

docs/observability/
└── slos.md
```

Plus: extend the load runner to target port 8082 when running against the observability stack (env override or sibling script).

### Closes when

- Stack comes up clean via `docker compose up -d`
- Running happy_path smoke against the observability stack's governance populates the dashboard with data (screenshot in findings or reviewer signs off)
- `docs/observability/slos.md` published with each SLO's definition + rationale + target
- Alert rules visible in Grafana's alert rule panel — all 6 present, at least 1 firing demonstrably (e.g. the collector-down alert when collector is stopped manually)
- README.md documents: how to bring up the stack, how to drive traffic into it, where to view the dashboard, how to tear down

---

## 11. Out of scope (explicit)

- AlertManager / PagerDuty / Slack integration — alerts fire into the void for v1
- Tracing backend (Tempo / Jaeger) — metrics only in 3.5.B
- Log aggregation (Loki / ELK) — governance structured logs go to stdout
- Multi-environment dashboard (staging/prod variables) — single local
- Production topology recommendations — the observability compose is local-only, not a deployment template
- Long-term storage / federation — Prometheus 1-day retention is enough for validation
- RBAC / auth — anonymous admin for local
- Custom SLO burn-rate math (multi-window, multi-burn) — simple threshold alerts in v1

---

## 12. Decision log

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Separate `test/observability/docker-compose.yaml` | Keep load harness clean; stack is independent, reusable |
| D2 | SLOs anchored at 2× 3.5.A baseline | Realistic headroom; tuneable from real traffic later |
| D3 | Alerts embedded in dashboard JSON (Grafana unified alerting) | Simple, portable; upgradeable to AlertManager later |
| D4 | Single "Governance Overview" dashboard | One pane of glass; split later if it grows |
| D5 | OTel Collector between governance and Prometheus | No Prometheus exporter in Go code; preserves OTel-only contract |
| D6 | Prometheus 1-day retention | Validation workload; long-term storage is a prod decision |
| D7 | Grafana anonymous admin | Local-only; documented explicitly as unsafe for shared networks |
| D8 | No tracing backend in 3.5.B | Scope limit; tracing is a separate track |
| D9 | Reuse `agent-governance-core:loadtest` Docker image from 3.5.A | No duplicate Dockerfile; one binary, two compose files |
| D10 | Port range shifted (5434 / 8082 vs 5433 / 8081) | Can run both stacks simultaneously during debugging |
| D11 | Submit error-rate alert split into warning (> 0.1 %) + critical (> 1 %) | Two-tier escalation: warning surfaces early degradation, critical marks hard failure |
| D12 | Quarantine-growth alert marked **experimental** | Threshold is a guess without real ops traffic; tune on first false positive |
| D13 | OTel overhead re-measurement is a mandatory plan step | Baseline was measured OTel-off; SLOs anchored to it must be validated against OTel-on numbers before being declared final |

---

## 13. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| OTel Collector config drift between SDK versions | Pin collector image to `:0.104.0`; note in README. Validate on stack up. |
| Alert thresholds are too tight / too loose | Documented as tuneable. First-run tuning is part of closing the track. Tune only if runs show obvious false positives / negatives. |
| Grafana provisioning path subtleties (dashboards UID, datasource UID) | Use static UIDs in JSON; don't rely on Grafana auto-gen. |
| Metric name mismatch (OTel → Prometheus naming convention) | Confirmed via collector docs: dots become underscores, histograms add `_bucket / _sum / _count`. Verify during validation (Step 5 of section 9). |
| Baseline was measured with OTel DISABLED; enabling OTel may add latency | **Critical caveat** — re-measure one happy-path smoke WITH `OTEL_ENABLED=true` to record the OTel overhead. Document the delta in `slos.md`. If overhead is large (> 10 % on P99), consider tightening collector batch interval or sampling. |
| Grafana embedded alerts don't work if notifier not configured | Accept for v1 — alert rules fire and show state in the Grafana UI (visible via `/alerting/list`) but have no delivery channel. This is an **explicit follow-up**: a separate track wires a notifier (Slack / email / PagerDuty) per the deployment environment. Document this in `slos.md` so no one assumes alerts reach humans today. |
| OTel overhead on latency SLOs anchored to a baseline measured with OTel DISABLED | **Mandatory plan step**: re-run one happy-path smoke with `OTEL_ENABLED=true` pointing at the observability stack and record the delta in `slos.md`. If P99 rises > 10 % vs the 3.5.A baseline, revisit the SLO thresholds **before** declaring them final. This is not optional. |

---

## 14. Ready-to-plan checklist

- [x] Stack topology defined (5 containers, ports, image versions)
- [x] Collector pipeline defined
- [x] All 14 existing metrics inventoried and mapped to Prometheus names
- [x] SLO catalogue written with explicit thresholds
- [x] Dashboard panel list concrete (10–14 panels)
- [x] Alert rule list concrete (6 rules)
- [x] Provisioning strategy defined
- [x] Validation flow defined
- [x] Deliverables list concrete
- [x] Close criteria explicit
- [x] Out of scope explicit
- [x] Baseline OTel-overhead risk called out

Spec ready for plan generation.
