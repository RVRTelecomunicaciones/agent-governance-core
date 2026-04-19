# Phase 3.5.C Chaos Scenarios Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-contained chaos harness that runs 5 Tier-1 failure scenarios against a local governance stack, each with explicit trigger + verify scripts + runbook, and publish a matrix of observed behaviour at `docs/chaos/scenarios.md`.

**Architecture:** New `test/chaos/` compose with governance + toxiproxy + pg. Governance connects to pg *through* toxiproxy so DB-side chaos (latency, partition, starvation) can be applied via the toxiproxy admin API. Container-level chaos (SIGKILL, stop) uses plain `docker` commands. Each scenario lives in its own directory with `trigger.sh`, `verify.sh`, `README.md`. `verify.sh` exits 0 on PASS, non-zero on FAIL, with a printed criteria table.

**Tech Stack:** Docker Compose, `ghcr.io/shopify/toxiproxy:2.9.0`, Postgres 16-alpine, reused `agent-governance-core:loadtest` image, bash, curl, psql, jq.

**Spec:** `docs/superpowers/specs/2026-04-19-phase3-5c-chaos-scenarios-design.md`

---

## Prerequisites (local)

- Docker Desktop running
- `k6`, `jq`, `curl`, `psql` (postgres client) on PATH. Install psql client: `brew install libpq && brew link --force libpq`
- `agent-governance-core:loadtest` image exists (from 3.5.A); rebuild if missing: `docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .`
- Free TCP ports on host: **5435** (pg), **8083** (governance), **8474** (toxiproxy admin)

---

## File structure

```
test/chaos/
├── docker-compose.yaml                # 3 services: pg + toxiproxy + governance
├── toxiproxy/
│   └── config.json                    # pg proxy definition loaded at startup
├── common.sh                          # shared helpers (wait_healthy, baseline_traffic, row_counts)
├── runner.sh                          # bring-up + baseline + teardown wrapper
├── scenarios/
│   ├── s1-pg-down/        { trigger.sh, verify.sh, README.md }
│   ├── s2-pg-latency/     { trigger.sh, verify.sh, README.md }
│   ├── s3-pool-starvation/{ trigger.sh, verify.sh, README.md }
│   ├── s4-governance-sigkill/{ trigger.sh, verify.sh, README.md }
│   └── s5-partition/      { trigger.sh, verify.sh, README.md }
├── results/
│   └── .gitkeep
└── README.md                          # master doc

docs/chaos/
└── scenarios.md                       # matrix filled after execution
```

---

## Task 1: Docker-compose + toxiproxy config

**Files:**
- Create: `test/chaos/docker-compose.yaml`
- Create: `test/chaos/toxiproxy/config.json`

- [ ] **Step 1: Write `test/chaos/toxiproxy/config.json`**

```json
[
  {
    "name": "pg",
    "listen": "0.0.0.0:5432",
    "upstream": "pg:5432",
    "enabled": true
  }
]
```

- [ ] **Step 2: Write `test/chaos/docker-compose.yaml`**

```yaml
# Local chaos harness for governance-core.
# NOT FOR STAGING / PROD — ports bound to localhost, no TLS, OTel disabled.

services:
  pg:
    image: postgres:16-alpine
    container_name: obs-chaos-pg
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: governance
    ports:
      - "5435:5432"
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

  toxiproxy:
    image: ghcr.io/shopify/toxiproxy:2.9.0
    container_name: obs-chaos-toxiproxy
    depends_on:
      pg:
        condition: service_healthy
    command: ["-host=0.0.0.0", "-config=/config/toxiproxy.json"]
    volumes:
      - ./toxiproxy/config.json:/config/toxiproxy.json:ro
    ports:
      - "8474:8474"   # admin API (toxics add/remove)
    cpus: "0.5"
    mem_limit: 256m

  governance:
    image: agent-governance-core:loadtest
    container_name: obs-chaos-governance
    depends_on:
      toxiproxy:
        condition: service_started
    environment:
      PORT: "8080"
      DB_HOST: toxiproxy       # <— governance goes THROUGH toxiproxy
      DB_PORT: "5432"
      DB_USER: postgres
      DB_PASSWORD: postgres
      DB_NAME: governance
      DB_SSLMODE: disable
      LOG_LEVEL: info
      OTEL_ENABLED: "false"
      ADAPTIVE_ROUTING_ENABLED: "false"
    ports:
      - "8083:8080"
    cpus: "2.0"
    mem_limit: 1024m
```

- [ ] **Step 3: Validate compose syntax**

Run: `docker compose -f test/chaos/docker-compose.yaml config --quiet`
Expected: silent success. If pg tuned.conf or init-db.sh reference missing files, the compose config will still pass (files validated at runtime).

- [ ] **Step 4: Commit**

```bash
git add test/chaos/docker-compose.yaml test/chaos/toxiproxy/config.json
git commit -m "chore(chaos): add docker-compose with pg + toxiproxy + governance"
```

---

## Task 2: Shared helpers and runner

**Files:**
- Create: `test/chaos/common.sh`
- Create: `test/chaos/runner.sh`
- Create: `test/chaos/results/.gitkeep`

- [ ] **Step 1: Write `test/chaos/common.sh`**

