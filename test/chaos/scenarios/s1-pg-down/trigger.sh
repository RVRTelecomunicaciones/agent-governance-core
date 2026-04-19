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
