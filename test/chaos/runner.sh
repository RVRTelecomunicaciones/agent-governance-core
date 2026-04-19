#!/usr/bin/env bash
# Usage:
#   ./runner.sh up            # bring stack up, wait healthy
#   ./runner.sh baseline      # submit one happy-path workflow to running state
#   ./runner.sh down          # tear stack down
#   ./runner.sh reset         # down + up + baseline — clean slate for a scenario
#
# Individual scenarios handle their own trigger + verify after the runner
# has prepared the stack.

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}"
# shellcheck source=common.sh
source ./common.sh

cmd="${1:-up}"

case "${cmd}" in
  up)
    docker compose up -d
    wait_healthy_toxi 30
    wait_healthy_gov 60
    echo ">> stack up"
    ;;
  baseline)
    wait_healthy_gov 30
    wf=$(baseline_traffic)
    echo "${wf}" > results/last_workflow_run_id.txt
    echo ">> baseline workflow_run_id=${wf}"
    ;;
  down)
    docker compose down -v
    echo ">> stack down"
    ;;
  reset)
    docker compose down -v
    docker compose up -d
    wait_healthy_toxi 30
    wait_healthy_gov 60
    wf=$(baseline_traffic)
    echo "${wf}" > results/last_workflow_run_id.txt
    echo ">> reset complete; workflow_run_id=${wf}"
    ;;
  *)
    echo "usage: $0 {up|baseline|down|reset}" >&2
    exit 2
    ;;
esac
