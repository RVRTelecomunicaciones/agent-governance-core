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
