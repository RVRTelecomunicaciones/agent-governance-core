# Observability harness — Phase 3.5.B

Local-only OTel Collector + Prometheus + Grafana stack for validating governance-core metrics and SLOs.

## Security

**NOT apt for shared networks, staging, or production.**
- Grafana runs with `GF_AUTH_ANONYMOUS_ENABLED=true` + `GF_AUTH_ANONYMOUS_ORG_ROLE=Admin`
- No TLS, no RBAC, all ports bound to `localhost`
- Replace with proper auth (OIDC / LDAP / provisioned admin password) before any non-local use.

## Port conflicts on this host

This host runs `ms-cotizacion-jaeger` (port 4318) and `ms-cotizacion-prometheus` (port 9090) by default. Stop them before bringing up the stack:

```bash
docker stop ms-cotizacion-jaeger ms-cotizacion-prometheus
```

Restart after teardown:

```bash
docker start ms-cotizacion-jaeger ms-cotizacion-prometheus
```

## Quickstart

```bash
docker compose -f test/observability/docker-compose.yaml up -d
# Grafana  → http://localhost:3000    (anonymous admin, dashboard "Governance Overview")
# Prom     → http://localhost:9090
# Gov API  → http://localhost:8082
docker compose -f test/observability/docker-compose.yaml down -v
```

## Drive traffic

Re-uses the k6 scripts from `test/load/scripts/` via `BASE_URL=http://localhost:8082`.

```bash
test/observability/runner.sh happy_path smoke
test/observability/runner.sh dlq_flow   load
test/observability/runner.sh breaker_flow smoke
```

The runner brings the stack up, waits for health, runs k6, captures a JSON summary in `results/`, fetches a Prometheus metric name snapshot, and tears the stack down.

## Ports

| Service | Host port | Purpose |
|---------|-----------|---------|
| pg | 5434 | Governance DB (separate from load harness's 5433) |
| governance | 8082 | HTTP API under test, `OTEL_ENABLED=true` |
| otel-collector | 4318 | OTLP HTTP receiver |
| otel-collector | 8889 | Prometheus exposition |
| prometheus | 9090 | Prometheus UI + scrape source |
| grafana | 3000 | Dashboard UI |

## Dashboard

Single dashboard `governance-overview` (UID `governance-overview`) provisioned on startup. 14 panels across 5 rows:

| Row | Panels |
|-----|--------|
| Intake | Tasks submitted rate, Tasks completed rate, Submission success %, Total tasks (today) |
| Routing & Policy | Routing P50/P95/P99, Policy outcomes rate, Policy denial % |
| Workflow & Execution | Workflow state distribution, Execution attempts P50/P95/P99, Execution failures by stage |
| Resilience | Circuit breaker open states, Breaker trips rate |
| Observability health | Collector up + scrape latency, OTel heartbeat |

## Alerts

The 7 alert rules are provisioned via Unified Alerting YAML at `grafana/provisioning/alerting/governance-rules.yaml` (these are the ones Grafana actually evaluates). The dashboard JSON's classic `.alert` fields are visual documentation only and are NOT evaluated by Grafana 11. Validated firing end-to-end during Bundle D / T8.5 by stopping the collector.

Alert rules are active in the Grafana UI but **not delivered anywhere** — no Slack / email / PagerDuty wired. See `docs/observability/slos.md` "Follow-ups" for the wire-up backlog.

| Rule | Severity | Fires when |
|------|----------|------------|
| `governance-happy-path-p99-slo-burn` | warning | routing P99 > 60 ms for 5 min |
| `governance-workflow-p99-slo-burn` | warning | execution P99 > 200 ms for 5 min |
| `governance-task-submit-error-rate-warning` | warning | submit error rate > 0.1% for 15 min |
| `governance-task-submit-error-rate-critical` | critical | submit error rate > 1% for 15 min |
| `governance-breaker-storm` | warning | > 5 breaker trips/min for 2 min |
| `governance-quarantine-growth` | warning | > 0.1 quarantines/min for 15 min (experimental) |
| `governance-collector-down` | critical | `up{job="otel-collector"} == 0` for 2 min |

## Rebuild the governance image

The stack reuses `agent-governance-core:loadtest` from the load harness. Rebuild with:

```bash
docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .
```

## Troubleshooting

- **Dashboard shows "no data":** check `curl http://localhost:9090/api/v1/query?query=governance_tasks_submitted_total` — if empty, collector isn't receiving OTel or isn't exposing to Prometheus. `docker logs obs-otel-collector` will show the reason.

- **Grafana cannot reach Prometheus:** both are on the same docker-compose network; verify with `docker exec obs-grafana wget -qO- http://prometheus:9090/-/ready`.

- **Alert rule list is empty:** ensure `test/observability/grafana/provisioning/alerting/governance-rules.yaml` is mounted. Verify with `curl -s http://localhost:3000/api/v1/provisioning/alert-rules | jq '[.[].title]'`.

- **Port already in use (4318 or 9090):** stop conflicting containers — see "Port conflicts on this host" above.

- **`agent-governance-core:loadtest` image not found:** rebuild with the command above.
