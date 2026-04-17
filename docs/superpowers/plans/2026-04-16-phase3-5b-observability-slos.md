# Phase 3.5.B Observability SLOs + Dashboards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a self-contained local observability stack (OTel Collector + Prometheus + Grafana) that consumes governance-core's existing OTLP metrics, a single "Governance Overview" dashboard with 14 panels + 7 embedded alert rules, and a published SLO document anchored at 2× the 3.5.A baseline (with OTel overhead re-measured).

**Architecture:** Standalone `test/observability/docker-compose.yaml` that reuses the `agent-governance-core:loadtest` image (from 3.5.A). Governance runs with `OTEL_ENABLED=true`; OTLP HTTP flows to an OTel Collector which re-exposes as Prometheus exposition format on port 8889; Prometheus scrapes the collector; Grafana is provisioned with a Prometheus datasource and the Governance Overview dashboard. Load is driven by the existing k6 scripts, retargeted to port 8082 via `BASE_URL` env.

**Tech Stack:** OTel Collector contrib 0.104.0, Prometheus v2.54.1, Grafana v11.2.2, existing k6 scripts, Docker Compose v2.

**Spec:** `docs/superpowers/specs/2026-04-16-phase3-5b-observability-slos-design.md`

---

## Prerequisites (local)

- Docker Desktop running
- `k6` and `jq` installed (same as 3.5.A)
- The image `agent-governance-core:loadtest` exists locally (built in Phase 3.5.A Task T1). If not: `docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .`
- Free TCP ports on host: `3000` (grafana), `4318` (otel-http), `5434` (pg), `8082` (governance), `8889` (otel-prom-exposition), `9090` (prometheus)

---

## File Structure

```
test/observability/
├── docker-compose.yaml                        # 5-service stack
├── otel-collector/
│   └── config.yaml                            # otlp→batch→prometheus pipeline
├── prometheus/
│   └── prometheus.yml                         # scrape config, 1d retention
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yaml                # auto-wires prom datasource
│   │   └── dashboards/
│   │       └── dashboards.yaml                # loader pointing at /var/lib/grafana/dashboards
│   └── dashboards/
│       └── governance-overview.json           # 14 panels + 7 alert rules
├── runner.sh                                  # BASE_URL=http://localhost:8082 wrapper
└── README.md

docs/observability/
└── slos.md                                    # SLI/SLO catalogue + OTel overhead delta

test/load/scripts/
├── happy_path.js                              # MODIFY: read BASE_URL env
├── dlq_flow.js                                # MODIFY: read BASE_URL env
└── breaker_flow.js                            # MODIFY: read BASE_URL env
```

---

## Task 1: OTel Collector config

**Files:**
- Create: `test/observability/otel-collector/config.yaml`

- [ ] **Step 1: Write the collector config**

```yaml
# OTel Collector configuration for local governance observability stack.
# Pipeline: OTLP receivers → batch → Prometheus exposition format

receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 5s
    send_batch_size: 512

exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
    resource_to_telemetry_conversion:
      enabled: true
  debug:
    verbosity: basic

service:
  telemetry:
    logs:
      level: info
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [prometheus, debug]
```

- [ ] **Step 2: Commit**

```bash
git add test/observability/otel-collector/config.yaml
git commit -m "chore(observability): add otel collector pipeline config"
```

---

## Task 2: Prometheus config

**Files:**
- Create: `test/observability/prometheus/prometheus.yml`

- [ ] **Step 1: Write the Prometheus config**

```yaml
# Prometheus scrape config for local observability stack.
# Single job: scrape the OTel Collector's prometheus exposition endpoint.

global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    environment: local
    service: agent-governance-core

scrape_configs:
  - job_name: 'otel-collector'
    static_configs:
      - targets: ['otel-collector:8889']
        labels:
          source: governance
```

- [ ] **Step 2: Commit**

```bash
git add test/observability/prometheus/prometheus.yml
git commit -m "chore(observability): add prometheus scrape config"
```

---

## Task 3: Grafana provisioning (datasource + dashboard loader)

**Files:**
- Create: `test/observability/grafana/provisioning/datasources/prometheus.yaml`
- Create: `test/observability/grafana/provisioning/dashboards/dashboards.yaml`

- [ ] **Step 1: Write the datasource provisioning file**

```yaml
# Auto-wires a Prometheus datasource on Grafana startup.

apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    uid: prom-governance
    editable: false
    jsonData:
      timeInterval: 15s
```

- [ ] **Step 2: Write the dashboard provider**

```yaml
# Loads dashboard JSONs from a mounted directory on Grafana startup.

apiVersion: 1

providers:
  - name: governance-dashboards
    orgId: 1
    folder: ''
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: false
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
```

- [ ] **Step 3: Commit**

```bash
git add test/observability/grafana/provisioning/
git commit -m "chore(observability): add grafana datasource and dashboard provisioners"
```

---

## Task 4: docker-compose.yaml

**Files:**
- Create: `test/observability/docker-compose.yaml`

- [ ] **Step 1: Write the compose file**

