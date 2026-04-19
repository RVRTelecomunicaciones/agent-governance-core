# S2 — pg latency injection

## Symptom
Governance response times spike. No errors, just slow. Metrics show elevated `governance_routing_duration_ms` P99.

## Diagnosis
Network or pg-side latency. Check:
1. Prometheus: `histogram_quantile(0.99, sum by (le) (rate(governance_workflow_duration_ms_milliseconds_bucket[5m])))`
2. pg `log_min_duration_statement` output (if enabled) — identify slow queries
3. Network: ping, traceroute pg. Cloud link health.

## Remediation
Latency is environmental — governance can't fix it. Options:
1. Restore healthy path (cloud team, network team, or, in chaos harness: `curl -X DELETE http://localhost:8474/proxies/pg/toxics/latency`)
2. If latency is pg-side slow queries: inspect `pg_stat_activity`, add indexes, or kill problematic sessions
3. If governance should handle it: tune query timeouts (not covered in 3.5.C)

## Reproduce
```bash
test/chaos/runner.sh up
test/chaos/scenarios/s2-pg-latency/trigger.sh
test/chaos/scenarios/s2-pg-latency/verify.sh
```
