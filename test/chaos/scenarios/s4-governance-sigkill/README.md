# S4 — governance SIGKILL

## Symptom
Governance process exited non-gracefully (OOM kill, panic, container stop without SIGTERM). On restart it boots clean. Any `execution_lease` rows from before the kill are retained as stale.

## Diagnosis
1. `docker inspect obs-chaos-governance --format '{{.State.ExitCode}} {{.State.OOMKilled}}'`
2. `docker logs obs-chaos-governance --tail=100` — look for panic, OOM
3. Post-restart: governance starts clean but **does NOT reconcile leases** (known gap in v0.6.0 — see Follow-ups in the spec)

## Remediation
1. Immediate: restart container — `docker start obs-chaos-governance` (or orchestrator equivalent)
2. Confirm healthy: `curl -sS http://localhost:8083/api/v1/audit?limit=1`
3. **Stale leases**: in v0.6.0 they are retained. Manual cleanup if needed:
   ```sql
   DELETE FROM execution_leases WHERE expires_at < NOW();
   -- or: UPDATE to reset the lease holder if the workflow should continue
   ```
4. For production: this gap should be addressed in a separate "lease reconciliation on startup" track — not part of 3.5.C.

## Reproduce
```bash
test/chaos/runner.sh up && test/chaos/runner.sh baseline
test/chaos/scenarios/s4-governance-sigkill/trigger.sh
test/chaos/scenarios/s4-governance-sigkill/verify.sh
cat test/chaos/scenarios/s4-governance-sigkill/results/observations.txt
```

## Known gap (do not fix in 3.5.C)
Governance v0.6.0 does not reconcile `execution_leases` on startup. Leases from a prior killed process persist until their `expires_at`. Separate track required to decide the recovery policy.