```yaml
# Local observability stack for governance-core.
# NOT FOR STAGING / PROD — anonymous Grafana admin, no TLS, open ports on host.

services:
  pg:
    image: postgres:16-alpine
    container_name: obs-pg
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: governance
    ports:
      - "5434:5432"
    volumes:
      - ../../migrations/postgres:/migrations:ro
      - ../load/pg/init-db.sh:/docker-entrypoint-initdb.d/init-db.sh:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d governance"]
      interval: 2s
      timeout: 2s
      retries: 20
    cpus: "2.0"
    mem_limit: 1024m

  governance:
    image: agent-governance-core:loadtest
    container_name: obs-governance
    depends_on:
      pg:
        condition: service_healthy
      otel-collector:
        condition: service_started
    environment:
      PORT: "8080"
      DB_HOST: pg
      DB_PORT: "5432"
      DB_USER: postgres
      DB_PASSWORD: postgres
      DB_NAME: governance
      DB_SSLMODE: disable
      LOG_LEVEL: info
      OTEL_ENABLED: "true"
      ADAPTIVE_ROUTING_ENABLED: "false"
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
      OTEL_SERVICE_NAME: agent-governance-core
      OTEL_METRIC_EXPORT_INTERVAL: "5000"
    ports:
      - "8082:8080"
    cpus: "2.0"
    mem_limit: 1024m

  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.104.0
    container_name: obs-otel-collector
    command: ["--config=/etc/otel-collector/config.yaml"]
    volumes:
      - ./otel-collector/config.yaml:/etc/otel-collector/config.yaml:ro
    ports:
      - "4318:4318"   # OTLP HTTP in
      - "8889:8889"   # prom exposition out
    cpus: "1.0"
    mem_limit: 512m

  prometheus:
    image: prom/prometheus:v2.54.1
    container_name: obs-prometheus
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.retention.time=1d"
      - "--web.enable-lifecycle"
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports:
      - "9090:9090"
    cpus: "1.0"
    mem_limit: 512m

  grafana:
    image: grafana/grafana:11.2.2
    container_name: obs-grafana
    depends_on:
      - prometheus
    environment:
      # LOCAL-ONLY — NOT APT FOR SHARED NETWORKS OR STAGING/PROD
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
      GF_ANALYTICS_REPORTING_ENABLED: "false"
      GF_USERS_ALLOW_SIGN_UP: "false"
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
    ports:
      - "3000:3000"
    cpus: "1.0"
    mem_limit: 512m
```

- [ ] **Step 2: Commit**

```bash
git add test/observability/docker-compose.yaml
git commit -m "chore(observability): add docker-compose with pg + governance + otel + prom + grafana"
```

---

## Task 5: Parameterize k6 scripts via BASE_URL env

**Files:**
- Modify: `test/load/scripts/happy_path.js`
- Modify: `test/load/scripts/dlq_flow.js`
- Modify: `test/load/scripts/breaker_flow.js`

The scripts currently have `const BASE = 'http://localhost:8081';` hard-coded. We add an env override so the observability runner can point them at port 8082 without duplicating scripts.

- [ ] **Step 1: Edit `test/load/scripts/happy_path.js`**

Replace the line:
```javascript
const BASE = 'http://localhost:8081';
```
with:
```javascript
const BASE = __ENV.BASE_URL || 'http://localhost:8081';
```

- [ ] **Step 2: Edit `test/load/scripts/dlq_flow.js`**

Same replacement as Step 1.

- [ ] **Step 3: Edit `test/load/scripts/breaker_flow.js`**

Same replacement as Step 1.

- [ ] **Step 4: Verify all 3 scripts still parse**

Run:
```bash
k6 inspect test/load/scripts/happy_path.js
k6 inspect test/load/scripts/dlq_flow.js
k6 inspect test/load/scripts/breaker_flow.js
```
Expected: all 3 exit 0.

- [ ] **Step 5: Sanity — confirm default behaviour unchanged for 3.5.A runner**

The existing `test/load/runner.sh` does NOT set `BASE_URL`. Run:
```bash
grep -n "BASE_URL" test/load/runner.sh || echo "BASE_URL not set → scripts default to 8081 — unchanged"
```
Expected: `BASE_URL not set` line printed. No code changes required in `test/load/runner.sh`.

- [ ] **Step 6: Commit**

```bash
git add test/load/scripts/happy_path.js test/load/scripts/dlq_flow.js test/load/scripts/breaker_flow.js
git commit -m "chore(load): parameterize k6 BASE via BASE_URL env (default preserved)"
```

---

## Task 6: Observability runner script

**Files:**
- Create: `test/observability/runner.sh`

- [ ] **Step 1: Write `test/observability/runner.sh`**

