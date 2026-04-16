# Phase 3.5.A Load Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a measured baseline of agent-governance-core v0.6.0 behaviour under sustained load for 3 critical flows (happy path, DLQ, breaker) at 3 intensities each (smoke / load / soak), documented in `docs/observability/baseline-v0.6.0.md`.

**Architecture:** Docker-compose with tuned Postgres 16 + a dockerized governance-core binary (stub memory-engine, stub notifier — same as integration tests). k6 drives the HTTP API. One run per (flow × intensity), 9 total; re-run only if numbers look suspicious. No thresholds — this is DISCOVERY.

**Tech Stack:** k6, docker-compose, Postgres 16-alpine, Go 1.26.2, existing `cmd/agent-governance-core` entrypoint.

**Spec:** `docs/superpowers/specs/2026-04-16-phase3-5a-load-baseline-design.md`

---

## Prerequisites (local)

- Docker Desktop running
- `k6` installed (`brew install k6`)
- `jq` installed (`brew install jq`) — used by runner.sh to extract summary numbers
- Free TCP ports: `5433` (pg), `8081` (governance) — non-default to avoid collisions

---

## File Structure

```
test/load/
├── Dockerfile.governance           # Dockerfile for governance binary
├── docker-compose.yaml             # pg + governance
├── pg/
│   └── postgresql.tuned.conf       # tuned pg config mounted into container
├── scripts/
│   ├── happy_path.js               # k6 script
│   ├── dlq_flow.js
│   └── breaker_flow.js
├── runner.sh                       # ./runner.sh <flow> <intensity>
├── results/
│   └── .gitkeep
└── README.md                       # quickstart for engineers

docs/observability/
└── baseline-v0.6.0.md              # final findings document
```

---

## Task 1: Dockerfile for governance-core

**Files:**
- Create: `test/load/Dockerfile.governance`

- [ ] **Step 1: Create the Dockerfile**

Write `test/load/Dockerfile.governance`:

```dockerfile
# syntax=docker/dockerfile:1.6
FROM golang:1.26.2-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/governance ./cmd/agent-governance-core

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/governance /usr/local/bin/governance
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/governance"]
```

- [ ] **Step 2: Build the image to verify it compiles**

Run: `docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .`
Expected: Image builds; final line like `Successfully tagged agent-governance-core:loadtest`.

- [ ] **Step 3: Commit**

```bash
git add test/load/Dockerfile.governance
git commit -m "chore(load): add dockerfile for governance-core load test binary"
```

---

## Task 2: Tuned postgres config

**Files:**
- Create: `test/load/pg/postgresql.tuned.conf`

- [ ] **Step 1: Write the tuned pg config**

Write `test/load/pg/postgresql.tuned.conf`:

```conf
# Baseline tuning for load tests. Fits a dev Mac M-series single host.
# Do NOT copy blindly to production.

listen_addresses = '*'
max_connections = 100

# Memory
shared_buffers = 256MB
effective_cache_size = 1GB
work_mem = 16MB
maintenance_work_mem = 64MB

# WAL
wal_level = replica
max_wal_size = 1GB
min_wal_size = 128MB
checkpoint_completion_target = 0.9

# Parallelism
max_worker_processes = 8
max_parallel_workers = 4
max_parallel_workers_per_gather = 2

# Stats
track_activities = on
track_counts = on
track_io_timing = on
track_wal_io_timing = on
log_min_duration_statement = 200ms
log_lock_waits = on
log_temp_files = 0
log_checkpoints = on
```

- [ ] **Step 2: Commit**

```bash
git add test/load/pg/postgresql.tuned.conf
git commit -m "chore(load): add tuned postgres config for load tests"
```

---

## Task 3: docker-compose

**Files:**
- Create: `test/load/docker-compose.yaml`

- [ ] **Step 1: Write docker-compose.yaml**

Write `test/load/docker-compose.yaml`:

