# Chaos harness — Phase 3.5.C

Local-only chaos test harness for governance-core. Exercises 5 Tier-1 failure scenarios and documents observed behaviour.

## Security

**NOT apt for shared networks, staging, or production.** All ports bound to localhost; no auth on toxiproxy admin API.

## Quickstart

```bash
# Bring up the stack
test/chaos/runner.sh up

# Run a scenario end-to-end
test/chaos/runner.sh baseline
test/chaos/scenarios/s1-pg-down/trigger.sh
test/chaos/scenarios/s1-pg-down/verify.sh

# Tear down
test/chaos/runner.sh down
```

## Scenarios

| # | Scenario | Mechanism |
|---|----------|-----------|
| S1 | pg down mid-workflow | `docker stop` pg + restart |
| S2 | pg latency injection | toxiproxy `latency` toxic |
| S3 | pg pool starvation | toxiproxy long-latency holds pool |
| S4 | governance SIGKILL | `docker kill --signal=SIGKILL` + restart |
| S5 | network partition | toxiproxy proxy disabled |

## Ports

| Service | Host port | Purpose |
|---------|-----------|---------|
| pg (direct) | 5435 | Host access for psql assertions |
| governance | 8083 | HTTP API under test |
| toxiproxy admin | 8474 | Toxic add/remove REST API |

## Pass / Fail protocol

Each `verify.sh` exits 0 on PASS and non-zero on FAIL. Output is a table of numbered criteria. See per-scenario `README.md` for the criteria list and remediation runbook.

## Known gaps documented (no fix in 3.5.C)

- S4: `execution_lease` rows persist after SIGKILL — governance v0.6.0 does not reconcile on startup. See `scenarios/s4-governance-sigkill/README.md` → "Known gap". Separate track to address.

## Rebuild the governance image

Reuses `agent-governance-core:loadtest` built in 3.5.A:
```bash
docker build -f test/load/Dockerfile.governance -t agent-governance-core:loadtest .
```