```bash
#!/usr/bin/env bash
# Usage: ./runner.sh <flow> <intensity>
#   flow:      happy_path | dlq_flow | breaker_flow
#   intensity: smoke | load | soak
#
# Brings up the observability stack (pg + governance + otel + prom + grafana),
# waits for health, retargets the existing k6 script at port 8082 via BASE_URL,
# runs k6, and tears the stack down.
#
# Intended for validating the dashboard and re-measuring latency under OTel on.

set -euo pipefail

FLOW="${1:-}"
INTENSITY="${2:-}"

if [[ -z "${FLOW}" || -z "${INTENSITY}" ]]; then
  echo "usage: $0 <happy_path|dlq_flow|breaker_flow> <smoke|load|soak>" >&2
  exit 2
fi

SCRIPT="../../test/load/scripts/${FLOW}.js"
if [[ ! -f "$(dirname "$0")/${SCRIPT}" ]]; then
  echo "no such script: ${SCRIPT} (relative to $(dirname "$0"))" >&2
  exit 2
fi

case "${INTENSITY}" in
  smoke) ;;
  load)  ;;
  soak)  ;;
  *) echo "invalid intensity: ${INTENSITY}" >&2; exit 2 ;;
esac

HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p results
OUT="results/${FLOW}-${INTENSITY}-${TS}.json"

echo ">> bringing up observability stack"
docker compose up -d

echo ">> waiting for governance health on :8082"
for i in $(seq 1 60); do
  if curl -fsS "http://localhost:8082/api/v1/audit?limit=1" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo ">> waiting for prometheus at :9090"
for i in $(seq 1 30); do
  if curl -fsS "http://localhost:9090/-/ready" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo ">> running k6 (${FLOW} / ${INTENSITY}) against :8082 with OTel enabled"
BASE_URL="http://localhost:8082" K6_INTENSITY="${INTENSITY}" \
  k6 run --summary-export="${OUT}" "${SCRIPT}"

echo ">> fetching prometheus snapshot of governance_* metric names"
curl -fsS "http://localhost:9090/api/v1/label/__name__/values" \
  | jq -r '.data[] | select(startswith("governance_"))' \
  > "results/${FLOW}-${INTENSITY}-${TS}.metrics.txt" || true

echo ">> tearing down"
docker compose down -v

echo ">> done: ${OUT}"
```

- [ ] **Step 2: Make executable**

```bash
chmod +x test/observability/runner.sh
```

- [ ] **Step 3: Sanity-check shell syntax**

```bash
bash -n test/observability/runner.sh
```
Expected: silent success.

- [ ] **Step 4: Create results placeholder**

```bash
mkdir -p test/observability/results
printf '' > test/observability/results/.gitkeep
```

- [ ] **Step 5: Commit**

```bash
git add test/observability/runner.sh test/observability/results/.gitkeep
git commit -m "chore(observability): add runner script retargeting k6 to OTel-enabled stack"
```

---

## Task 7: Bring-up smoke test (stack without dashboards yet)

**Goal:** Confirm containers all come up healthy and metrics flow end-to-end to Prometheus. Grafana will load but without the dashboard — that is Task 8.

**Files:**
- No new files. This task is validation only — output feeds Task 8 + Task 9 + Task 10.

- [ ] **Step 1: Bring up the stack**

```bash
docker compose -f test/observability/docker-compose.yaml up -d
```
Expected: all 5 services start. If `agent-governance-core:loadtest` image is missing, re-build: `docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .`

- [ ] **Step 2: Verify each service is healthy / up**

```bash
docker compose -f test/observability/docker-compose.yaml ps
```
Expected: `obs-pg` healthy; `obs-governance`, `obs-otel-collector`, `obs-prometheus`, `obs-grafana` all `running`.

- [ ] **Step 3: Verify governance boots with OTel on**

```bash
docker logs obs-governance 2>&1 | grep -E "server starting|otel"
```
Expected: `"server starting","port":8080`. No OTel setup errors.

- [ ] **Step 4: Probe governance HTTP**

```bash
curl -sS -X POST http://localhost:8082/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"type":"bugfix","title":"probe","scope":"file","priority":"normal"}'
```
Expected: HTTP 201 with a `id` field in the body (same shape as 3.5.A T3 probe).

- [ ] **Step 5: Verify OTel collector received metrics**

```bash
docker logs obs-otel-collector 2>&1 | grep -i "metrics" | head -5
```
Expected: at least one line mentioning metric batch received (from the `debug` exporter). If empty, wait 10 s and retry — the first export cycle is ~5 s.

- [ ] **Step 6: Verify Prometheus is scraping the collector**

```bash
curl -sS 'http://localhost:9090/api/v1/targets' | jq '.data.activeTargets[] | {job: .labels.job, health: .health}'
```
Expected:
```json
{"job": "otel-collector", "health": "up"}
```

- [ ] **Step 7: Verify governance metrics reached Prometheus**

```bash
curl -sS 'http://localhost:9090/api/v1/label/__name__/values' \
  | jq -r '.data[] | select(startswith("governance_"))'
```
Expected: non-empty list. At minimum, `governance_tasks_submitted_total` should appear because of the curl probe in Step 4.

- [ ] **Step 8: Verify Grafana is up and the Prometheus datasource is provisioned**

