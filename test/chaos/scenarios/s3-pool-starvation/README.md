# S3 — pg pool starvation (by held connections)

## Symptom
Governance HTTP latency balloons. Some requests 5xx. `pg_stat_activity` shows many long-running sessions.

## Diagnosis
The pgx pool is saturated — every connection is held on a slow query or a blocked upstream. Not a hard cap on connections; effective exhaustion via busy sessions.
1. `SELECT pid, state, wait_event_type, wait_event, query FROM pg_stat_activity WHERE datname='governance' ORDER BY state_change;`
2. Look for locked sessions, long IDLE-IN-TRANSACTION, or network-blocked `ClientWrite`
3. Governance pgx pool default is `max(4, NumCPU)` — typically 8 on dev / 4 in containers

## Remediation
1. Identify the blocking cause (network, replica lag, bad query, downstream service)
2. Terminate stuck sessions if clearly hung: `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE ...`
3. Lift the blocking cause; pool drains naturally
4. If chronic: consider bumping pool size or sharding workload (separate track)

## Reproduce
```bash
test/chaos/runner.sh up
test/chaos/scenarios/s3-pool-starvation/trigger.sh
test/chaos/scenarios/s3-pool-starvation/verify.sh
```
