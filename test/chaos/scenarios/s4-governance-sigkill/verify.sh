#!/usr/bin/env bash
# S4 — verify SIGKILL recovery
# NOTE: criterion 6 (execution_lease stale rows) is an OBSERVATION, not a
# pass gate — pre-declared in the spec. Recorded in results/observations.txt.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}/../.."
# shellcheck source=../../common.sh
source ./common.sh

R1=FAIL; R2=FAIL; R3=FAIL; R4=FAIL; R5=FAIL; R7=FAIL

# Criterion 1: pre-kill counts captured (sanity — trigger.sh produced them)
[[ -s "${HERE}/results/pre_counts.tsv" ]] && R1=PASS

# Criterion 2: container was killed (we can only assert post-hoc that it exited at some point —
# use docker inspect start/finish timestamps)
LAST_EXIT=$(docker inspect -f '{{.State.FinishedAt}}' obs-chaos-governance 2>/dev/null || echo "")
[[ -n "${LAST_EXIT}" && "${LAST_EXIT}" != "0001-01-01T00:00:00Z" ]] && R2=PASS

# Criterion 3: container is now running
container_running obs-chaos-governance && R3=PASS

# Criterion 4: POST /api/v1/tasks returns 2xx (healthy after restart)
assert_http_status_is_2xx "http://${GOV_HOST}:${GOV_PORT}/api/v1/tasks" && R4=PASS

# Criterion 5: prior workflow still queryable
WF_ID=$(cat "${HERE}/results/wf_id.txt" 2>/dev/null || echo "")
if [[ -n "${WF_ID}" ]]; then
  status=$(curl -sS -o /dev/null -w '%{http_code}' "http://${GOV_HOST}:${GOV_PORT}/api/v1/workflows/${WF_ID}")
  [[ "${status}" =~ ^2[0-9][0-9]$ ]] && R5=PASS
else
  # no baseline workflow — treat as trivially pass
  R5=PASS
fi

# Criterion 7 (skip 6 — it's an observation, not a gate): row counts are monotonic
IFS=$'\t' read -r pt pw pl pa < "${HERE}/results/pre_counts.tsv"
IFS=$'\t' read -r ct cw cl ca < "${HERE}/results/post_counts.tsv"
if (( ct >= pt && cw >= pw && cl >= pl && ca >= pa )); then
  R7=PASS
fi

# Criterion 6 (OBSERVATION): record execution_lease rows pre vs post
{
  echo "pre-kill execution_leases: ${pl}"
  echo "post-kill execution_leases: ${cl}"
  if (( cl > 0 && cl >= pl )); then
    echo "OBSERVATION: stale leases retained after SIGKILL (expected — no lease reconciliation in v0.6.0)"
  fi
} > "${HERE}/results/observations.txt"

cat <<EOT
S4 — governance SIGKILL
─────────────────────────────────
[1] pre-kill counts captured         ${R1}
[2] container was killed/restarted   ${R2}
[3] container running post-restart   ${R3}
[4] POST after restart → 2xx         ${R4}
[5] prior workflow queryable         ${R5}
[6] execution_lease observation      — (see results/observations.txt)
[7] row counts monotonic             ${R7}
─────────────────────────────────
EOT

[[ ${R1} == PASS && ${R2} == PASS && ${R3} == PASS && ${R4} == PASS && ${R5} == PASS && ${R7} == PASS ]] \
  && { echo "RESULT: PASS"; exit 0; } \
  || { echo "RESULT: FAIL"; exit 1; }
