#!/usr/bin/env bash
# Usage: ./runner.sh <flow> <intensity>
#   flow:      happy_path | dlq_flow | breaker_flow
#   intensity: smoke | load | soak
#
# Brings up the observability stack (pg + governance + otel + prom + grafana),
# waits for health, retargets the existing k6 script at port 8082 via BASE_URL,
# runs k6, and tears the stack down.
#
# Intended for validating the dashboard and re-measuring latency under OTel on.

set -euo pipefail

FLOW="${1:-}"
INTENSITY="${2:-}"

if [[ -z "${FLOW}" || -z "${INTENSITY}" ]]; then
  echo "usage: $0 <happy_path|dlq_flow|breaker_flow> <smoke|load|soak>" >&2
  exit 2
fi

SCRIPT="../../test/load/scripts/${FLOW}.js"
if [[ ! -f "$(dirname "$0")/${SCRIPT}" ]]; then
  echo "no such script: ${SCRIPT} (relative to $(dirname "$0"))" >&2
  exit 2
fi

case "${INTENSITY}" in
  smoke) ;;
  load)  ;;
  soak)  ;;
  *) echo "invalid intensity: ${INTENSITY}" >&2; exit 2 ;;
esac

HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p results
OUT="results/${FLOW}-${INTENSITY}-${TS}.json"

echo ">> bringing up observability stack"
docker compose up -d

echo ">> waiting for governance health on :8082"
for i in $(seq 1 60); do
  if curl -fsS "http://localhost:8082/api/v1/audit?limit=1" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo ">> waiting for prometheus at :9090"
for i in $(seq 1 30); do
  if curl -fsS "http://localhost:9090/-/ready" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo ">> running k6 (${FLOW} / ${INTENSITY}) against :8082 with OTel enabled"
BASE_URL="http://localhost:8082" K6_INTENSITY="${INTENSITY}" \
  k6 run --summary-export="${OUT}" "${SCRIPT}"

echo ">> fetching prometheus snapshot of governance_* metric names"
curl -fsS "http://localhost:9090/api/v1/label/__name__/values" \
  | jq -r '.data[] | select(startswith("governance_"))' \
  > "results/${FLOW}-${INTENSITY}-${TS}.metrics.txt" || true

echo ">> tearing down"
docker compose down -v

echo ">> done: ${OUT}"
