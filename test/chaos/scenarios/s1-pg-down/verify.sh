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
