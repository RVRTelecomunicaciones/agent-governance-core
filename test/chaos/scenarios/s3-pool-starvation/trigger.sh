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
