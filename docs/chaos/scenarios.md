# Chaos scenarios — observed behaviour matrix

**Status:** v1 — populated after Phase 3.5.C execution
**Harness:** `test/chaos/`
**Tooling:** toxiproxy + shell + psql

All criteria are boolean. A scenario passes only if every criterion holds.

| # | Scenario | Trigger | Expected | Pass/Fail | Observations |
|---|----------|---------|----------|-----------|--------------|
| S1 | pg down mid-workflow | `docker stop` pg + restart | graceful 5xx, no panic, recovery on pg restart, data intact | *to be filled after execution* | *to be filled* |
| S2 | pg latency injection | toxiproxy 500 ms ± 50 ms | 0 % errors, P99 ≥ 500 ms under toxic, baseline restored after removal | *to be filled* | *to be filled* |
| S3 | pg pool starvation | 30 s latency toxic + 30 parallel floods | pool pressure observable, no panic, pool recovers, no leaked conns | *to be filled* | *to be filled* |
| S4 | governance SIGKILL | `docker kill --signal=SIGKILL` + restart | restart clean, prior workflow queryable, monotonic counts; lease rows retained (observation only, not gate) | *to be filled* | *to be filled* |
| S5 | network partition | toxiproxy proxy disabled | identical to S1 from gov view, pg counts unchanged | *to be filled* | *to be filled* |

## Executing

Each scenario's README has the exact reproduction command. The harness master README (`test/chaos/README.md`) has the quickstart.

## Known gaps

- **S4 lease reconciliation**: v0.6.0 does not reconcile `execution_leases` on startup. Stale rows accumulate on every SIGKILL. Deferred to a separate track — do NOT fix in 3.5.C.
- **No CI integration**: scenarios are operator-runnable only. Automated smoke in CI is a follow-up.
- **Single-fault only**: v1 scope. Multi-fault composition (e.g. pg slow + notifier slow) is a future track.
