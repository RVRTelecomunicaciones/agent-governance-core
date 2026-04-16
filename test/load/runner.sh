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
