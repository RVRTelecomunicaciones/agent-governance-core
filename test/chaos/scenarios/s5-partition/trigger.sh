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