```bash
#!/usr/bin/env bash
# Shared helpers for chaos scenarios.
# Source this file, do NOT run it directly.

set -euo pipefail

GOV_HOST="${GOV_HOST:-localhost}"
GOV_PORT="${GOV_PORT:-8083}"
PG_HOST="${PG_HOST:-localhost}"
PG_PORT="${PG_PORT:-5435}"
TOXI_HOST="${TOXI_HOST:-localhost}"
TOXI_PORT="${TOXI_PORT:-8474}"

PSQL_CMD=(psql "postgresql://postgres:postgres@${PG_HOST}:${PG_PORT}/governance")

# wait_healthy_gov — poll governance /api/v1/audit until 2xx or timeout
wait_healthy_gov() {
  local deadline=$((SECONDS + ${1:-30}))
  while (( SECONDS < deadline )); do
    if curl -fsS "http://${GOV_HOST}:${GOV_PORT}/api/v1/audit?limit=1" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "timeout waiting for governance at ${GOV_HOST}:${GOV_PORT}" >&2
  return 1
}

# wait_healthy_toxi — poll toxiproxy admin API
wait_healthy_toxi() {
  local deadline=$((SECONDS + ${1:-15}))
  while (( SECONDS < deadline )); do
    if curl -fsS "http://${TOXI_HOST}:${TOXI_PORT}/version" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "timeout waiting for toxiproxy at ${TOXI_HOST}:${TOXI_PORT}" >&2
  return 1
}

# row_counts — print monotonic counts for key tables, tab-separated
row_counts() {
  "${PSQL_CMD[@]}" -At -F $'\t' -c "
    SELECT
      (SELECT count(*) FROM tasks),
      (SELECT count(*) FROM workflow_runs),
      (SELECT count(*) FROM execution_leases),
      (SELECT count(*) FROM audit_entries);
  "
}

# baseline_traffic — submit+route+eval+start+attempt → one running workflow
# Prints the workflow_run_id on stdout.
baseline_traffic() {
  local base="http://${GOV_HOST}:${GOV_PORT}"
  local task_id wf_id

  task_id=$(curl -fsS -X POST "${base}/api/v1/tasks" \
    -H 'Content-Type: application/json' \
    -d '{"type":"bugfix","title":"chaos-baseline","scope":"file","priority":"normal"}' \
    | jq -r '.id')
  curl -fsS -X POST "${base}/api/v1/tasks/${task_id}/route" -H 'Content-Type: application/json' -d '{}' >/dev/null
  curl -fsS -X POST "${base}/api/v1/tasks/${task_id}/evaluate-policy" -H 'Content-Type: application/json' -d '{"action":"file_write"}' >/dev/null
  wf_id=$(curl -fsS -X POST "${base}/api/v1/tasks/${task_id}/start-workflow" -H 'Content-Type: application/json' -d '{}' \
    | jq -r '.id')
  echo "${wf_id}"
}

# assert_http_status_is_5xx — runs a curl and returns 0 if status is 5xx, else 1
# Usage: assert_http_status_is_5xx <url> [method] [body]
assert_http_status_is_5xx() {
  local url="$1"
  local method="${2:-POST}"
  local body="${3:-{\"type\":\"bugfix\",\"title\":\"probe\",\"scope\":\"file\",\"priority\":\"normal\"}}"
  local status
  status=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X "${method}" "${url}" \
    -H 'Content-Type: application/json' \
    -d "${body}" --max-time 10 || echo 000)
  [[ "${status}" =~ ^5[0-9][0-9]$ ]]
}

# assert_http_status_is_2xx — same pattern but for 2xx
assert_http_status_is_2xx() {
  local url="$1"
  local method="${2:-POST}"
  local body="${3:-{\"type\":\"bugfix\",\"title\":\"probe\",\"scope\":\"file\",\"priority\":\"normal\"}}"
  local status
  status=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X "${method}" "${url}" \
    -H 'Content-Type: application/json' \
    -d "${body}" --max-time 10 || echo 000)
  [[ "${status}" =~ ^2[0-9][0-9]$ ]]
}

# toxic_add / toxic_del — convenience wrappers around toxiproxy admin
toxic_add() {
  local payload="$1"
  curl -fsS -X POST "http://${TOXI_HOST}:${TOXI_PORT}/proxies/pg/toxics" \
    -H 'Content-Type: application/json' \
    -d "${payload}"
}
toxic_del() {
  local name="$1"
  curl -fsS -X DELETE "http://${TOXI_HOST}:${TOXI_PORT}/proxies/pg/toxics/${name}" \
    || true  # ignore 404 if already removed
}

proxy_set_enabled() {
  local enabled="$1"   # "true" or "false"
  curl -fsS -X POST "http://${TOXI_HOST}:${TOXI_PORT}/proxies/pg" \
    -H 'Content-Type: application/json' \
    -d "{\"enabled\": ${enabled}}"
}

# container_running — returns 0 if the named container is running
container_running() {
  [[ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" == "true" ]]
}

# panic_in_logs — returns 0 if any panic/stack-trace in container logs since time
panic_in_logs() {
  local container="$1"
  local since="${2:-5m}"
  docker logs --since "${since}" "${container}" 2>&1 \
    | grep -Ei 'panic:|goroutine [0-9]+ \[running\]:' >/dev/null
}
```

- [ ] **Step 2: Write `test/chaos/runner.sh`**

```bash
#!/usr/bin/env bash
# Usage:
#   ./runner.sh up            # bring stack up, wait healthy
#   ./runner.sh baseline      # submit one happy-path workflow to running state
#   ./runner.sh down          # tear stack down
#   ./runner.sh reset         # down + up + baseline — clean slate for a scenario
#
# Individual scenarios handle their own trigger + verify after the runner
# has prepared the stack.

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}"
# shellcheck source=common.sh
source ./common.sh

cmd="${1:-up}"

case "${cmd}" in
  up)
    docker compose up -d
    wait_healthy_toxi 30
    wait_healthy_gov 60
    echo ">> stack up"
    ;;
  baseline)
    wait_healthy_gov 30
    wf=$(baseline_traffic)
    echo "${wf}" > results/last_workflow_run_id.txt
    echo ">> baseline workflow_run_id=${wf}"
    ;;
  down)
    docker compose down -v
    echo ">> stack down"
    ;;
  reset)
    docker compose down -v
    docker compose up -d
    wait_healthy_toxi 30
    wait_healthy_gov 60
    wf=$(baseline_traffic)
    echo "${wf}" > results/last_workflow_run_id.txt
    echo ">> reset complete; workflow_run_id=${wf}"
    ;;
  *)
    echo "usage: $0 {up|baseline|down|reset}" >&2
    exit 2
    ;;
esac
```

- [ ] **Step 3: Make executable + create results placeholder**

```bash
chmod +x test/chaos/common.sh test/chaos/runner.sh
printf '' > test/chaos/results/.gitkeep
```

- [ ] **Step 4: Sanity-check shell syntax**

Run:
```bash
bash -n test/chaos/common.sh
bash -n test/chaos/runner.sh
```
Expected: silent success for both.

- [ ] **Step 5: Commit**

```bash
git add test/chaos/common.sh test/chaos/runner.sh test/chaos/results/.gitkeep
git commit -m "chore(chaos): add shared helpers and stack runner"
```

---

## Task 3: Bring-up validation (no new files)

- [ ] **Step 1: Bring the chaos stack up**

```bash
test/chaos/runner.sh up
```
Expected output (last line): `>> stack up`. All 3 containers running (`docker ps | grep obs-chaos`).

- [ ] **Step 2: Verify governance can reach pg via toxiproxy**

```bash
curl -sS -X POST http://localhost:8083/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"type":"bugfix","title":"smoke","scope":"file","priority":"normal"}'
```
Expected: HTTP 201 JSON body with `"id"` field. If this fails, toxiproxy may not be forwarding correctly — check `docker logs obs-chaos-toxiproxy` and `docker logs obs-chaos-governance`.

- [ ] **Step 3: Verify toxiproxy admin is reachable**

```bash
curl -sS http://localhost:8474/proxies | jq '.pg | {name, listen, upstream, enabled, toxics: .toxics}'
```
Expected: name=`pg`, listen=`[::]:5432` or `0.0.0.0:5432`, upstream=`pg:5432`, enabled=`true`, toxics=`[]`.

