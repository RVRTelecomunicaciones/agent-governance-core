#!/usr/bin/env bash
# S3 — verify pool starvation outcomes
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

R1=FAIL; R2=FAIL; R3=FAIL; R4=FAIL

FLOOD="${HERE}/results/flood_status.txt"

# Criterion 1: at least one request returns 5xx OR takes > 5 s (proof of pool pressure)
if [[ -f "${FLOOD}" ]]; then
  if awk -F';' '{ if ($1 ~ /^5[0-9][0-9]$/ || $2 + 0 > 5) found=1 } END { exit (found ? 0 : 1) }' "${FLOOD}"; then
    R1=PASS
  fi
fi

# Criterion 2: no panic in governance logs, process still running
if container_running obs-chaos-governance && ! panic_in_logs obs-chaos-governance 10m; then
  R2=PASS
fi

# Criterion 3: POST after recovery → 2xx within retry window
for _ in $(seq 1 5); do
  if assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
    R3=PASS
    break
  fi
  sleep 2
done

# Criterion 4: no connection leak — post-flood conn count within pool-size bounds
# pgxpool default is max(4, NumCPU). We allow up to 20 as a generous ceiling.
# The real assertion is "not significantly above the pre-flood baseline".
PRE_CONN=$(cat "${HERE}/results/pre_conn.txt" 2>/dev/null || echo 0)
POST_CONN=$(cat "${HERE}/results/post_conn.txt" 2>/dev/null || echo 99)
POOL_CEILING=20
if (( POST_CONN <= POOL_CEILING )) && (( POST_CONN <= PRE_CONN + 15 )); then
  R4=PASS
fi

cat <<EOT
S3 — pg pool starvation
─────────────────────────────────
[1] pool pressure observed (5xx or >5s) ${R1}
[2] gov running, no panic                ${R2}
[3] POST after recovery → 2xx            ${R3}
[4] pg conn count within pool bounds     ${R4} (pre=${PRE_CONN} post=${POST_CONN} ceiling=${POOL_CEILING})
─────────────────────────────────
EOT

[[ ${R1} == PASS && ${R2} == PASS && ${R3} == PASS && ${R4} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