```yaml
services:
  pg:
    image: postgres:16-alpine
    container_name: load-pg
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: governance
    ports:
      - "5433:5432"
    volumes:
      - ./pg/postgresql.tuned.conf:/etc/postgresql/postgresql.conf:ro
      - ../../migrations/postgres:/docker-entrypoint-initdb.d:ro
    command:
      - "postgres"
      - "-c"
      - "config_file=/etc/postgresql/postgresql.conf"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d governance"]
      interval: 2s
      timeout: 2s
      retries: 20
    cpus: "2.0"
    mem_limit: 1536m

  governance:
    image: agent-governance-core:loadtest
    container_name: load-governance
    depends_on:
      pg:
        condition: service_healthy
    environment:
      PORT: "8080"
      DB_HOST: pg
      DB_PORT: "5432"
      DB_USER: postgres
      DB_PASSWORD: postgres
      DB_NAME: governance
      DB_SSLMODE: disable
      LOG_LEVEL: info
      OTEL_ENABLED: "false"
      ADAPTIVE_ROUTING_ENABLED: "false"
    ports:
      - "8081:8080"
    cpus: "2.0"
    mem_limit: 1024m
```

**Note on migrations (D8 fix applied during execution):** Our migrations use goose annotations (`-- +goose Up`, `-- +goose Down`). If we dropped the raw `.sql` files into `/docker-entrypoint-initdb.d`, pg would run both sections, dropping what it just created. Workaround: mount migrations at `/migrations` (read-only) and drop an `init-db.sh` in `initdb.d` that extracts only the Up section with `awk` before piping to `psql -v ON_ERROR_STOP=1`. See `test/load/pg/init-db.sh`. This is a harness fix committed with T3.

- [ ] **Step 2: Bring up the stack and verify health**

Run:
```bash
docker compose -f test/load/docker-compose.yaml up -d
docker compose -f test/load/docker-compose.yaml ps
```
Expected: both services `running`, pg `healthy`, governance logs show `"server starting"` on port 8080.

- [ ] **Step 3: Smoke probe the HTTP API**

Run:
```bash
curl -sS -X POST http://localhost:8081/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"type":"bugfix","title":"probe","scope":"file","priority":"normal"}'
```
Expected: HTTP 200 or 201 with a JSON body containing a `task_id` (or equivalent).

- [ ] **Step 4: Tear down**

Run: `docker compose -f test/load/docker-compose.yaml down -v`
Expected: both containers removed, `pg_data` volume removed.

- [ ] **Step 5: Commit**

```bash
git add test/load/docker-compose.yaml
git commit -m "chore(load): add docker-compose stack with tuned pg and governance binary"
```

---

## Task 4: Runner script and results skeleton

**Files:**
- Create: `test/load/runner.sh`
- Create: `test/load/results/.gitkeep`
- Create: `test/load/README.md`

- [ ] **Step 1: Write `test/load/runner.sh`**

```bash
#!/usr/bin/env bash
# Usage: ./runner.sh <flow> <intensity>
#   flow:      happy_path | dlq_flow | breaker_flow
#   intensity: smoke | load | soak
#
# Brings up docker-compose, waits for health, runs the k6 script,
# captures the JSON summary to results/, and tears the stack down.

set -euo pipefail

FLOW="${1:-}"
INTENSITY="${2:-}"

if [[ -z "${FLOW}" || -z "${INTENSITY}" ]]; then
  echo "usage: $0 <happy_path|dlq_flow|breaker_flow> <smoke|load|soak>" >&2
  exit 2
fi

SCRIPT="scripts/${FLOW}.js"
if [[ ! -f "${SCRIPT}" ]]; then
  echo "no such script: ${SCRIPT}" >&2
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
OUT="results/${FLOW}-${INTENSITY}-${TS}.json"

echo ">> bringing up stack"
docker compose up -d

echo ">> waiting for governance health"
for i in $(seq 1 30); do
  if curl -fsS http://localhost:8081/api/v1/audit?limit=1 >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo ">> running k6 (${FLOW} / ${INTENSITY})"
K6_INTENSITY="${INTENSITY}" k6 run --summary-export="${OUT}" "${SCRIPT}"

echo ">> capturing pg stats snapshot"
PG_OUT="results/${FLOW}-${INTENSITY}-${TS}.pg.txt"
{
  echo "-- active connections peak / current"
  docker exec load-pg psql -U postgres -d governance -c "select count(*) from pg_stat_activity where datname='governance';"
  echo "-- deadlocks"
  docker exec load-pg psql -U postgres -d governance -c "select deadlocks from pg_stat_database where datname='governance';"
  echo "-- top 5 slowest"
  docker exec load-pg psql -U postgres -d governance -c "select substring(query,1,80) as q, calls, mean_exec_time from pg_stat_statements order by mean_exec_time desc limit 5;" 2>/dev/null || echo "(pg_stat_statements not enabled)"
} > "${PG_OUT}" || true

echo ">> tearing down"
docker compose down -v

echo ">> done: ${OUT}"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x test/load/runner.sh`