```bash
curl -sS 'http://localhost:3000/api/datasources/name/Prometheus' -H 'Accept: application/json'
```
Expected: JSON with `"uid":"prom-governance"` and `"url":"http://prometheus:9090"`.

- [ ] **Step 9: Tear down**

```bash
docker compose -f test/observability/docker-compose.yaml down -v
```
Expected: all containers removed.

- [ ] **Step 10: No commit**

This task creates no files. If any step from 1–8 failed, STOP and diagnose before continuing — the later tasks assume this pipeline works.

---

## Task 8: Governance Overview dashboard JSON

**Files:**
- Create: `test/observability/grafana/dashboards/governance-overview.json`

This is the biggest artifact. Dashboard has 14 panels across 5 rows and 7 embedded alert rules. The schema uses Grafana 11 dashboard v39 JSON.

**Panel specifications (queries, panel types, axes):**

| # | Row | Title | Panel type | PromQL | Unit |
|---|-----|-------|------------|--------|------|
| 1 | Intake | Tasks submitted rate | timeseries (stacked) | `sum by (task_type) (rate(governance_tasks_submitted_total[1m]))` | req/s |
| 2 | Intake | Tasks completed rate | timeseries (stacked) | `sum by (outcome) (rate(governance_tasks_completed_total[1m]))` | req/s |
| 3 | Intake | Submission success % (30d) | stat | `100 * sum(rate(governance_tasks_submitted_total[30d])) / (sum(rate(governance_tasks_submitted_total[30d])) + sum(rate(governance_execution_failures_total{failure_stage="intake"}[30d])))` | percent |
| 4 | Intake | Total tasks (today) | stat | `sum(increase(governance_tasks_submitted_total[24h]))` | short |
| 5 | Routing & Policy | Routing P50/P95/P99 | timeseries | 3 queries: `histogram_quantile(0.50, sum by (le) (rate(governance_routing_duration_ms_bucket[5m])))`, 0.95, 0.99 | ms |
| 6 | Routing & Policy | Policy outcomes rate | timeseries (stacked) | `sum by (outcome) (rate(governance_policy_outcomes_total[1m]))` | req/s |
| 7 | Routing & Policy | Policy denial % (5m) | stat | `100 * sum(rate(governance_policy_outcomes_total{outcome="deny"}[5m])) / sum(rate(governance_policy_outcomes_total[5m]))` | percent |
| 8 | Workflow & Execution | Workflow state distribution | timeseries (stacked) | `sum by (to_state) (rate(governance_workflow_transitions_total[5m]))` | req/s |
| 9 | Workflow & Execution | Execution attempts P50/P95/P99 | timeseries | 3 quantiles of `governance_execution_attempts_bucket` | ms |
| 10 | Workflow & Execution | Execution failures by stage | timeseries (stacked) | `sum by (failure_stage) (rate(governance_execution_failures_total[1m]))` | req/s |
| 11 | Resilience | Circuit breaker open states | table | `sum by (tool_name, agent_role) (governance_circuit_breaker_transitions_total{to_state="open"}) - sum by (tool_name, agent_role) (governance_circuit_breaker_transitions_total{to_state="closed"})` | count |
| 12 | Resilience | Breaker trips rate | timeseries (stacked) | `sum by (reason) (rate(governance_circuit_breaker_trips_total[5m]))` | trips/s |
| 13 | Observability health | Collector up + scrape latency | stat + timeseries | `up{job="otel-collector"}` and `scrape_duration_seconds{job="otel-collector"}` | bool + s |
| 14 | Observability health | Governance OTel heartbeat | stat | `time() - max(timestamp(governance_tasks_submitted_total))` | s (ago) |

**Alert rules (embedded in dashboard JSON panels):**

| # | Panel anchor | Rule name | Expression | For | Severity |
|---|--------------|-----------|------------|-----|----------|
| 1 | #5 Routing P99 | `governance-happy-path-p99-slo-burn` | `histogram_quantile(0.99, sum by (le) (rate(governance_routing_duration_ms_bucket[5m]))) > 60` | 5m | warning |
| 2 | #9 Execution P99 | `governance-workflow-p99-slo-burn` | `histogram_quantile(0.99, sum by (le) (rate(governance_workflow_duration_ms_bucket[5m]))) > 200` | 5m | warning |
| 3 | #10 Execution failures | `governance-task-submit-error-rate-warning` | `100 * sum(rate(governance_execution_failures_total{failure_stage="intake"}[15m])) / sum(rate(governance_tasks_submitted_total[15m])) > 0.1` | 15m | warning |
| 4 | #10 Execution failures | `governance-task-submit-error-rate-critical` | `100 * sum(rate(governance_execution_failures_total{failure_stage="intake"}[15m])) / sum(rate(governance_tasks_submitted_total[15m])) > 1` | 15m | critical |
| 5 | #12 Breaker trips | `governance-breaker-storm` | `sum(rate(governance_circuit_breaker_trips_total[5m])) > 5` | 2m | warning |
| 6 | #12 Breaker trips | `governance-quarantine-growth` (experimental) | `sum(rate(governance_workflow_transitions_total{to_state="quarantined"}[15m])) > 0.1` | 15m | warning |
| 7 | #13 Collector up | `governance-collector-down` | `up{job="otel-collector"} == 0` | 2m | critical |

