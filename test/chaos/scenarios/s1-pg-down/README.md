# S1 — pg down mid-workflow

## Symptom
Governance HTTP API begins returning 5xx or timing out. Logs show pgx connection errors.

## Diagnosis
PostgreSQL is unreachable. Check:
1. `docker ps | grep obs-chaos-pg` → container state
2. `docker logs obs-chaos-pg --tail=50` → shutdown or OOM messages
3. If production: connectivity, disk space, auth, cert rotation

## Remediation
1. Bring pg back: `docker start obs-chaos-pg` (or equivalent in production orchestrator)
2. Wait for healthcheck to turn green
3. Governance's pgx pool auto-reconnects on next request; no governance restart needed
4. Confirm recovery: `curl -sS -X POST http://localhost:8083/api/v1/tasks -H 'Content-Type: application/json' -d '{...}'` → 2xx
5. Verify data integrity: `psql ...-c "SELECT count(*) FROM tasks, workflow_runs;"` vs pre-incident snapshot

## Reproduce
```bash
test/chaos/runner.sh up && test/chaos/runner.sh baseline
test/chaos/scenarios/s1-pg-down/trigger.sh
test/chaos/scenarios/s1-pg-down/verify.sh
```
