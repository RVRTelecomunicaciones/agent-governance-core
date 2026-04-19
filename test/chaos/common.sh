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