- [ ] **Step 3: Write `test/load/README.md`**

```markdown
# Load harness — Phase 3.5.A baseline

Quickstart:

```bash
cd test/load
./runner.sh happy_path smoke
./runner.sh happy_path load
./runner.sh happy_path soak
./runner.sh dlq_flow     smoke
./runner.sh dlq_flow     load
./runner.sh dlq_flow     soak
./runner.sh breaker_flow smoke
./runner.sh breaker_flow load
./runner.sh breaker_flow soak
```

Results land in `results/<flow>-<intensity>-<timestamp>.json` and `.pg.txt`.

Ports used on host:
- `5433` → postgres
- `8081` → governance HTTP API

Depends on: Docker Desktop running, `k6` and `jq` installed via Homebrew.
```

- [ ] **Step 4: Create results placeholder**

Run: `echo "" > test/load/results/.gitkeep`

- [ ] **Step 5: Commit**

```bash
git add test/load/runner.sh test/load/results/.gitkeep test/load/README.md
git commit -m "chore(load): add runner script and results directory"
```

---

## Task 5: `happy_path.js` k6 script

**Files:**
- Create: `test/load/scripts/happy_path.js`

- [ ] **Step 1: Write the script**

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = 'http://localhost:8081';
const INTENSITY = __ENV.K6_INTENSITY || 'smoke';

const profiles = {
  smoke: { vus: 10, duration: '1m' },
  load:  { vus: 50, duration: '5m' },
  soak:  { vus: 50, duration: '30m' },
};
const profile = profiles[INTENSITY];
if (!profile) { throw new Error(`unknown intensity: ${INTENSITY}`); }

export const options = {
  vus: profile.vus,
  duration: profile.duration,
  // No thresholds — DISCOVERY, not validation.
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

const latSubmit   = new Trend('gov_submit_ms',   true);
const latRoute    = new Trend('gov_route_ms',    true);
const latEval     = new Trend('gov_eval_ms',     true);
const latStart    = new Trend('gov_start_ms',    true);
const latAttempt  = new Trend('gov_attempt_ms',  true);

function postJSON(path, body) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
}

