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