- [ ] **Step 1: Write the dashboard JSON**

Create `test/observability/grafana/dashboards/governance-overview.json` with the schema shown below. Because the full JSON is large, one complete panel is shown as a template — replicate the same pattern for the remaining 13 panels and the 7 alert rules. Use static `uid` values so Grafana does not regenerate them across restarts.

**Minimum required top-level fields:**

```json
{
  "uid": "governance-overview",
  "title": "Governance Overview",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "time": { "from": "now-15m", "to": "now" },
  "timezone": "browser",
  "tags": ["governance", "agent-governance-core"],
  "panels": [ /* 14 panels here */ ],
  "templating": {
    "list": [
      {
        "name": "task_type",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "prom-governance" },
        "query": { "query": "label_values(governance_tasks_submitted_total, task_type)", "refId": "task_type" },
        "refresh": 1,
        "includeAll": true,
        "multi": true
      },
      {
        "name": "tool_name",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "prom-governance" },
        "query": { "query": "label_values(governance_circuit_breaker_trips_total, tool_name)", "refId": "tool_name" },
        "refresh": 1,
        "includeAll": true,
        "multi": true
      }
    ]
  },
  "annotations": { "list": [] }
}
```

**Template panel (Panel 1 — "Tasks submitted rate", timeseries, stacked):**

```json
{
  "id": 1,
  "type": "timeseries",
  "title": "Tasks submitted rate",
  "gridPos": { "h": 8, "w": 6, "x": 0, "y": 0 },
  "datasource": { "type": "prometheus", "uid": "prom-governance" },
  "targets": [
    {
      "refId": "A",
      "expr": "sum by (task_type) (rate(governance_tasks_submitted_total[1m]))",
      "legendFormat": "{{task_type}}"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "unit": "reqps",
      "custom": {
        "drawStyle": "line",
        "lineInterpolation": "linear",
        "stacking": { "mode": "normal" }
      }
    },
    "overrides": []
  },
  "options": {
    "tooltip": { "mode": "multi" },
    "legend": { "displayMode": "table", "placement": "bottom" }
  }
}
```