export default function () {
  // 1. Submit
  const sub = postJSON('/api/v1/tasks', {
    type: 'bugfix',
    title: `load-${__VU}-${__ITER}`,
    scope: 'file',
    priority: 'normal',
  });
  latSubmit.add(sub.timings.duration);
  check(sub, { 'submit 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (sub.status >= 300) { return; }
  const taskID = sub.json('id') || sub.json('task_id');

  // 2. Route
  const rt = postJSON(`/api/v1/tasks/${taskID}/route`, {});
  latRoute.add(rt.timings.duration);
  check(rt, { 'route 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (rt.status >= 300) { return; }

  // 3. Evaluate policy
  const ev = postJSON(`/api/v1/tasks/${taskID}/evaluate-policy`, { action: 'file_write' });
  latEval.add(ev.timings.duration);
  check(ev, { 'eval 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (ev.status >= 300) { return; }

  // 4. Start workflow
  const st = postJSON(`/api/v1/tasks/${taskID}/start-workflow`, {});
  latStart.add(st.timings.duration);
  check(st, { 'start 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (st.status >= 300) { return; }
  const wfID = st.json('id') || st.json('workflow_run_id');

  // 5. Register 2 successful attempts
  for (let i = 0; i < 2; i++) {
    const at = postJSON(`/api/v1/workflows/${wfID}/attempts`, {
      status: 'success',
      tool_name: 'shell',
      agent_role: 'implementer',
    });
    latAttempt.add(at.timings.duration);
    check(at, { 'attempt 2xx': (r) => r.status >= 200 && r.status < 300 });
  }

  sleep(0.1);
}
```

- [ ] **Step 2: Lint the script by invoking k6 in dry run**

Run: `k6 inspect test/load/scripts/happy_path.js`
Expected: prints summary of options and exits 0. Any parse error → fix before continuing.

- [ ] **Step 3: Commit**

```bash
git add test/load/scripts/happy_path.js
git commit -m "chore(load): add k6 script for happy path flow"
```

---

## Task 6: `dlq_flow.js` k6 script

**Files:**
- Create: `test/load/scripts/dlq_flow.js`

- [ ] **Step 1: Write the script**

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = 'http://localhost:8081';
const INTENSITY = __ENV.K6_INTENSITY || 'smoke';

const profiles = {
  smoke: { vus: 5,  duration: '1m'  },
  load:  { vus: 25, duration: '5m'  },
  soak:  { vus: 25, duration: '30m' },
};
const profile = profiles[INTENSITY];
if (!profile) { throw new Error(`unknown intensity: ${INTENSITY}`); }

export const options = {
  vus: profile.vus,
  duration: profile.duration,
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

const latAttempt  = new Trend('gov_attempt_ms', true);

function postJSON(path, body) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
}

function createWorkflow(iter) {
  const sub = postJSON('/api/v1/tasks', {
    type: 'bugfix',
    title: `dlq-${__VU}-${iter}`,
    scope: 'file',
    priority: 'normal',
  });
  if (sub.status >= 300) { return null; }
  const taskID = sub.json('id') || sub.json('task_id');
  const rt = postJSON(`/api/v1/tasks/${taskID}/route`, {});
  if (rt.status >= 300) { return null; }
  const ev = postJSON(`/api/v1/tasks/${taskID}/evaluate-policy`, { action: 'file_write' });
  if (ev.status >= 300) { return null; }
  const st = postJSON(`/api/v1/tasks/${taskID}/start-workflow`, {});
  if (st.status >= 300) { return null; }
  return st.json('id') || st.json('workflow_run_id');
}

export default function () {
  const wfID = createWorkflow(__ITER);
  if (!wfID) { return; }

  // Register retryable failures until the retry budget is exhausted
  // and the workflow is quarantined. The service enforces the budget;
  // we just keep pushing failures and let the server decide when to
  // stop accepting.
  for (let i = 0; i < 10; i++) {
    const at = postJSON(`/api/v1/workflows/${wfID}/attempts`, {
      status: 'failure',
      failure_stage: 'runtime',
      failure_code: 'tool/shell_timeout',
      retryable: true,
      tool_name: 'shell',
      agent_role: 'implementer',
    });
    latAttempt.add(at.timings.duration);
    // Stop once server refuses further attempts (workflow terminal).
    if (at.status >= 400) { break; }
    check(at, { 'attempt accepted': (r) => r.status >= 200 && r.status < 300 });
  }

  sleep(0.1);
}
```

- [ ] **Step 2: Lint**

Run: `k6 inspect test/load/scripts/dlq_flow.js`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add test/load/scripts/dlq_flow.js
git commit -m "chore(load): add k6 script for DLQ/quarantine flow"
```

---

## Task 7: `breaker_flow.js` k6 script

**Files:**
- Create: `test/load/scripts/breaker_flow.js`

- [ ] **Step 1: Write the script**

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = 'http://localhost:8081';
const INTENSITY = __ENV.K6_INTENSITY || 'smoke';

const profiles = {
  smoke: { vus: 5,  duration: '1m'  },
  load:  { vus: 25, duration: '5m'  },
  soak:  { vus: 25, duration: '30m' },
};
const profile = profiles[INTENSITY];
if (!profile) { throw new Error(`unknown intensity: ${INTENSITY}`); }

export const options = {
  vus: profile.vus,
  duration: profile.duration,
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

const latAttempt = new Trend('gov_attempt_ms', true);
const latList    = new Trend('gov_list_breakers_ms', true);

function postJSON(path, body) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
}

function createWorkflow(tag) {
  const sub = postJSON('/api/v1/tasks', {
    type: 'bugfix',
    title: `breaker-${tag}`,
    scope: 'file',
    priority: 'normal',
  });
  if (sub.status >= 300) { return null; }
  const taskID = sub.json('id') || sub.json('task_id');
  const rt = postJSON(`/api/v1/tasks/${taskID}/route`, {});
  if (rt.status >= 300) { return null; }
  const ev = postJSON(`/api/v1/tasks/${taskID}/evaluate-policy`, { action: 'file_write' });
  if (ev.status >= 300) { return null; }
  const st = postJSON(`/api/v1/tasks/${taskID}/start-workflow`, {});
  if (st.status >= 300) { return null; }
  return st.json('id') || st.json('workflow_run_id');
}

export default function () {
  const wfID = createWorkflow(`${__VU}-${__ITER}`);
  if (!wfID) { return; }

  // Force 3 consecutive failures on the same (tool, role) to trip the breaker.
  for (let i = 0; i < 3; i++) {
    const at = postJSON(`/api/v1/workflows/${wfID}/attempts`, {
      status: 'failure',
      failure_stage: 'runtime',
      failure_code: 'tool/shell_timeout',
      retryable: true,
      tool_name: 'shell',
      agent_role: 'implementer',
    });
    latAttempt.add(at.timings.duration);
    if (at.status >= 400) { break; }
  }

  // Observe the tripped state.
  const ls = http.get(`${BASE}/api/v1/breakers?state=open`);
  latList.add(ls.timings.duration);
  check(ls, { 'list breakers 2xx': (r) => r.status >= 200 && r.status < 300 });

  sleep(0.1);
}
```

- [ ] **Step 2: Lint**

Run: `k6 inspect test/load/scripts/breaker_flow.js`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add test/load/scripts/breaker_flow.js
git commit -m "chore(load): add k6 script for circuit breaker flow"
```

---

## Task 8: Smoke runs (3 flows × 1 min)

**Files:**
- Modify: `test/load/results/` (writes JSON + pg.txt per run)

- [ ] **Step 1: Run happy path smoke**

Run (from repo root):
```bash
cd test/load && ./runner.sh happy_path smoke && cd -
```
Expected: runner completes; a new `results/happy_path-smoke-<TS>.json` exists. Non-2xx rate is low (< 1%). If the run crashes or 2xx rate is low, stop — D8 (critical fix) applies: investigate and fix the minimal thing that unblocks the harness, then re-run.

- [ ] **Step 2: Run DLQ smoke**

Run: `cd test/load && ./runner.sh dlq_flow smoke && cd -`
Expected: `results/dlq_flow-smoke-<TS>.json` exists. Some attempts return 4xx once the workflow is terminal — that is expected, not a failure.

- [ ] **Step 3: Run breaker smoke**

Run: `cd test/load && ./runner.sh breaker_flow smoke && cd -`
Expected: `results/breaker_flow-smoke-<TS>.json` exists. `list breakers 2xx` is near 100%.

- [ ] **Step 4: Commit the smoke results**

```bash
git add test/load/results/
git commit -m "chore(load): capture smoke baseline results (happy / dlq / breaker)"
```

---

## Task 9: Load runs (3 flows × 5 min)

**Files:**
- Modify: `test/load/results/` (writes JSON + pg.txt per run)

- [ ] **Step 1: Happy path load**

Run: `cd test/load && ./runner.sh happy_path load && cd -`
Expected: 5-minute run completes; JSON captured. Watch governance container logs in a second terminal (`docker logs -f load-governance`) for anomalies — high-error spikes, panics. If any panic occurs, stop and apply D8.

- [ ] **Step 2: DLQ load**

Run: `cd test/load && ./runner.sh dlq_flow load && cd -`
Expected: JSON captured.

- [ ] **Step 3: Breaker load**

Run: `cd test/load && ./runner.sh breaker_flow load && cd -`
Expected: JSON captured.

- [ ] **Step 4: Commit load results**

```bash
git add test/load/results/
git commit -m "chore(load): capture load baseline results (happy / dlq / breaker)"
```

---

## Task 10: Soak runs (3 flows × 30 min) — DEFERRED

**Status:** Deferred out of the current iteration. The runner script and k6 scripts already support `soak` intensity — no code changes required when it is picked up. See Task 11 findings doc which must call out soak as a named follow-up.

**When picked up:**
- `cd test/load && ./runner.sh happy_path soak && cd -`
- `cd test/load && ./runner.sh dlq_flow soak && cd -`
- `cd test/load && ./runner.sh breaker_flow soak && cd -`
- Capture `docker stats` at t=0 / t=15m / t=30m per run for drift tracking.
- Append a "Soak results" section to `docs/observability/baseline-v0.6.0.md` and bump the doc header.

---

## Task 11: Findings document

**Files:**
- Create: `docs/observability/baseline-v0.6.0.md`

- [ ] **Step 1: Extract summary numbers from each JSON**

Run these 9 commands (one per run — pick the actual timestamp suffix from `ls test/load/results/`):

```bash
for f in test/load/results/*.json; do
  echo "=== $f ==="
  jq '{
    iterations: .metrics.iterations.count,
    req_count:  .metrics.http_reqs.count,
    req_failed: .metrics.http_req_failed.value,
    req_p50:    .metrics.http_req_duration["p(50)"],
    req_p95:    .metrics.http_req_duration["p(95)"],
    req_p99:    .metrics.http_req_duration["p(99)"],
    req_max:    .metrics.http_req_duration.max,
    vus_max:    .metrics.vus_max.max
  }' "$f"
done
```

Save the output — you will paste it into the findings doc.

- [ ] **Step 2: Write `docs/observability/baseline-v0.6.0.md`**

Use this exact template and fill in the measured numbers. Leave a section blank only if that run genuinely produced no data (and explain why).

```markdown
# agent-governance-core v0.6.0 — Load Baseline

**Date measured:** YYYY-MM-DD
**Commit:** <git rev-parse --short HEAD>
**Host:** <your Mac model / CPU cores / RAM>
**Harness:** docker-compose (pg 16-alpine + governance binary); stub memory-engine; stub notifier; OTel disabled
**Tool:** k6
**Purpose:** DISCOVERY baseline — NOT validation. No SLOs yet.
**Scope of this iteration:** smoke + load ONLY. Soak (30-minute sustained) intentionally deferred. See "Follow-ups" section below.

---

## Summary table

| Flow          | Intensity | VUs | Duration | Iter/s | Req/s | P50 (ms) | P95 (ms) | P99 (ms) | Err % |
|---------------|-----------|-----|----------|--------|-------|----------|----------|----------|-------|
| happy_path    | smoke     | 10  | 1m       |        |       |          |          |          |       |
| happy_path    | load      | 50  | 5m       |        |       |          |          |          |       |
| happy_path    | soak      | 50  | 30m      | DEFERRED — follow-up |||||||
| dlq_flow      | smoke     | 5   | 1m       |        |       |          |          |          |       |
| dlq_flow      | load      | 25  | 5m       |        |       |          |          |          |       |
| dlq_flow      | soak      | 25  | 30m      | DEFERRED — follow-up |||||||
| breaker_flow  | smoke     | 5   | 1m       |        |       |          |          |          |       |
| breaker_flow  | load      | 25  | 5m       |        |       |          |          |          |       |
| breaker_flow  | soak      | 25  | 30m      | DEFERRED — follow-up |||||||

---

## Per-flow findings

### happy_path

**Sustained throughput (load):** <iter/s>
**Sustained throughput (soak):** <iter/s>
**First saturation cause:** <pg connections | CPU | goroutines | none observed>
**Soak observations:**
- RSS at t=0 / t=15m / t=30m: <MB> / <MB> / <MB>
- Goroutine count at t=0 / t=15m / t=30m: <N> / <N> / <N>
- Drift verdict: <flat | growing — suspect leak | shrinking after GC>

Raw summary (condensed from k6 JSON):
```
<paste the jq block for the 3 happy_path runs>
```

### dlq_flow

Same structure as happy_path.

### breaker_flow

Same structure as happy_path. Add:
- Number of unique (tool, role) breakers tripped during soak
- `/api/v1/breakers?state=open` P99 latency

---

## Known limits (current ceiling)

One short paragraph per flow. Example: "happy_path sustains ~X iter/s at 50 VU. Latency knee appears between Y and Z VU — above that, P99 grows non-linearly. First saturation cause is <pg connections | CPU>."

---

## Bugs / anomalies discovered

List anything unexpected — panics, leaks, weird numbers, failed assertions. Each entry:
- What happened
- When (which run)
- Repro pointer (which k6 run JSON)
- Filed as issue? (link or "deferred")

If D8 (critical fix) was applied to unblock the harness, record the fix here.

---

## Caveats

- Single-host run (pg + governance + k6 on the same Mac). Numbers are comparable across future runs on the same host; they are NOT a prediction of production throughput.
- Stubs for memory-engine and notifier. Real dependencies may add latency — measured in Phase 3.5.C.
- OTel disabled. Cost of telemetry not reflected in these numbers.
- pg_stat_statements was not enabled; slow-query analysis is approximate.

---

## Follow-ups (deferred from this iteration)

- **Soak baseline (30-minute sustained) for all 3 flows.** Deferred to keep this iteration tight (~90 min wall-clock). The runner and k6 scripts already support the `soak` intensity — no code changes needed to pick it up. When executed, append a "Soak results" section here and bump the doc header. Purpose: detect memory/goroutine leaks and cumulative degradation that smoke+load cannot reveal.
- **Re-run of any flow × intensity that looked suspicious** in this iteration. Each re-run is appended here (not replacing the original) so we keep the variability visible.
- **Enable `pg_stat_statements`** in the tuned pg config for deeper slow-query analysis.
- **Repeat the full matrix on a dedicated staging host** once one exists — single-host Mac numbers carry a local-host caveat.

## Hand-off to Phase 3.5.B

The numbers above are the raw input for SLO definition in Phase 3.5.B. The SLO exercise should anchor targets to this baseline (e.g. "aim for P99 ≤ 2× current") rather than to aspirational numbers. Note that soak numbers are not yet available — SLOs involving sustained behaviour (memory stability, long-tail latency drift) should wait until soak runs are executed.
```

- [ ] **Step 3: Commit**

```bash
git add docs/observability/baseline-v0.6.0.md
git commit -m "docs(load): add v0.6.0 baseline findings document"
```

- [ ] **Step 4: Final verification and tag**

Run:
```bash
ls test/load/results/ | wc -l     # expect >= 18 (9 JSON + 9 pg.txt)
git log --oneline -15
```
Expected: 11 new commits since v0.6.0, all under `chore(load):` or `docs(load):`. Phase 3.5.A closed.

No tag in this phase — baseline is documented; tagging is for shipped behaviour changes. Phase 3.5.B will be the next candidate for a tag bump.

---

## Self-review checklist

- [x] Each flow in the spec has a dedicated k6 script + script task + 3 run steps
- [x] `max_connections=100`, VUs pico 50, docker-compose dedicated, stubs for memory-engine / notifier — all reflected
- [x] 1 run per (flow × intensity), no auto-re-run in the plan; re-run only explicitly if numbers are suspicious
- [x] D7 (no fixes) + D8 (critical-fix exception) reflected in Task 8–10 step notes
- [x] Findings template covers all measurements specified in spec §5
- [x] No "TBD" / no "add appropriate handling" / full code in every step
- [x] Paths, ports, and env var names consistent between Dockerfile, docker-compose, runner.sh, and k6 scripts (8081 host → 8080 container; pg 5433 host → 5432 container; DB_* vars match `config.Load()`)
- [x] No code paths invented — HTTP routes verified against `internal/adapters/inbound/http/router.go`
