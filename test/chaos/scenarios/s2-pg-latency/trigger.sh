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
