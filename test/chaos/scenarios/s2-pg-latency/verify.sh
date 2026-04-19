#!/usr/bin/env bash
# S2 — verify latency outcomes
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

declare -A R=( [1]=FAIL [2]=FAIL [3]=FAIL [4]=FAIL [5]=FAIL )

# compute_p99: from seconds file, print P99 in milliseconds (integer)
compute_p99() {
  awk '{print $1 * 1000}' "$1" | sort -g | awk 'BEGIN{c=0} {a[c++]=$0} END{i=int(c*0.99); if (i>=c) i=c-1; print a[i]}'
}
# compute_p50
compute_p50() {
  awk '{print $1 * 1000}' "$1" | sort -g | awk 'BEGIN{c=0} {a[c++]=$0} END{i=int(c*0.50); if (i>=c) i=c-1; print a[i]}'
}
count_lines() { wc -l < "$1" | tr -d ' '; }

UT="${HERE}/results/under_toxic.txt"
PT="${HERE}/results/post_toxic.txt"

# Criterion 1: 0% failure (file has 30 lines — i.e., 30 successful probes)
if [[ -f "${UT}" ]] && (( $(count_lines "${UT}") == 30 )); then
  R[1]=PASS
fi

# Criterion 2: P99 under toxic >= 500 ms
if [[ -f "${UT}" ]]; then
  UT_P99=$(compute_p99 "${UT}")
  (( UT_P99 >= 500 )) && R[2]=PASS
fi

# Criterion 3: P50 under toxic in [450, 700] ms
if [[ -f "${UT}" ]]; then
  UT_P50=$(compute_p50 "${UT}")
  (( UT_P50 >= 450 && UT_P50 <= 700 )) && R[3]=PASS
fi

# Criterion 4: P99 post-toxic < 50 ms
if [[ -f "${PT}" ]]; then
  PT_P99=$(compute_p99 "${PT}")
  (( PT_P99 < 50 )) && R[4]=PASS
fi

# Criterion 5: no panic, no crash, no unexpected restart
# Check container state: RestartCount unchanged, FinishedAt empty/unchanged (never exited during scenario)
RESTART_COUNT=$(docker inspect -f '{{.RestartCount}}' obs-chaos-governance 2>/dev/null || echo 99)
FINISHED_AT=$(docker inspect -f '{{.State.FinishedAt}}' obs-chaos-governance 2>/dev/null || echo "")
if (( RESTART_COUNT == 0 )) \
   && [[ "${FINISHED_AT}" == "0001-01-01T00:00:00Z" ]] \
   && ! panic_in_logs obs-chaos-governance 10m; then
  R[5]=PASS
fi

cat <<EOT
S2 — pg latency injection
─────────────────────────────────
[1] 30/30 probes succeeded          ${R[1]}
[2] under-toxic P99 ≥ 500 ms        ${R[2]} (measured ${UT_P99:-?} ms)
[3] under-toxic P50 in [450,700] ms ${R[3]} (measured ${UT_P50:-?} ms)
[4] post-toxic P99 < 50 ms          ${R[4]} (measured ${PT_P99:-?} ms)
[5] no panic / crash / unexpected restart ${R[5]}
─────────────────────────────────
EOT

[[ ${R[1]} == PASS && ${R[2]} == PASS && ${R[3]} == PASS && ${R[4]} == PASS && ${R[5]} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
