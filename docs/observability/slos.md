# Governance-core — SLI / SLO Catalogue

**Baseline:** v0.6.0 + Phase 3.5.A load baseline
**Stack context:** Local observability harness at `test/observability/`; OTel Collector → Prometheus → Grafana
**Evaluation window (SLOs):** 30-day rolling (canonical); 1-hour and 24-hour burn rates for alerting
**Alert delivery:** Not wired in v1 — alerts fire in Grafana UI only; no Slack/email/PagerDuty delivery (see Follow-ups). Validated firing via `governance-collector-down` during Bundle D.

---

## OTel overhead measurement

| Metric | OTel OFF (3.5.A) | OTel ON (3.5.B) | Delta |
|--------|-------------------|-----------------|-------|
| happy_path smoke P99 (10 VU, 60 s) | 10.93 ms | 51.83 ms | +374.2 % |

**Interpretation:** OTel overhead is 374.2 %; the single-host Docker environment amplifies OTel Collector batch and export latency under cold start — latency SLOs are loosened as follows: happy-path P99 SLO raised from 60 ms to 104 ms (2× the OTel-ON measured P99); workflow end-to-end P99 SLO raised from 200 ms to 400 ms; all other latency SLOs scaled proportionally. These targets MUST be re-baselined on production or staging infra before being treated as operational.

---

## Latency SLOs

| SLI | SLO target | Baseline (3.5.A load) | Rationale |
|-----|-----------|-----------------------|-----------|
| Happy-path workflow request P99 | **< 104 ms** | 26.8 ms (OTel-OFF) / 51.83 ms (OTel-ON smoke) | 2× OTel-ON measured P99; original 60 ms loosened due to +374% overhead on this host |
| DLQ flow request P99 | **< 30 ms** | 13.3 ms | 2× baseline; DLQ not re-measured under OTel-ON — treat as provisional |
| Breaker flow request P99 | **< 35 ms** | 14.5 ms | 2× baseline; breaker not re-measured under OTel-ON — treat as provisional |
| `governance_workflow_duration_ms` P99 (end-to-end) | **< 400 ms** | not measured directly | Raised from 200 ms; generous — workflow lifecycle includes wait states; OTel overhead applied proportionally |

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

Note: alert thresholds above reflect the original 3.5.A baseline targets. After re-baselining on staging or production infra, update the alert expressions in `governance-rules.yaml` to match the revised SLO targets in the Latency SLOs section above.

---

## Caveats

- All thresholds were derived from **single-host local measurements**; real production traffic will require tuning.
- The OTel-ON overhead of +374% on this host is **not representative of production**. The OTel Collector runs as a sidecar in the same Docker Compose network and all traffic is loopback. A dedicated collector on a separate host will yield materially lower overhead.
- Grafana in the local stack runs with anonymous admin; **not apt for shared networks, staging, or production**.
- No soak baseline yet — any SLO involving sustained behaviour (memory stability, long-tail latency drift) is provisional until soak runs are executed.
- Alert rules are active in the Grafana UI but have **no notification delivery** — no email / Slack / PagerDuty wired. See Follow-ups.

---

## Follow-ups

- Wire a notification channel (Slack / email / PagerDuty) to Grafana alerts.
- Re-baseline all latency SLOs on staging or production infra with a dedicated OTel Collector; the +374% local overhead makes local measurements unsuitable for capacity planning.
- Re-tune thresholds on first real operational traffic; the quarantine-growth rule in particular is marked experimental.
- Add soak-based SLIs once soak runs are executed (Phase 3.5.A follow-up).
- Add tracing backend (Tempo / Jaeger) in a separate track — metrics-only in 3.5.B.
- Replace anonymous Grafana admin with provisioned auth (OIDC / LDAP / password) before any non-local deployment.
- Update alert expressions in `governance-rules.yaml` to reflect the revised SLO targets once re-baselined.
