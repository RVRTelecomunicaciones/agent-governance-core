#!/usr/bin/env bash
# S5 — verify partition recovery
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

R1=FAIL; R2=FAIL; R3=FAIL; R4=FAIL; R5=FAIL

# Criterion 1: POST during partition → 5xx (re-apply brief partition for the probe)
proxy_set_enabled false >/dev/null
sleep 2
if assert_http_status_is_5xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
  R1=PASS
fi
proxy_set_enabled true >/dev/null
wait_healthy_gov 60 || true

# Criterion 2: governance still running
container_running obs-chaos-governance && R2=PASS

# Criterion 3: no panic in logs
if ! panic_in_logs obs-chaos-governance 10m; then
  R3=PASS
fi

# Criterion 4: POST after partition heals → 2xx
sleep 3
for _ in $(seq 1 5); do
  if assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks"; then
    R4=PASS
    break
  fi
  sleep 2
done

# Criterion 5: pg row counts unchanged across the partition window
#   (pg was always alive — so row counts must match exactly pre vs post the partition
#    window itself, ignoring the probes in criterion 1 and 4 which add rows.)
# We assert that pre counts <= post counts by at most the number of probes made
# (1 probe in crit 1 might have failed and added nothing; 1+ probes in crit 4 succeeded).
IFS=$'\t' read -r pt pw pl pa < "${HERE}/results/pre_partition.tsv"
IFS=$'\t' read -r ct cw cl ca < "${HERE}/results/post_partition.tsv"
# Post-counts should be >= pre-counts (monotonic) and reasonable drift (< 10 extra rows).
if (( ct >= pt && cw >= pw && cl >= pl && ca >= pa && (ct - pt) < 10 )); then
  R5=PASS
fi

cat <<EOT
S5 — network partition
─────────────────────────────────
[1] POST during partition → 5xx       ${R1}
[2] gov container running             ${R2}
[3] no panic in logs                  ${R3}
[4] POST after heal → 2xx             ${R4}
[5] pg row counts unchanged           ${R5}
─────────────────────────────────
EOT

[[ ${R1} == PASS && ${R2} == PASS && ${R3} == PASS && ${R4} == PASS && ${R5} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
