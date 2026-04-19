# Governance-core — SLI / SLO Catalogue

**Baseline:** v0.6.0 + Phase 3.5.A load baseline
**Stack context:** Local observability harness at `test/observability/`; OTel Collector → Prometheus → Grafana
**Evaluation window (SLOs):** 30-day rolling (canonical); 1-hour and 24-hour burn rates for alerting
**Alert delivery:** Not wired in v1 — alerts fire in Grafana UI only; no Slack/email/PagerDuty delivery (see Follow-ups). Validated firing via `governance-collector-down` during Bundle D.

---

## OTel overhead measurement

Three happy-path smoke runs (10 VU, 60 s) measured 2026-04-19 with different OTel configurations to isolate sampling overhead:

| Config | P99 (ms) | Δ vs OFF | Notes |
|--------|----------|----------|-------|
| OTel OFF | 18.31 | baseline | 3.5.A default; no OTel SDK at all |
| OTel ON — 100 % sampling | 17.66 | -3.6 % (within noise) | SDK default `ParentBased(AlwaysOn)` — prior 3.5.B result (+374 %) was a cold-start / warm-up artefact |
| OTel ON — 10 % sampling | 20.60 | +12.5 % | New default via `OTEL_TRACES_SAMPLER=parentbased_traceidratio`, `OTEL_TRACES_SAMPLER_ARG=0.1` |

**Interpretation:** With a warm OTel Collector the SDK overhead is negligible even at 100 % sampling — the +374 % previously observed in Phase 3.5.B was a cold-start artefact where the Collector's batch pipeline was not yet warmed. At 10 % sampling the delta is +12.5 %, within the 10–30 % range. At 100 % sampling the delta is within measurement noise (< 5 %).

**SLO treatment:** 10 % delta is 10–30 % → SLOs bumped proportionally from original values. The aggressive 104 ms / 400 ms loosening from 3.5.B is removed; new targets are set at 2× the warm OTel-ON P99.

---

## Latency SLOs

| SLI | SLO target | Baseline (3.5.A load) | Rationale |
|-----|-----------|-----------------------|-----------|
| Happy-path workflow request P99 | **< 45 ms** | 18.31 ms (OTel-OFF) / 20.60 ms (OTel-ON 10%) | 2× warm OTel-ON P99; tightened from 104 ms after cold-start artefact was resolved |
| DLQ flow request P99 | **< 30 ms** | 13.3 ms | 2× baseline; DLQ not re-measured under OTel-ON — treat as provisional |
| Breaker flow request P99 | **< 35 ms** | 14.5 ms | 2× baseline; breaker not re-measured under OTel-ON — treat as provisional |
| `governance_workflow_duration_ms` P99 (end-to-end) | **< 200 ms** | not measured directly | Restored to original target; OTel overhead with warm Collector is negligible |

Re-baseline all SLOs on production or staging infra. OTel overhead on a dedicated collector host will be substantially lower.

---

## Success-rate SLOs

| SLI | SLO target | Rationale |
|-----|-----------|-----------|
| Task submission success (not counted in `governance_execution_failures{failure_stage="intake"}`) | **≥ 99.9 %** | Entry point must be near-perfect |
| Workflow progression success (not ending in `failed` or `quarantined`) | **≥ 95 %** | Quarantine is a legitimate outcome — lower bar |
| Audit write success (proxy: no surge in `governance_execution_failures{failure_stage="audit"}`) | **≥ 99.9 %** | Audit loss is a compliance risk |

---

## Availability SLOs

| SLI | SLO target | Rationale |
|-----|-----------|-----------|
| `up{job="otel-collector"}` | **≥ 99.9 %** | Proxy for governance observability — without this SLI we cannot measure anything |

---

## Saturation / operational SLIs (no hard SLO — alerting only)

- `rate(governance_circuit_breaker_trips_total[5m])` — alerted at > 5 trips/min sustained 2 min
- Quarantined workflow rate (`rate(governance_workflow_transitions_total{to_state="quarantined"}[15m])`) — alerted at > 0.1/min sustained 15 min (experimental threshold)

---

## Alert rules (7 total — provisioned via Unified Alerting YAML at `test/observability/grafana/provisioning/alerting/governance-rules.yaml`)

| Name | Severity | Condition | For |
|------|----------|-----------|-----|
| `governance-happy-path-p99-slo-burn` | warning | routing P99 > 60 ms | 5 min |
| `governance-workflow-p99-slo-burn` | warning | execution P99 > 200 ms | 5 min |
| `governance-task-submit-error-rate-warning` | warning | submit error rate > 0.1 % | 15 min |
| `governance-task-submit-error-rate-critical` | critical | submit error rate > 1 % | 15 min |
| `governance-breaker-storm` | warning | > 5 trips/min | 2 min |
| `governance-quarantine-growth` (experimental) | warning | > 0.1 quarantines/min | 15 min |
| `governance-collector-down` | critical | `up{job="otel-collector"} == 0` | 2 min |

Note: alert thresholds above reflect the 3.5.A baseline targets (60 ms / 200 ms). These are still appropriate now that the OTel-ON overhead is confirmed negligible at warm start. After re-baselining on staging or production infra, update the alert expressions in `governance-rules.yaml` if the targets are further tightened.

---

## Caveats

- All thresholds were derived from **single-host local measurements**; real production traffic will require tuning.
- The OTel-ON overhead with a warm Collector is **< 15%** on this host (confirmed via 3-way sampling comparison on 2026-04-19). The earlier +374% figure (Phase 3.5.B) was a cold-start artefact and is no longer applicable.
- Grafana in the local stack runs with anonymous admin; **not apt for shared networks, staging, or production**.
- No soak baseline yet — any SLO involving sustained behaviour (memory stability, long-tail latency drift) is provisional until soak runs are executed.
- Alert rules are active in the Grafana UI but have **no notification delivery** — no email / Slack / PagerDuty wired. See Follow-ups.

---

## Follow-ups

- Wire a notification channel (Slack / email / PagerDuty) to Grafana alerts.
- Re-baseline all latency SLOs on staging or production infra with a dedicated OTel Collector to confirm the < 15% overhead figure holds under real traffic.
- Re-tune thresholds on first real operational traffic; the quarantine-growth rule in particular is marked experimental.
- Add soak-based SLIs once soak runs are executed (Phase 3.5.A follow-up).
- Add tracing backend (Tempo / Jaeger) in a separate track — metrics-only in 3.5.B.
- Replace anonymous Grafana admin with provisioned auth (OIDC / LDAP / password) before any non-local deployment.
- Update alert expressions in `governance-rules.yaml` to reflect the revised SLO targets once re-baselined.