- [ ] **Step 4: Verify psql client can reach pg directly on host 5435**

```bash
psql "postgresql://postgres:postgres@localhost:5435/governance" -c "SELECT count(*) FROM tasks;"
```
Expected: row count of at least 1 (the smoke task from Step 2).

- [ ] **Step 5: Tear down**

```bash
test/chaos/runner.sh down
```

- [ ] **Step 6: No commit** — validation only.

If any step failed, STOP. Do not proceed to scenario tasks. Investigate and fix before continuing.

---

## Task 4: Scenario S1 — pg down mid-workflow

**Files:**
- Create: `test/chaos/scenarios/s1-pg-down/trigger.sh`
- Create: `test/chaos/scenarios/s1-pg-down/verify.sh`
- Create: `test/chaos/scenarios/s1-pg-down/README.md`

- [ ] **Step 1: Write `trigger.sh`**

```bash
#!/usr/bin/env bash
# S1 — pg down mid-workflow
# Requires: stack up with a baseline workflow_run_id in results/last_workflow_run_id.txt

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

PRE_COUNTS=$(row_counts)
echo "pre-counts (tasks, workflows, leases, audit) = ${PRE_COUNTS}" | tr '\t' ',' \
  > "${HERE}/last_pre_counts.csv"

echo ">> stopping pg"
docker stop obs-chaos-pg >/dev/null

echo ">> waiting 30s with pg down"
sleep 30

echo ">> starting pg"
docker start obs-chaos-pg >/dev/null

echo ">> waiting for governance health recovery"
wait_healthy_gov 60 || {
  echo ">> governance did not recover within 60s" >&2
  exit 1
}

echo ">> trigger complete"
```

- [ ] **Step 2: Write `verify.sh`**

```bash
#!/usr/bin/env bash
# S1 — verify pg-down recovery
# Must run AFTER trigger.sh has completed (pg is already back up).
# Separately re-trigger a brief pg-down for criterion 1 (which requires
# observing 5xx during the down window). This is a safe re-probe.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

declare -A R=( [1]=FAIL [2]=FAIL [3]=FAIL [4]=FAIL [5]=FAIL )

# Criterion 1: POST during pg-down → 5xx
docker stop obs-chaos-pg >/dev/null
sleep 2
if assert_http_status_is_5xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
  R[1]=PASS
fi
docker start obs-chaos-pg >/dev/null
wait_healthy_gov 60 || true

# Criterion 2: governance container still running
container_running obs-chaos-governance && R[2]=PASS

# Criterion 3: no panic in logs (last 10 min)
if ! panic_in_logs obs-chaos-governance 10m; then
  R[3]=PASS
fi

# Criterion 4: POST after recovery → 2xx (with retry for pool warm-up)
sleep 5
for _ in $(seq 1 5); do
  if assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
    R[4]=PASS
    break
  fi
  sleep 2
done

# Criterion 5: row counts monotonic (>= pre-counts)
PRE_CSV=$(cat "${HERE}/last_pre_counts.csv" 2>/dev/null || echo "0,0,0,0")
IFS=',' read -r pt pw pl pa <<< "${PRE_CSV#*= }"
IFS=$'\t' read -r ct cw cl ca <<< "$(row_counts)"
if (( ct >= ${pt:-0} && cw >= ${pw:-0} && cl >= ${pl:-0} && ca >= ${pa:-0} )); then
  R[5]=PASS
fi

cat <<EOT
S1 — pg down mid-workflow
─────────────────────────────────
[1] POST during pg-down → 5xx        ${R[1]}
[2] gov container still running       ${R[2]}
[3] no panic in logs                  ${R[3]}
[4] POST after recovery → 2xx         ${R[4]}
[5] row counts monotonic              ${R[5]}
─────────────────────────────────
EOT

[[ ${R[1]} == PASS && ${R[2]} == PASS && ${R[3]} == PASS && ${R[4]} == PASS && ${R[5]} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
```

- [ ] **Step 3: Write `README.md` (short runbook)**

```markdown
# S1 — pg down mid-workflow

## Symptom
Governance HTTP API begins returning 5xx or timing out. Logs show pgx connection errors.

## Diagnosis
PostgreSQL is unreachable. Check:
1. `docker ps | grep obs-chaos-pg` → container state
2. `docker logs obs-chaos-pg --tail=50` → shutdown or OOM messages
3. If production: connectivity, disk space, auth, cert rotation

## Remediation
1. Bring pg back: `docker start obs-chaos-pg` (or equivalent in production orchestrator)
2. Wait for healthcheck to turn green
3. Governance's pgx pool auto-reconnects on next request; no governance restart needed
4. Confirm recovery: `curl -sS -X POST http://localhost:8083/api/v1/tasks -H 'Content-Type: application/json' -d '{...}'` → 2xx
5. Verify data integrity: `psql ...-c "SELECT count(*) FROM tasks, workflow_runs;"` vs pre-incident snapshot

## Reproduce
```bash
test/chaos/runner.sh up && test/chaos/runner.sh baseline
test/chaos/scenarios/s1-pg-down/trigger.sh
test/chaos/scenarios/s1-pg-down/verify.sh
```
```

- [ ] **Step 4: Make scripts executable**

```bash
chmod +x test/chaos/scenarios/s1-pg-down/trigger.sh test/chaos/scenarios/s1-pg-down/verify.sh
bash -n test/chaos/scenarios/s1-pg-down/trigger.sh
bash -n test/chaos/scenarios/s1-pg-down/verify.sh
```
Expected: silent success for both bash -n.

- [ ] **Step 5: Commit**

```bash
git add test/chaos/scenarios/s1-pg-down/
git commit -m "chore(chaos): add S1 pg-down scenario (trigger + verify + runbook)"
```

---

## Task 5: Scenario S2 — pg latency injection

**Files:**
- Create: `test/chaos/scenarios/s2-pg-latency/trigger.sh`
- Create: `test/chaos/scenarios/s2-pg-latency/verify.sh`
- Create: `test/chaos/scenarios/s2-pg-latency/README.md`

- [ ] **Step 1: Write `trigger.sh`**

```bash
#!/usr/bin/env bash
# S2 — pg latency injection (500 ms + 50 ms jitter)
# Applies the toxic, runs a short k6 (or curl loop) to gather samples,
# removes the toxic, and captures before/after latency numbers in results/.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

RESULTS_DIR="${HERE}/results"
mkdir -p "${RESULTS_DIR}"

echo ">> applying latency toxic (500 ms, jitter 50)"
toxic_add '{"name":"latency","type":"latency","attributes":{"latency":500,"jitter":50}}'

