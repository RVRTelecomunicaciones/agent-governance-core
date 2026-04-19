# S5 — network partition governance ↔ pg

## Symptom
Governance returns 5xx. pg is healthy and reachable from other clients. Connection errors in governance logs: dial tcp timeout, connection refused.

## Diagnosis
Network path between governance and pg is broken while pg itself is fine:
1. From governance container: `docker exec obs-chaos-governance sh -c 'nc -zv pg 5432'` → timeout or refused
2. From another client (psql directly): `psql postgresql://...localhost:5435/governance` → succeeds (pg is up)
3. Check: security groups, NACLs, service mesh, DNS, LB health-check flapping

## Remediation
1. Restore network path (ops problem, not governance)
2. Once path is up, governance reconnects on next request; no restart needed
3. Verify: `curl -sS http://localhost:8083/api/v1/audit?limit=1` → 2xx
4. Data integrity: pg never went down, so no row loss expected. Reconcile only if specific operations were mid-flight (check audit_entries for partial transactions).

## Reproduce (chaos harness)
```bash
test/chaos/runner.sh up
test/chaos/scenarios/s5-partition/trigger.sh
test/chaos/scenarios/s5-partition/verify.sh
```

## Partition mechanism
The harness triggers the partition by calling `proxy_set_enabled false` on the toxiproxy admin API — this disables the `pg` proxy entirely, making the port unreachable to governance while pg itself stays healthy. Alternative for slow-death simulations: inject a `bandwidth` toxic with rate=0 B/s, which achieves the same effect gradually rather than as a hard cut.