**Template panel with embedded alert (Panel 5 — "Routing P50/P95/P99" with alert rule #1):**

Grafana 11 embeds alert rules as a separate `"alert"` object inside the panel. The rule references the panel's query by `refId`.

```json
{
  "id": 5,
  "type": "timeseries",
  "title": "Routing duration P50/P95/P99",
  "gridPos": { "h": 8, "w": 8, "x": 0, "y": 8 },
  "datasource": { "type": "prometheus", "uid": "prom-governance" },
  "targets": [
    { "refId": "P50", "expr": "histogram_quantile(0.50, sum by (le) (rate(governance_routing_duration_ms_bucket[5m])))", "legendFormat": "P50" },
    { "refId": "P95", "expr": "histogram_quantile(0.95, sum by (le) (rate(governance_routing_duration_ms_bucket[5m])))", "legendFormat": "P95" },
    { "refId": "P99", "expr": "histogram_quantile(0.99, sum by (le) (rate(governance_routing_duration_ms_bucket[5m])))", "legendFormat": "P99" }
  ],
  "fieldConfig": {
    "defaults": { "unit": "ms" }
  },
  "alert": {
    "name": "governance-happy-path-p99-slo-burn",
    "conditions": [
      {
        "evaluator": { "params": [60], "type": "gt" },
        "operator": { "type": "and" },
        "query": { "params": ["P99", "5m", "now"] },
        "reducer": { "params": [], "type": "avg" },
        "type": "query"
      }
    ],
    "executionErrorState": "alerting",
    "for": "5m",
    "frequency": "1m",
    "handler": 1,
    "noDataState": "no_data",
    "notifications": []
  }
}
```

**Guidance for filling in the remaining panels:**
- Panels 2, 6, 8, 10, 12 follow the template of Panel 1 (stacked timeseries, `legendFormat` uses the grouping label, `unit=reqps` except where noted).
- Panels 3, 4, 7, 14 are `stat` panels. Use `"type": "stat"`, no stacking, `"reduceOptions": { "calcs": ["lastNotNull"] }`.
- Panel 9 mirrors Panel 5 exactly — three quantile queries, but swap the metric name to `governance_execution_attempts_bucket`.
- Panel 11 is a `"type": "table"` panel — single query, transform the result with a `"transformations": [{"id":"organize","options":{"excludeByName":{"Time":true}}}]`.
- Panel 13 is a row with 2 sub-panels: a stat for `up{job="otel-collector"}` (red if 0) and a timeseries for `scrape_duration_seconds{job="otel-collector"}` in seconds.
- Alert rules 2–7 attach to panels 9, 10, 10, 12, 12, 13 respectively — copy the Panel 5 alert structure, swap `name`, `expr`, `params[0]` (threshold), and `for` (duration).

Complete the JSON file with all 14 panels and 7 alerts following these patterns.

- [ ] **Step 2: Validate the JSON is well-formed**

```bash
jq empty test/observability/grafana/dashboards/governance-overview.json
echo "panels: $(jq '.panels | length' test/observability/grafana/dashboards/governance-overview.json)"
echo "alerts: $(jq '[.panels[] | select(.alert)] | length' test/observability/grafana/dashboards/governance-overview.json)"
```
Expected: `jq empty` succeeds (no output); panels = 14; alerts = 7.

- [ ] **Step 3: Verify Grafana loads the dashboard**

```bash
docker compose -f test/observability/docker-compose.yaml up -d grafana prometheus otel-collector
sleep 8
curl -fsS 'http://localhost:3000/api/dashboards/uid/governance-overview' \
  | jq '{title: .dashboard.title, panels: (.dashboard.panels | length), alerts: ([.dashboard.panels[] | select(.alert)] | length)}'
docker compose -f test/observability/docker-compose.yaml down -v
```
Expected: title "Governance Overview", panels 14, alerts 7. If 404, check Grafana logs (`docker logs obs-grafana`) for JSON parse errors.

- [ ] **Step 4: Commit**

```bash
git add test/observability/grafana/dashboards/governance-overview.json
git commit -m "feat(observability): add governance overview dashboard with 14 panels and 7 alert rules"
```

---

## Task 9: End-to-end validation with traffic

**Files:**
- No new files. Validation only. Result artefacts land in `test/observability/results/`.

- [ ] **Step 1: Drive a happy-path smoke against the observability stack**

```bash
test/observability/runner.sh happy_path smoke
```
Expected: runner completes; JSON in `test/observability/results/happy_path-smoke-<TS>.json`; a `.metrics.txt` sibling listing 10+ `governance_*` metric names.

- [ ] **Step 2: Bring the stack back up for inspection**

(The runner tears down after k6. Bring it up again to look at Grafana.)

```bash
docker compose -f test/observability/docker-compose.yaml up -d
sleep 10
# drive a bit of traffic so the dashboard has live data
for i in $(seq 1 30); do
  curl -fsS -X POST http://localhost:8082/api/v1/tasks \
    -H 'Content-Type: application/json' \
    -d '{"type":"bugfix","title":"t-'$i'","scope":"file","priority":"normal"}' >/dev/null
done
sleep 20
```

- [ ] **Step 3: Confirm dashboard shows data via Grafana API**

```bash
curl -fsS 'http://localhost:3000/api/ds/query' \
  -H 'Content-Type: application/json' \
  -d '{"queries":[{"refId":"A","expr":"sum(governance_tasks_submitted_total)","datasource":{"uid":"prom-governance","type":"prometheus"}}],"from":"now-15m","to":"now"}' \
  | jq '.results.A.frames[0].data.values[1] | last'
```
Expected: a number ≥ 30 (we just submitted 30 tasks in Step 2). If 0 or null, check collector → prometheus chain: `curl http://localhost:9090/api/v1/query?query=governance_tasks_submitted_total | jq`.

- [ ] **Step 4: Confirm all 7 alert rules are registered in Grafana**

```bash
curl -fsS 'http://localhost:3000/api/v1/provisioning/alert-rules' -H 'Accept: application/json' \
  | jq '[.[] | .title] | sort'
```
Expected: an array containing the 7 rule names (`governance-*`). If the endpoint returns 404 in Grafana 11, fall back to querying the dashboard JSON directly:
```bash
curl -fsS 'http://localhost:3000/api/dashboards/uid/governance-overview' \
  | jq '[.dashboard.panels[] | select(.alert) | .alert.name]'
```
Expected: 7 names.

- [ ] **Step 5: Trigger one alert to prove the pipeline works end-to-end**

Stop the collector (Grafana's `governance-collector-down` alert should fire within 2 min):
```bash
docker compose -f test/observability/docker-compose.yaml stop otel-collector
```
Wait 150 s, then:
```bash
curl -fsS 'http://localhost:3000/api/alertmanager/grafana/api/v2/alerts' \
  | jq '[.[] | select(.labels.alertname | test("collector-down"))]'
```
Expected: at least one active alert. If empty, the pipeline works but the rule is not firing — inspect `curl http://localhost:9090/api/v1/query?query=up` to verify `up{job="otel-collector"}` is indeed 0.

Restart the collector afterwards:
```bash
docker compose -f test/observability/docker-compose.yaml start otel-collector
```

- [ ] **Step 6: Capture a Grafana dashboard screenshot reference**

This plan does not script UI capture. Open `http://localhost:3000/d/governance-overview` in a browser and take a manual screenshot for the findings section later. Save it to `docs/observability/images/governance-overview-live.png` (create the `images/` directory).

- [ ] **Step 7: Tear down**

```bash
docker compose -f test/observability/docker-compose.yaml down -v
```

- [ ] **Step 8: Commit results**

```bash
git add test/observability/results/ docs/observability/images/
git commit -m "chore(observability): capture end-to-end validation results and dashboard screenshot"
```

If the screenshot was NOT captured (headless environment, etc.), note this in Step 8's commit message and proceed — the `.metrics.txt` + Grafana API query in Step 3 are sufficient evidence.

---

## Task 10: OTel overhead re-measurement (mandatory per spec D13)

**Goal:** The 3.5.A baseline was measured with `OTEL_ENABLED=false`. Before finalizing the SLOs we must record the delta when OTel is on. If the delta is > 10 % on P99, the SLOs may need to be loosened.

**Files:**
- Modify: `test/observability/results/` (adds one more JSON — the OTel-on smoke)

- [ ] **Step 1: Ensure no leftover observability stack is running**

```bash
docker compose -f test/observability/docker-compose.yaml ps
```
Expected: empty. If not, `docker compose -f test/observability/docker-compose.yaml down -v`.

- [ ] **Step 2: Run happy-path smoke against the OTel-enabled stack**

```bash
test/observability/runner.sh happy_path smoke
```
Expected: 60-second k6 run completes.

- [ ] **Step 3: Extract the P99 from the OTel-on smoke**

```bash
jq '.metrics.http_req_duration["p(99)"]' test/observability/results/happy_path-smoke-*.json | tail -1
```
Save this number — you will need it for `slos.md`.

- [ ] **Step 4: Retrieve the 3.5.A OTel-off P99 for comparison**

From `docs/observability/baseline-v0.6.0.md`, the happy-path smoke P99 (OTel-off, 10 VU, 60 s) is **10.93 ms**.

- [ ] **Step 5: Compute delta**

```
delta_pct = 100 * (OTel_on_P99 - 10.93) / 10.93
```

If `delta_pct > 10`, emit a warning in the commit message and flag in `slos.md` Task 11. Do NOT auto-adjust SLOs — surface the number for the human to decide.

- [ ] **Step 6: Commit the OTel-on result**

```bash
git add test/observability/results/
git commit -m "chore(observability): capture OTel-on happy-path smoke for overhead measurement"
```

---

## Task 11: SLO document

**Files:**
- Create: `docs/observability/slos.md`

- [ ] **Step 1: Write `docs/observability/slos.md`**

Use this template. Fill in the measured OTel delta from Task 10.

```markdown
# Governance-core — SLI / SLO Catalogue

**Baseline:** v0.6.0 + Phase 3.5.A load baseline
**Stack context:** Local observability harness at `test/observability/`; OTel Collector → Prometheus → Grafana
**Evaluation window (SLOs):** 30-day rolling (canonical); 1-hour and 24-hour burn rates for alerting
**Alert delivery:** Not wired in v1 — alerts fire into the Grafana UI only (see Follow-ups)

---

## OTel overhead measurement

| Metric | OTel OFF (3.5.A) | OTel ON (3.5.B) | Delta |
|--------|-------------------|-----------------|-------|
| happy_path smoke P99 (10 VU, 60 s) | 10.93 ms | <FILL FROM TASK 10 STEP 3> | <FILL FROM TASK 10 STEP 5> |

**Interpretation (human-filled after running):** <one sentence: "overhead is within 10 % and SLOs below stand" OR "overhead is X %, loosening Y SLO from Z to W">

---

## Latency SLOs

| SLI | SLO target | Baseline (3.5.A load) | Rationale |
|-----|-----------|-----------------------|-----------|
| Happy-path workflow request P99 | **< 60 ms** | 26.8 ms | 2× 3.5.A load P99 headroom |
| DLQ flow request P99 | **< 30 ms** | 13.3 ms | 2× baseline |
| Breaker flow request P99 | **< 35 ms** | 14.5 ms | 2× baseline |
| `governance_workflow_duration_ms` P99 (end-to-end) | **< 200 ms** | not measured directly | Generous — workflow lifecycle includes wait states |

If the OTel overhead delta above pushed the real numbers up, adjust the "Rationale" column to cite the updated base.

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

## Alert rules (7 total — embedded in `governance-overview.json`)

| Name | Severity | Condition | For |
|------|----------|-----------|-----|
| `governance-happy-path-p99-slo-burn` | warning | routing P99 > 60 ms | 5 min |
| `governance-workflow-p99-slo-burn` | warning | execution P99 > 200 ms | 5 min |
| `governance-task-submit-error-rate-warning` | warning | submit error rate > 0.1 % | 15 min |
| `governance-task-submit-error-rate-critical` | critical | submit error rate > 1 % | 15 min |
| `governance-breaker-storm` | warning | > 5 trips/min | 2 min |
| `governance-quarantine-growth` (experimental) | warning | > 0.1 quarantines/min | 15 min |
| `governance-collector-down` | critical | `up{job="otel-collector"} == 0` | 2 min |

---

## Caveats

- All thresholds were derived from **single-host local measurements**; real production traffic will require tuning.
- Grafana in the local stack runs with anonymous admin; **not apt for shared networks, staging, or production**.
- No soak baseline yet — any SLO involving sustained behaviour (memory stability, long-tail latency drift) is provisional until soak runs are executed.
- Alert rules are active in the Grafana UI but have **no notification delivery** — no email / Slack / PagerDuty wired. See Follow-ups.

---

## Follow-ups

- Wire a notification channel (Slack / email / PagerDuty) to Grafana alerts.
- Re-tune thresholds on first real operational traffic; the quarantine-growth rule in particular is marked experimental.
- Add soak-based SLIs once soak runs are executed (Phase 3.5.A follow-up).
- Add tracing backend (Tempo / Jaeger) in a separate track — metrics-only in 3.5.B.
- Replace anonymous Grafana admin with provisioned auth (OIDC / LDAP / password) before any non-local deployment.
```

- [ ] **Step 2: Populate the OTel delta from Task 10**

Replace `<FILL FROM TASK 10 STEP 3>` and `<FILL FROM TASK 10 STEP 5>` with the actual measured number and percentage. Write a one-sentence interpretation.

- [ ] **Step 3: Commit**

```bash
git add docs/observability/slos.md
git commit -m "docs(observability): publish SLI/SLO catalogue with measured OTel overhead"
```

---

## Task 12: README for the observability harness

**Files:**
- Create: `test/observability/README.md`

- [ ] **Step 1: Write the README**

```markdown
# Observability harness — Phase 3.5.B

Local-only OTel Collector + Prometheus + Grafana stack for validating governance-core metrics and SLOs.

## Security

**NOT apt for shared networks, staging, or production.**
- Grafana runs with `GF_AUTH_ANONYMOUS_ENABLED=true` + `GF_AUTH_ANONYMOUS_ORG_ROLE=Admin`
- No TLS, no RBAC, all ports bound to `localhost`
- Replace with proper auth (OIDC / LDAP / provisioned admin password) before any non-local use.

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

Single dashboard `governance-overview` (UID `governance-overview`) provisioned on startup. 14 panels + 7 embedded alert rules.

## Alerts

Alert rules are active in the Grafana UI but **not delivered anywhere** — no Slack / email / PagerDuty wired. See `docs/observability/slos.md` "Follow-ups" for the wire-up backlog.

## Rebuild the governance image

The stack reuses `agent-governance-core:loadtest` from the load harness. Rebuild with:

```bash
docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .
```

## Troubleshooting

- Dashboard shows "no data": check `curl http://localhost:9090/api/v1/query?query=governance_tasks_submitted_total` — if empty, collector isn't receiving OTel or isn't exposing. `docker logs obs-otel-collector` will show the reason.
- Grafana cannot reach Prometheus: both are on the same docker-compose network; verify with `docker exec obs-grafana wget -qO- http://prometheus:9090/-/ready`.
- Alert rule list is empty: ensure `test/observability/grafana/dashboards/governance-overview.json` contains panels with an `alert` object. Verify with `jq '[.panels[] | select(.alert) | .alert.name]' test/observability/grafana/dashboards/governance-overview.json`.
```

- [ ] **Step 2: Commit**

```bash
git add test/observability/README.md
git commit -m "docs(observability): add README with quickstart, ports, dashboards, troubleshooting"
```

---

## Self-review checklist

- [x] **Spec coverage — stack topology (§2):** Task 4 covers all 5 containers with the specified ports; Tasks 1, 2, 3 cover their configs
- [x] **Spec coverage — collector pipeline (§3):** Task 1
- [x] **Spec coverage — metrics (§4):** Task 8 panels reference all 14 metric names
- [x] **Spec coverage — SLOs (§5):** Task 11
- [x] **Spec coverage — dashboard (§6):** Task 8 (14 panels)
- [x] **Spec coverage — alerts (§7, 7 rules):** Task 8 + Task 11; 7 names match across both
- [x] **Spec coverage — provisioning (§8):** Task 3 + Task 4
- [x] **Spec coverage — validation flow (§9):** Task 7 + Task 9
- [x] **Spec coverage — OTel overhead re-measurement (D13):** Task 10 (mandatory step)
- [x] **Spec coverage — anonymous admin explicit warning (D7 + R):** Task 4 compose comment, Task 11 slos.md, Task 12 README
- [x] **Spec coverage — alerts fire-into-void follow-up:** Task 11 slos.md Follow-ups section
- [x] **No placeholders** in code blocks — configs are complete, JSON has one complete panel template + one complete alert template + explicit pattern guidance for the rest
- [x] **Port consistency:** 3000 / 4318 / 5434 / 8082 / 8889 / 9090 — same across compose, probes, runner, README
- [x] **Metric name consistency:** `governance_*` (Prometheus form, underscores) used in dashboard PromQL, alert expressions, and validation curls — matches collector default OTel→Prom name mapping
- [x] **Reused resources:** `agent-governance-core:loadtest` image (from 3.5.A T1), `migrations/postgres/` + `test/load/pg/init-db.sh` (from 3.5.A T3), `test/load/scripts/*.js` (parameterized in T5 here) — all present in the repo before this plan starts