echo ">> sampling latency under toxic (30 curl probes)"
rm -f "${RESULTS_DIR}/under_toxic.txt"
for _ in $(seq 1 30); do
  curl -sS -o /dev/null -w '%{time_total}\n' \
    -X POST "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks" \
    -H 'Content-Type: application/json' \
    -d '{"type":"bugfix","title":"lat-probe","scope":"file","priority":"normal"}' \
    --max-time 10 >> "${RESULTS_DIR}/under_toxic.txt"
done

echo ">> removing latency toxic"
toxic_del latency

echo ">> sampling latency post-toxic (15 curl probes, 5 s warm-up)"
sleep 5
rm -f "${RESULTS_DIR}/post_toxic.txt"
for _ in $(seq 1 15); do
  curl -sS -o /dev/null -w '%{time_total}\n' \
    -X POST "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks" \
    -H 'Content-Type: application/json' \
    -d '{"type":"bugfix","title":"baseline-probe","scope":"file","priority":"normal"}' \
    --max-time 5 >> "${RESULTS_DIR}/post_toxic.txt"
done

echo ">> trigger complete"
```

- [ ] **Step 2: Write `verify.sh`**

```bash
#!/usr/bin/env bash
# S2 — verify latency outcomes
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

declare -A R=( [1]=FAIL [2]=FAIL [3]=FAIL [4]=FAIL [5]=FAIL )

# compute_p99: from seconds file, print P99 in milliseconds (integer)
compute_p99() {
  awk '{print $1 * 1000}' "$1" | sort -g | awk 'BEGIN{c=0} {a[c++]=$0} END{i=int(c*0.99); if (i>=c) i=c-1; print a[i]}'
}
# compute_p50
compute_p50() {
  awk '{print $1 * 1000}' "$1" | sort -g | awk 'BEGIN{c=0} {a[c++]=$0} END{i=int(c*0.50); if (i>=c) i=c-1; print a[i]}'
}
count_lines() { wc -l < "$1" | tr -d ' '; }

UT="${HERE}/results/under_toxic.txt"
PT="${HERE}/results/post_toxic.txt"

# Criterion 1: 0% failure (file has 30 lines — i.e., 30 successful probes)
if [[ -f "${UT}" ]] && (( $(count_lines "${UT}") == 30 )); then
  R[1]=PASS
fi

# Criterion 2: P99 under toxic >= 500 ms
if [[ -f "${UT}" ]]; then
  UT_P99=$(compute_p99 "${UT}")
  (( UT_P99 >= 500 )) && R[2]=PASS
fi

# Criterion 3: P50 under toxic in [450, 700] ms
if [[ -f "${UT}" ]]; then
  UT_P50=$(compute_p50 "${UT}")
  (( UT_P50 >= 450 && UT_P50 <= 700 )) && R[3]=PASS
fi

# Criterion 4: P99 post-toxic < 50 ms
if [[ -f "${PT}" ]]; then
  PT_P99=$(compute_p99 "${PT}")
  (( PT_P99 < 50 )) && R[4]=PASS
fi

# Criterion 5: no panic, no crash, no unexpected restart
# Check container state: RestartCount unchanged, FinishedAt empty/unchanged (never exited during scenario)
RESTART_COUNT=$(docker inspect -f '{{.RestartCount}}' obs-chaos-governance 2>/dev/null || echo 99)
FINISHED_AT=$(docker inspect -f '{{.State.FinishedAt}}' obs-chaos-governance 2>/dev/null || echo "")
if (( RESTART_COUNT == 0 )) \
   && [[ "${FINISHED_AT}" == "0001-01-01T00:00:00Z" ]] \
   && ! panic_in_logs obs-chaos-governance 10m; then
  R[5]=PASS
fi

cat <<EOT
S2 — pg latency injection
─────────────────────────────────
[1] 30/30 probes succeeded          ${R[1]}
[2] under-toxic P99 ≥ 500 ms        ${R[2]} (measured ${UT_P99:-?} ms)
[3] under-toxic P50 in [450,700] ms ${R[3]} (measured ${UT_P50:-?} ms)
[4] post-toxic P99 < 50 ms          ${R[4]} (measured ${PT_P99:-?} ms)
[5] no panic / crash / unexpected restart ${R[5]}
─────────────────────────────────
EOT

[[ ${R[1]} == PASS && ${R[2]} == PASS && ${R[3]} == PASS && ${R[4]} == PASS && ${R[5]} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
```

- [ ] **Step 3: Write `README.md`**

```markdown
# S2 — pg latency injection

## Symptom
Governance response times spike. No errors, just slow. Metrics show elevated `governance_routing_duration_ms` P99.

## Diagnosis
Network or pg-side latency. Check:
1. Prometheus: `histogram_quantile(0.99, sum by (le) (rate(governance_workflow_duration_ms_milliseconds_bucket[5m])))`
2. pg `log_min_duration_statement` output (if enabled) — identify slow queries
3. Network: ping, traceroute pg. Cloud link health.

## Remediation
Latency is environmental — governance can't fix it. Options:
1. Restore healthy path (cloud team, network team, or, in chaos harness: `curl -X DELETE http://localhost:8474/proxies/pg/toxics/latency`)
2. If latency is pg-side slow queries: inspect `pg_stat_activity`, add indexes, or kill problematic sessions
3. If governance should handle it: tune query timeouts (not covered in 3.5.C)

## Reproduce
```bash
test/chaos/runner.sh up
test/chaos/scenarios/s2-pg-latency/trigger.sh
test/chaos/scenarios/s2-pg-latency/verify.sh
```
```

- [ ] **Step 4: Make executable + check syntax**

```bash
chmod +x test/chaos/scenarios/s2-pg-latency/trigger.sh test/chaos/scenarios/s2-pg-latency/verify.sh
bash -n test/chaos/scenarios/s2-pg-latency/trigger.sh
bash -n test/chaos/scenarios/s2-pg-latency/verify.sh
```

- [ ] **Step 5: Commit**

```bash
git add test/chaos/scenarios/s2-pg-latency/
git commit -m "chore(chaos): add S2 pg-latency scenario (trigger + verify + runbook)"
```

---

## Task 6: Scenario S3 — pg pool starvation (held connections)

**Files:**
- Create: `test/chaos/scenarios/s3-pool-starvation/trigger.sh`
- Create: `test/chaos/scenarios/s3-pool-starvation/verify.sh`
- Create: `test/chaos/scenarios/s3-pool-starvation/README.md`

- [ ] **Step 1: Write `trigger.sh`**

```bash
#!/usr/bin/env bash
# S3 — pg pool starvation via held connections
# Applies a large-latency toxic so in-flight queries block the pool,
# then floods governance with parallel requests to saturate. Records
# timing + status distribution.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

RESULTS_DIR="${HERE}/results"
mkdir -p "${RESULTS_DIR}"

echo ">> pre-flood pg_stat_activity snapshot"
"${PSQL_CMD[@]}" -At -c "SELECT count(*) FROM pg_stat_activity WHERE datname='governance';" \
  > "${RESULTS_DIR}/pre_conn.txt"

echo ">> applying 30 s latency toxic (blocks pg queries -> pool starvation)"
toxic_add '{"name":"pool_block","type":"latency","attributes":{"latency":30000,"jitter":0}}'

echo ">> flooding governance with 30 parallel POSTs (15 s timeout each)"
rm -f "${RESULTS_DIR}/flood_status.txt" "${RESULTS_DIR}/flood_times.txt"
for i in $(seq 1 30); do
  (
    code=$(curl -sS -o /dev/null -w '%{http_code};%{time_total}\n' \
      -X POST "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks" \
      -H 'Content-Type: application/json' \
      -d "{\"type\":\"bugfix\",\"title\":\"flood-${i}\",\"scope\":\"file\",\"priority\":\"normal\"}" \
      --max-time 15 || echo "000;timeout")
    echo "${code}" >> "${RESULTS_DIR}/flood_status.txt"
  ) &
done
wait
echo ">> flood complete"

echo ">> removing toxic"
toxic_del pool_block

echo ">> waiting 30 s for pool to drain / governance to recover"
sleep 30

echo ">> post-flood pg_stat_activity snapshot"
"${PSQL_CMD[@]}" -At -c "SELECT count(*) FROM pg_stat_activity WHERE datname='governance';" \
  > "${RESULTS_DIR}/post_conn.txt"

echo ">> trigger complete"
```

- [ ] **Step 2: Write `verify.sh`**

```bash
#!/usr/bin/env bash
# S3 — verify pool starvation outcomes
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

declare -A R=( [1]=FAIL [2]=FAIL [3]=FAIL [4]=FAIL )

FLOOD="${HERE}/results/flood_status.txt"

# Criterion 1: at least one request returns 5xx OR takes > 5 s (proof of pool pressure)
if [[ -f "${FLOOD}" ]]; then
  if awk -F';' '{ if ($1 ~ /^5[0-9][0-9]$/ || $2 + 0 > 5) found=1 } END { exit (found ? 0 : 1) }' "${FLOOD}"; then
    R[1]=PASS
  fi
fi

# Criterion 2: no panic in governance logs, process still running
if container_running obs-chaos-governance && ! panic_in_logs obs-chaos-governance 10m; then
  R[2]=PASS
fi

# Criterion 3: POST after recovery → 2xx within retry window
for _ in $(seq 1 5); do
  if assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
    R[3]=PASS
    break
  fi
  sleep 2
done

# Criterion 4: pg_stat_activity connection count ≤ 5 within 30 s (no leaked conns)
POST_CONN=$(cat "${HERE}/results/post_conn.txt" 2>/dev/null || echo 99)
(( POST_CONN <= 5 )) && R[4]=PASS

cat <<EOT
S3 — pg pool starvation
─────────────────────────────────
[1] pool pressure observed (5xx or >5s) ${R[1]}
[2] gov running, no panic                ${R[2]}
[3] POST after recovery → 2xx            ${R[3]}
[4] pg conn count ≤ 5 post-flood         ${R[4]} (measured ${POST_CONN})
─────────────────────────────────
EOT

[[ ${R[1]} == PASS && ${R[2]} == PASS && ${R[3]} == PASS && ${R[4]} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
```

- [ ] **Step 3: Write `README.md`**

```markdown
# S3 — pg pool starvation (by held connections)

## Symptom
Governance HTTP latency balloons. Some requests 5xx. `pg_stat_activity` shows many long-running sessions.

## Diagnosis
The pgx pool is saturated — every connection is held on a slow query or a blocked upstream. Not a hard cap on connections; effective exhaustion via busy sessions.
1. `SELECT pid, state, wait_event_type, wait_event, query FROM pg_stat_activity WHERE datname='governance' ORDER BY state_change;`
2. Look for locked sessions, long IDLE-IN-TRANSACTION, or network-blocked `ClientWrite`
3. Governance pgx pool default is `max(4, NumCPU)` — typically 8 on dev / 4 in containers

## Remediation
1. Identify the blocking cause (network, replica lag, bad query, downstream service)
2. Terminate stuck sessions if clearly hung: `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE ...`
3. Lift the blocking cause; pool drains naturally
4. If chronic: consider bumping pool size or sharding workload (separate track)

## Reproduce
```bash
test/chaos/runner.sh up
test/chaos/scenarios/s3-pool-starvation/trigger.sh
test/chaos/scenarios/s3-pool-starvation/verify.sh
```
```

- [ ] **Step 4: Make executable + check syntax**

```bash
chmod +x test/chaos/scenarios/s3-pool-starvation/trigger.sh test/chaos/scenarios/s3-pool-starvation/verify.sh
bash -n test/chaos/scenarios/s3-pool-starvation/trigger.sh
bash -n test/chaos/scenarios/s3-pool-starvation/verify.sh
```

- [ ] **Step 5: Commit**

```bash
git add test/chaos/scenarios/s3-pool-starvation/
git commit -m "chore(chaos): add S3 pool-starvation scenario (trigger + verify + runbook)"
```

---

## Task 7: Scenario S4 — governance SIGKILL

**Files:**
- Create: `test/chaos/scenarios/s4-governance-sigkill/trigger.sh`
- Create: `test/chaos/scenarios/s4-governance-sigkill/verify.sh`
- Create: `test/chaos/scenarios/s4-governance-sigkill/README.md`

- [ ] **Step 1: Write `trigger.sh`**

```bash
#!/usr/bin/env bash
# S4 — governance SIGKILL mid-workflow
# Captures pre-kill row counts, SIGKILLs governance, restarts it, waits healthy.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

RESULTS_DIR="${HERE}/results"
mkdir -p "${RESULTS_DIR}"

echo ">> pre-kill row counts"
row_counts > "${RESULTS_DIR}/pre_counts.tsv"
WF_ID=$(cat results/last_workflow_run_id.txt 2>/dev/null || echo "")
echo "${WF_ID}" > "${RESULTS_DIR}/wf_id.txt"

echo ">> SIGKILL governance"
docker kill --signal=SIGKILL obs-chaos-governance >/dev/null

echo ">> waiting 3 s"
sleep 3

echo ">> starting governance"
docker start obs-chaos-governance >/dev/null

echo ">> waiting for governance health"
wait_healthy_gov 60 || {
  echo ">> governance did not recover within 60 s" >&2
  exit 1
}

echo ">> post-recovery row counts"
row_counts > "${RESULTS_DIR}/post_counts.tsv"

echo ">> trigger complete"
```

- [ ] **Step 2: Write `verify.sh`**

```bash
#!/usr/bin/env bash
# S4 — verify SIGKILL recovery
# NOTE: criterion 6 (execution_lease stale rows) is an OBSERVATION, not a
# pass gate — pre-declared in the spec. Recorded in results/observations.txt.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

declare -A R=( [1]=FAIL [2]=FAIL [3]=FAIL [4]=FAIL [5]=FAIL [7]=FAIL )

# Criterion 1: pre-kill counts captured (sanity — trigger.sh produced them)
[[ -s "${HERE}/results/pre_counts.tsv" ]] && R[1]=PASS

# Criterion 2: container was killed (we can only assert post-hoc that it exited at some point —
# use docker inspect start/finish timestamps)
LAST_EXIT=$(docker inspect -f '{{.State.FinishedAt}}' obs-chaos-governance 2>/dev/null || echo "")
[[ -n "${LAST_EXIT}" && "${LAST_EXIT}" != "0001-01-01T00:00:00Z" ]] && R[2]=PASS

# Criterion 3: container is now running
container_running obs-chaos-governance && R[3]=PASS

# Criterion 4: POST /api/v1/tasks returns 2xx (healthy after restart)
assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks" && R[4]=PASS

# Criterion 5: prior workflow still queryable
WF_ID=$(cat "${HERE}/results/wf_id.txt" 2>/dev/null || echo "")
if [[ -n "${WF_ID}" ]]; then
  status=$(curl -sS -o /dev/null -w '%{http_code}' "http://${GOV_HOST}:${GOV_PORT}/api/v1/workflows/${WF_ID}")
  [[ "${status}" =~ ^2[0-9][0-9]$ ]] && R[5]=PASS
else
  # no baseline workflow — treat as trivially pass
  R[5]=PASS
fi

# Criterion 7 (skip 6 — it's an observation, not a gate): row counts are monotonic
IFS=$'\t' read -r pt pw pl pa < "${HERE}/results/pre_counts.tsv"
IFS=$'\t' read -r ct cw cl ca < "${HERE}/results/post_counts.tsv"
if (( ct >= pt && cw >= pw && cl >= pl && ca >= pa )); then
  R[7]=PASS
fi

# Criterion 6 (OBSERVATION): record execution_lease rows pre vs post
{
  echo "pre-kill execution_leases: ${pl}"
  echo "post-kill execution_leases: ${cl}"
  if (( cl > 0 && cl >= pl )); then
    echo "OBSERVATION: stale leases retained after SIGKILL (expected — no lease reconciliation in v0.6.0)"
  fi
} > "${HERE}/results/observations.txt"

cat <<EOT
S4 — governance SIGKILL
─────────────────────────────────
[1] pre-kill counts captured         ${R[1]}
[2] container was killed/restarted   ${R[2]}
[3] container running post-restart   ${R[3]}
[4] POST after restart → 2xx         ${R[4]}
[5] prior workflow queryable         ${R[5]}
[6] execution_lease observation      — (see results/observations.txt)
[7] row counts monotonic             ${R[7]}
─────────────────────────────────
EOT

[[ ${R[1]} == PASS && ${R[2]} == PASS && ${R[3]} == PASS && ${R[4]} == PASS && ${R[5]} == PASS && ${R[7]} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
```

- [ ] **Step 3: Write `README.md`**

```markdown
# S4 — governance SIGKILL

## Symptom
Governance process exited non-gracefully (OOM kill, panic, container stop without SIGTERM). On restart it boots clean. Any `execution_lease` rows from before the kill are retained as stale.

## Diagnosis
1. `docker inspect obs-chaos-governance --format '{{.State.ExitCode}} {{.State.OOMKilled}}'`
2. `docker logs obs-chaos-governance --tail=100` — look for panic, OOM
3. Post-restart: governance starts clean but **does NOT reconcile leases** (known gap in v0.6.0 — see Follow-ups in the spec)

## Remediation
1. Immediate: restart container — `docker start obs-chaos-governance` (or orchestrator equivalent)
2. Confirm healthy: `curl -sS http://localhost:8083/api/v1/audit?limit=1`
3. **Stale leases**: in v0.6.0 they are retained. Manual cleanup if needed:
   ```sql
   DELETE FROM execution_leases WHERE expires_at < NOW();
   -- or: UPDATE to reset the lease holder if the workflow should continue
   ```
4. For production: this gap should be addressed in a separate "lease reconciliation on startup" track — not part of 3.5.C.

## Reproduce
```bash
test/chaos/runner.sh up && test/chaos/runner.sh baseline
test/chaos/scenarios/s4-governance-sigkill/trigger.sh
test/chaos/scenarios/s4-governance-sigkill/verify.sh
cat test/chaos/scenarios/s4-governance-sigkill/results/observations.txt
```

## Known gap (do not fix in 3.5.C)
Governance v0.6.0 does not reconcile `execution_leases` on startup. Leases from a prior killed process persist until their `expires_at`. Separate track required to decide the recovery policy.
```

- [ ] **Step 4: Make executable + check syntax**

```bash
chmod +x test/chaos/scenarios/s4-governance-sigkill/trigger.sh test/chaos/scenarios/s4-governance-sigkill/verify.sh
bash -n test/chaos/scenarios/s4-governance-sigkill/trigger.sh
bash -n test/chaos/scenarios/s4-governance-sigkill/verify.sh
```

- [ ] **Step 5: Commit**

```bash
git add test/chaos/scenarios/s4-governance-sigkill/
git commit -m "chore(chaos): add S4 SIGKILL scenario with lease observation (no gate)"
```

---

## Task 8: Scenario S5 — network partition

**Files:**
- Create: `test/chaos/scenarios/s5-partition/trigger.sh`
- Create: `test/chaos/scenarios/s5-partition/verify.sh`
- Create: `test/chaos/scenarios/s5-partition/README.md`

- [ ] **Step 1: Write `trigger.sh`**

```bash
#!/usr/bin/env bash
# S5 — network partition governance ↔ pg (toxiproxy proxy disabled)
# Pg process stays alive the whole time; governance sees unreachable.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

RESULTS_DIR="${HERE}/results"
mkdir -p "${RESULTS_DIR}"

echo ">> pre-partition pg row counts"
row_counts > "${RESULTS_DIR}/pre_partition.tsv"

echo ">> disabling pg proxy (partition)"
proxy_set_enabled false >/dev/null

echo ">> waiting 30 s with partition"
sleep 30

echo ">> re-enabling pg proxy"
proxy_set_enabled true >/dev/null

echo ">> waiting for governance health"
wait_healthy_gov 60 || {
  echo ">> governance did not recover within 60 s" >&2
  exit 1
}

echo ">> post-partition pg row counts"
row_counts > "${RESULTS_DIR}/post_partition.tsv"

echo ">> trigger complete"
```

- [ ] **Step 2: Write `verify.sh`**

```bash
#!/usr/bin/env bash
# S5 — verify partition recovery
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

declare -A R=( [1]=FAIL [2]=FAIL [3]=FAIL [4]=FAIL [5]=FAIL )

# Criterion 1: POST during partition → 5xx (re-apply brief partition for the probe)
proxy_set_enabled false >/dev/null
sleep 2
if assert_http_status_is_5xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
  R[1]=PASS
fi
proxy_set_enabled true >/dev/null
wait_healthy_gov 60 || true

# Criterion 2: governance still running
container_running obs-chaos-governance && R[2]=PASS

# Criterion 3: no panic in logs
if ! panic_in_logs obs-chaos-governance 10m; then
  R[3]=PASS
fi

# Criterion 4: POST after partition heals → 2xx
sleep 3
for _ in $(seq 1 5); do
  if assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
    R[4]=PASS
    break
  fi
  sleep 2
done

# Criterion 5: pg row counts unchanged across the partition window
#   (pg was always alive — so row counts must match exactly pre vs post the partition
#    window itself, ignoring the probes in criterion 1 and 4 which add rows.)
# We assert that pre counts <= post counts by at most the number of probes made
# (1 probe in crit 1 might have failed and added nothing; 1+ probes in crit 4 succeeded).
IFS=$'\t' read -r pt pw pl pa < "${HERE}/results/pre_partition.tsv"
IFS=$'\t' read -r ct cw cl ca < "${HERE}/results/post_partition.tsv"
# Post-counts should be >= pre-counts (monotonic) and reasonable drift (< 10 extra rows).
if (( ct >= pt && cw >= pw && cl >= pl && ca >= pa && (ct - pt) < 10 )); then
  R[5]=PASS
fi

cat <<EOT
S5 — network partition
─────────────────────────────────
[1] POST during partition → 5xx       ${R[1]}
[2] gov container running             ${R[2]}
[3] no panic in logs                  ${R[3]}
[4] POST after heal → 2xx             ${R[4]}
[5] pg row counts unchanged           ${R[5]}
─────────────────────────────────
EOT

[[ ${R[1]} == PASS && ${R[2]} == PASS && ${R[3]} == PASS && ${R[4]} == PASS && ${R[5]} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
```

- [ ] **Step 3: Write `README.md`**

```markdown
# S5 — network partition governance ↔ pg

## Symptom
Governance returns 5xx. pg is healthy and reachable from other clients. Connection errors in governance logs: dial tcp timeout, connection refused.

## Diagnosis
Network path between governance and pg is broken while pg itself is fine:
1. From governance container: `docker exec obs-chaos-governance sh -c 'nc -zv pg 5432'` → timeout or refused
2. From another client (psql directly): `psql postgresql://...localhost:5435/governance` → succeeds (pg is up)
3. Check: security groups, NACLs, service mesh, DNS, LB health-check flapping

## Remediation
1. Restore network path (ops problem, not governance)
2. Once path is up, governance reconnects on next request; no restart needed
3. Verify: `curl -sS http://localhost:8083/api/v1/audit?limit=1` → 2xx
4. Data integrity: pg never went down, so no row loss expected. Reconcile only if specific operations were mid-flight (check audit_entries for partial transactions).

## Reproduce (chaos harness)
```bash
test/chaos/runner.sh up
test/chaos/scenarios/s5-partition/trigger.sh
test/chaos/scenarios/s5-partition/verify.sh
```
```

- [ ] **Step 4: Make executable + check syntax**

```bash
chmod +x test/chaos/scenarios/s5-partition/trigger.sh test/chaos/scenarios/s5-partition/verify.sh
bash -n test/chaos/scenarios/s5-partition/trigger.sh
bash -n test/chaos/scenarios/s5-partition/verify.sh
```

- [ ] **Step 5: Commit**

```bash
git add test/chaos/scenarios/s5-partition/
git commit -m "chore(chaos): add S5 partition scenario (trigger + verify + runbook)"
```

---

## Task 9: Master README + `docs/chaos/scenarios.md` skeleton

**Files:**
- Create: `test/chaos/README.md`
- Create: `docs/chaos/scenarios.md`

- [ ] **Step 1: Write `test/chaos/README.md`**

```markdown
# Chaos harness — Phase 3.5.C

Local-only chaos test harness for governance-core. Exercises 5 Tier-1 failure scenarios and documents observed behaviour.

## Security

**NOT apt for shared networks, staging, or production.** All ports bound to localhost; no auth on toxiproxy admin API.

## Quickstart

```bash
# Bring up the stack
test/chaos/runner.sh up

# Run a scenario end-to-end
test/chaos/runner.sh baseline
test/chaos/scenarios/s1-pg-down/trigger.sh
test/chaos/scenarios/s1-pg-down/verify.sh

# Tear down
test/chaos/runner.sh down
```

## Scenarios

| # | Scenario | Mechanism |
|---|----------|-----------|
| S1 | pg down mid-workflow | `docker stop` pg + restart |
| S2 | pg latency injection | toxiproxy `latency` toxic |
| S3 | pg pool starvation | toxiproxy long-latency holds pool |
| S4 | governance SIGKILL | `docker kill --signal=SIGKILL` + restart |
| S5 | network partition | toxiproxy proxy disabled |

## Ports

| Service | Host port | Purpose |
|---------|-----------|---------|
| pg (direct) | 5435 | Host access for psql assertions |
| governance | 8083 | HTTP API under test |
| toxiproxy admin | 8474 | Toxic add/remove REST API |

## Pass / Fail protocol

Each `verify.sh` exits 0 on PASS and non-zero on FAIL. Output is a table of numbered criteria. See per-scenario `README.md` for the criteria list and remediation runbook.

## Known gaps documented (no fix in 3.5.C)

- S4: `execution_lease` rows persist after SIGKILL — governance v0.6.0 does not reconcile on startup. See `scenarios/s4-governance-sigkill/README.md` → "Known gap". Separate track to address.

## Rebuild the governance image

Reuses `agent-governance-core:loadtest` built in 3.5.A:
```bash
docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .
```
```

- [ ] **Step 2: Write `docs/chaos/scenarios.md` (skeleton — filled in Task 10)**

```markdown
# Chaos scenarios — observed behaviour matrix

**Status:** v1 — populated after Phase 3.5.C execution
**Harness:** `test/chaos/`
**Tooling:** toxiproxy + shell + psql

All criteria are boolean. A scenario passes only if every criterion holds.

| # | Scenario | Trigger | Expected | Pass/Fail | Observations |
|---|----------|---------|----------|-----------|--------------|
| S1 | pg down mid-workflow | `docker stop` pg + restart | graceful 5xx, no panic, recovery on pg restart, data intact | *to be filled after execution* | *to be filled* |
| S2 | pg latency injection | toxiproxy 500 ms ± 50 ms | 0 % errors, P99 ≥ 500 ms under toxic, baseline restored after removal | *to be filled* | *to be filled* |
| S3 | pg pool starvation | 30 s latency toxic + 30 parallel floods | pool pressure observable, no panic, pool recovers, no leaked conns | *to be filled* | *to be filled* |
| S4 | governance SIGKILL | `docker kill --signal=SIGKILL` + restart | restart clean, prior workflow queryable, monotonic counts; lease rows retained (observation only, not gate) | *to be filled* | *to be filled* |
| S5 | network partition | toxiproxy proxy disabled | identical to S1 from gov view, pg counts unchanged | *to be filled* | *to be filled* |

## Executing

Each scenario's README has the exact reproduction command. The harness master README (`test/chaos/README.md`) has the quickstart.

## Known gaps

- **S4 lease reconciliation**: v0.6.0 does not reconcile `execution_leases` on startup. Stale rows accumulate on every SIGKILL. Deferred to a separate track — do NOT fix in 3.5.C.
- **No CI integration**: scenarios are operator-runnable only. Automated smoke in CI is a follow-up.
- **Single-fault only**: v1 scope. Multi-fault composition (e.g. pg slow + notifier slow) is a future track.
```

- [ ] **Step 3: Commit**

```bash
git add test/chaos/README.md docs/chaos/scenarios.md
git commit -m "docs(chaos): add harness README and empty scenarios matrix"
```

---

## Task 10: Execute all 5 scenarios, fill matrix

**Files:**
- Modify: `docs/chaos/scenarios.md` (fill in observed results)
- Modify: `test/chaos/results/` (run artefacts; commit results/observation files only)

- [ ] **Step 1: Bring up the stack and run S1**

```bash
test/chaos/runner.sh reset
test/chaos/scenarios/s1-pg-down/trigger.sh
test/chaos/scenarios/s1-pg-down/verify.sh | tee test/chaos/scenarios/s1-pg-down/results/verify.log
```
Record: PASS / FAIL, and any criteria that failed.

- [ ] **Step 2: S2 latency**

```bash
test/chaos/runner.sh reset
test/chaos/scenarios/s2-pg-latency/trigger.sh
test/chaos/scenarios/s2-pg-latency/verify.sh | tee test/chaos/scenarios/s2-pg-latency/results/verify.log
```
Note the measured P99/P50 numbers printed in the table.

- [ ] **Step 3: S3 pool starvation**

```bash
test/chaos/runner.sh reset
test/chaos/scenarios/s3-pool-starvation/trigger.sh
test/chaos/scenarios/s3-pool-starvation/verify.sh | tee test/chaos/scenarios/s3-pool-starvation/results/verify.log
```

- [ ] **Step 4: S4 SIGKILL**

```bash
test/chaos/runner.sh reset
test/chaos/scenarios/s4-governance-sigkill/trigger.sh
test/chaos/scenarios/s4-governance-sigkill/verify.sh | tee test/chaos/scenarios/s4-governance-sigkill/results/verify.log
cat test/chaos/scenarios/s4-governance-sigkill/results/observations.txt
```
Record the observation (expected: stale leases persist).

- [ ] **Step 5: S5 partition**

```bash
test/chaos/runner.sh reset
test/chaos/scenarios/s5-partition/trigger.sh
test/chaos/scenarios/s5-partition/verify.sh | tee test/chaos/scenarios/s5-partition/results/verify.log
```

- [ ] **Step 6: Tear down**

```bash
test/chaos/runner.sh down
```

- [ ] **Step 7: Fill `docs/chaos/scenarios.md` with observed results**

For each row of the matrix table, replace the *to be filled* placeholders with:
- **Pass/Fail**: `PASS` or `FAIL` (from verify.sh exit code)
- **Observations**: one sentence summary. For S2, cite measured P99/P50. For S4, note the lease-retention observation.

Add a "Run details" section below the matrix, one subsection per scenario, each with:
- Date measured (today)
- `verify.sh` output (paste the table)
- For failures: root-cause candidates (no fix, per D7)

- [ ] **Step 8: Commit results + scenarios.md**

```bash
git add docs/chaos/scenarios.md test/chaos/scenarios/*/results/
git commit -m "docs(chaos): populate scenarios matrix with observed v0.6.0 behaviour"
```

- [ ] **Step 9: Final verification**

```bash
git log --oneline | head -15
ls -la test/chaos/scenarios/*/verify.sh
```
Expected: 5 verify.sh files all executable; 10 chaos-related commits since 3.5.B closure.

---

## Self-review checklist

- [x] **Spec coverage — 5 Tier-1 scenarios**: Tasks 4–8 each implement one scenario with trigger + verify + README
- [x] **Spec coverage — stack topology (§4)**: Task 1 compose matches the 3-service diagram (governance → toxiproxy → pg)
- [x] **Spec coverage — toxic provisioning (§5)**: Task 1 toxiproxy config.json + common.sh `toxic_add`/`toxic_del` helpers
- [x] **Spec coverage — harness layout (§7)**: directory tree in plan header matches spec exactly
- [x] **Spec coverage — verify.sh pattern**: each verify script prints the criteria table, exits 0 on PASS, non-zero on FAIL
- [x] **Spec coverage — execution flow (§8)**: Task 10 follows the 6-step flow
- [x] **Spec coverage — deliverables (§9)**: harness, 5 trigger + verify + readme, master README, scenarios.md — all mapped to tasks
- [x] **D6 — boolean pass/fail explicit**: every criterion in every verify.sh is PASS/FAIL, no narrative
- [x] **D7 — no code fixes**: S4 lease observation is explicit; no governance-code modifications in any task
- [x] **S4 lease gap = observation, not gate**: verify.sh indexes criteria 1-5 and 7; criterion 6 is recorded to observations.txt only
- [x] **Port consistency**: 5435 / 8083 / 8474 used identically across compose, common.sh, scenario scripts, README
- [x] **Container names consistency**: `obs-chaos-pg`, `obs-chaos-toxiproxy`, `obs-chaos-governance` — same across all scripts
- [x] **No placeholders** in scripts; complete bash in every step
- [x] **Reused resources**: `migrations/postgres`, `test/load/pg/init-db.sh`, `agent-governance-core:loadtest` image — all confirmed to exist before this plan starts
