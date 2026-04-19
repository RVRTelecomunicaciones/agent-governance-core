# Phase 3.5.C: Chaos Scenarios — Design Spec

**Date**: 2026-04-19
**Status**: Approved
**Scope**: Phase 3.5 — Hardening, Sub-track C (Chaos scenarios — Tier 1)
**Baseline**: v0.6.0 + 3.5.A load baseline + 3.5.B observability/SLOs
**Tooling**: `toxiproxy` (for network/db chaos) + shell scripts (for container-level events) + curl/psql for assertions
**Purpose**: Exercise governance under real failure conditions and document observed behaviour so operators can recover incidents without reading code.

---

## 1. Objective

Run 5 controlled failure scenarios against the running governance stack and produce, for each one:

1. An explicit trigger (exact command to reproduce)
2. An expected-behaviour statement
3. A verification procedure (HTTP + SQL assertions)
4. **A binary pass/fail outcome** — not a narrative
5. A short runbook entry for the operator when the same failure shows up in production

Scope for v1 is Tier 1 only — failures that can be induced without code changes. Tier 2 (notifier/memory-engine failure injection) is deferred — requires code changes to stubs.

---

## 2. Scope — Tier 1 scenarios

| # | Scenario | Mechanism |
|---|----------|-----------|
| S1 | pg down mid-workflow | `docker stop obs-chaos-pg` while a workflow is running |
| S2 | pg latency injection (500 ms + 50 ms jitter) | toxiproxy `latency` toxic on pg proxy |
| S3 | pg connection pool starvation | toxiproxy long-latency toxic holds pgx pool connections busy → effective exhaustion (NOT a hard cap on connection count) |
| S4 | Container SIGKILL of governance | `docker kill --signal=SIGKILL obs-chaos-governance` mid-workflow |
| S5 | Network partition governance ↔ pg | toxiproxy `bandwidth=0` or `down` toxic |

---

## 3. Out of scope (explicit)

- **Tier 2 scenarios**: notifier failure, memory-engine timeout — would require failure-injecting variants of the stubs. Deferred.
- **Kubernetes / orchestrator chaos**: pod eviction, node drain, etc. Docker-compose only.
- **Production chaos engineering**: this is hardening validation, not a live GameDay platform.
- **Automated chaos in CI**: manual runs only for v1. Adding to CI is a separate follow-up.
- **Fault-tolerance improvements**: D7 — we are OBSERVING failure modes and documenting them, not fixing governance code under the same track. Fixes go to separate tracks.
- **Complex multi-fault chaos**: e.g. "kill pg and governance simultaneously". Single-fault scenarios only.

---

## 4. Stack topology

### New compose at `test/chaos/docker-compose.yaml`

```
┌──────────────────┐    5432     ┌──────────────┐    5432     ┌──────────────┐
│   governance     │ ──────────▶ │  toxiproxy   │ ──────────▶ │      pg      │
│  OTEL_ENABLED=   │             │  :8474 admin │             │              │
│     false        │             │  :5432 proxy │             │              │
└──────────────────┘             └──────────────┘             └──────────────┘
```

- Governance connects to `toxiproxy:5432` (NOT directly to pg) — this lets us apply DB-side toxics without touching the governance config.
- OTel is **disabled** in the chaos compose — we want to isolate failure behaviour, not measure telemetry overhead. Chaos findings about observability can be a follow-up.
- Toxiproxy admin API exposed at host `localhost:8474` so shell scripts can apply/remove toxics via curl.

### Ports (shifted again to avoid 3.5.A / 3.5.B collisions)

| Service | Host port | Container port | Purpose |
|---------|-----------|----------------|---------|
| pg | 5435 | 5432 | Governance DB (chaos-only) |
| toxiproxy admin | 8474 | 8474 | Toxic configuration REST API |
| toxiproxy proxy | — | 5432 (internal) | governance → pg path |
| governance | 8083 | 8080 | Governance HTTP API |

### Reused images

- `agent-governance-core:loadtest` (from 3.5.A)
- `postgres:16-alpine`
- `ghcr.io/shopify/toxiproxy:2.9.0` (official pinned)

---

## 5. Toxic provisioning

Toxiproxy loads proxies at startup from a JSON config file mounted read-only. The proxy is created on startup with no toxics applied — toxics are added per-scenario via `POST /proxies/<name>/toxics`.

### `test/chaos/toxiproxy/config.json`

```json
[
  {
    "name": "pg",
    "listen": "0.0.0.0:5432",
    "upstream": "pg:5432",
    "enabled": true
  }
]
```

### Toxic application scripts

Each scenario has a dedicated shell script at `test/chaos/scenarios/<name>/trigger.sh` that applies + removes the relevant toxic or invokes docker actions. Each script exits 0 on successful trigger application; non-zero means the chaos couldn't even be introduced (rare but should surface clearly).

---

## 6. Scenario matrix (pass / fail criteria)

Every criterion is boolean. Scenarios pass only if **all** listed criteria hold.

### S1 — pg down mid-workflow

**Trigger**
```bash
# While a workflow is in 'running' state:
docker stop obs-chaos-pg
# Wait 30 s
docker start obs-chaos-pg
```

**Expected behaviour**
Governance HTTP degrades gracefully (5xx), does not crash. On pg return, service recovers and prior workflow data is intact.

**Verification** (HTTP + SQL, exact commands in runbook)
1. During pg-down: `POST /api/v1/tasks` returns HTTP 5xx
2. Governance container is still `running` per `docker ps`
3. Governance logs contain no panic / stack trace
4. After pg restart + 15 s grace: `POST /api/v1/tasks` returns 2xx
5. `SELECT count(*) FROM tasks, workflow_runs` pre-kill vs post-recovery — counts match

**Pass if**: all 5 criteria hold.

---

### S2 — pg latency injection (500 ms + 50 ms jitter)

**Trigger**
```bash
curl -s -X POST localhost:8474/proxies/pg/toxics \
  -H 'Content-Type: application/json' \
  -d '{"name":"latency","type":"latency","attributes":{"latency":500,"jitter":50}}'
# Run k6 smoke against governance
# Remove toxic:
curl -s -X DELETE localhost:8474/proxies/pg/toxics/latency
```

**Expected behaviour**
All requests succeed. P99 visibly bumped by ~500 ms. No timeout-related errors (governance default query timeout is 30 s — 500 ms is well under).

**Verification**
1. k6 `http_req_failed` rate = 0 %
2. k6 `http_req_duration` P99 ≥ 500 ms (proof toxic took effect)
3. k6 P50 within [450 ms, 700 ms] (latency + base)
4. After toxic removed and 15 s baseline run: P99 < 50 ms (back to warm baseline)
5. No panic, no crash, no unexpected restart — `docker inspect` shows governance container uptime spans the entire scenario (FinishedAt unchanged, restart count unchanged)

**Pass if**: all 5 criteria hold.

---

### S3 — pg connection pool starvation (via held connections)

> **Framing:** toxiproxy does NOT provide a hard cap on connection count. This scenario induces **pool starvation** by holding existing connections busy with a long-latency toxic (30 s) — the governance pgx pool saturates because every checked-out connection is blocked. Not a hard cap test; an "effective exhaustion via busy connections" test. Record it as such in the runbook.


**Trigger**
```bash
curl -s -X POST localhost:8474/proxies/pg/toxics \
  -H 'Content-Type: application/json' \
  -d '{"name":"connection_cap","type":"limit_data","attributes":{"bytes":0}}'
# Alternative: set the proxy's listen max — toxiproxy doesn't directly cap
# connection count, but we can apply a low bandwidth to make connections
# effectively useless. Better: use a very high latency toxic (e.g. 30s)
# which keeps connections busy so the pool drains.
curl -s -X POST localhost:8474/proxies/pg/toxics \
  -H 'Content-Type: application/json' \
  -d '{"name":"pool_block","type":"latency","attributes":{"latency":30000}}'
# Flood governance with parallel requests:
for i in $(seq 1 30); do
  curl -sS -X POST http://localhost:8083/api/v1/tasks \
    -H 'Content-Type: application/json' \
    -d '{"type":"bugfix","title":"flood-'$i'","scope":"file","priority":"normal"}' &
done
wait
# Remove toxic:
curl -s -X DELETE localhost:8474/proxies/pg/toxics/pool_block
```

**Expected behaviour**
Governance serializes requests on the pool; once pool is exhausted, subsequent requests either wait or return 5xx. Process does not deadlock. Pool recovers when toxic is removed.

**Verification**
1. At least one request returns 5xx or takes > 5 s (proof of pool pressure)
2. No panic in governance logs; process still running
3. After toxic removed: `POST /api/v1/tasks` normal latency within 15 s
4. `pg_stat_activity` connection count returns to ≤ 5 within 30 s (no leaked connections)

**Pass if**: all 4 criteria hold.

---

### S4 — Container SIGKILL of governance

**Trigger**
```bash
# While a workflow is in 'running' state and an execution_lease exists:
docker kill --signal=SIGKILL obs-chaos-governance
# Observe briefly, then restart:
docker start obs-chaos-governance
# Wait 30 s for recovery
```

**Expected behaviour**
Governance dies abruptly with no graceful shutdown. On restart it starts clean. Any active `execution_lease` rows remain in DB as "stale" until expiry (governance does not reconcile leases on startup in v0.6.0 — this is a documented gap). No data corruption.

**Verification**
1. Pre-kill: capture `workflow_run_id`, `task_id`, and `execution_lease` row counts
2. `docker kill --signal=SIGKILL` — container exits immediately
3. `docker start` — container comes back `running`
4. Post-restart: `POST /api/v1/tasks` returns 2xx within 15 s
5. Prior `workflow_run` row still present and in its prior state (SQL check)
6. Prior `execution_lease` rows still present (acknowledged stale — this is documented behaviour, NOT a failure)
7. No data corruption: row counts for tasks/workflow_runs/audit_entries are monotonic (did not decrease)

**Pass if**: criteria 1–5 and 7 hold. Criterion 6 is a **documented observation**, not a pass gate, but must be recorded in the findings.

---

### S5 — Network partition governance ↔ pg

**Trigger (primary — `proxy enabled=false`)**
```bash
# Most reproducible: disable the proxy — all new and existing TCP connections
# are terminated. Governance sees connection refused / reset.
curl -s -X POST localhost:8474/proxies/pg \
  -H 'Content-Type: application/json' \
  -d '{"enabled":false}'
# Wait 30 s
curl -s -X POST localhost:8474/proxies/pg \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true}'
```

**Alternative (bandwidth=0)**: If you need to observe "slow death" behaviour instead of hard disconnect, apply a `bandwidth=0` toxic — connections stay open but no bytes flow. Useful for long-hang simulations, but less reproducible than enabled=false. The primary trigger above is what `trigger.sh` uses.

**Expected behaviour**
From governance's view: identical to S1 (pg unreachable). But pg process is alive the whole time, so recovery is faster.

**Verification**
Same as S1 criteria 1-4, plus:
5. After partition heals: pg row counts unchanged (pg itself never went down, so no data ever disappeared)

**Pass if**: all 5 criteria hold.

---

## 7. Harness layout

```
test/chaos/
├── docker-compose.yaml
├── toxiproxy/
│   └── config.json
├── scenarios/
│   ├── s1-pg-down/
│   │   ├── trigger.sh            # docker stop/start pg
│   │   ├── verify.sh             # runs the 5 assertions, prints PASS/FAIL
│   │   └── README.md             # short runbook: symptom → diagnosis → action
│   ├── s2-pg-latency/
│   │   ├── trigger.sh
│   │   ├── verify.sh
│   │   └── README.md
│   ├── s3-pool-exhaustion/
│   │   ├── trigger.sh
│   │   ├── verify.sh
│   │   └── README.md
│   ├── s4-governance-sigkill/
│   │   ├── trigger.sh
│   │   ├── verify.sh
│   │   └── README.md
│   └── s5-partition/
│       ├── trigger.sh
│       ├── verify.sh
│       └── README.md
├── runner.sh                     # brings stack up, drives baseline traffic, tears down
├── common.sh                     # shared helpers: wait_healthy, baseline_traffic, count_rows
├── results/
│   └── .gitkeep
└── README.md                     # master doc: how to use the harness

docs/chaos/
└── scenarios.md                  # matrix: scenario × expected × verification × observed × pass/fail
```

### verify.sh pattern

Each `verify.sh` exits 0 on PASS (all criteria hold) and non-zero on FAIL. Output is a short table:

```
S1 — pg down mid-workflow
─────────────────────────────────
[1] POST during pg-down → 5xx     PASS
[2] gov container running          PASS
[3] no panic in logs               PASS
[4] POST after recovery → 2xx      PASS
[5] row counts unchanged           PASS
─────────────────────────────────
RESULT: PASS
```

---

## 8. Execution flow per scenario

1. Operator brings up the chaos stack: `docker compose -f test/chaos/docker-compose.yaml up -d`
2. Run `test/chaos/runner.sh baseline` — a short happy-path traffic stream to create prior-state rows (one task, one workflow, one attempt). Records baseline row counts.
3. Run `test/chaos/scenarios/<N>/trigger.sh` — applies the chaos.
4. Run `test/chaos/scenarios/<N>/verify.sh` — asserts the pass/fail criteria. Prints the table. Exits 0 or non-zero.
5. (Optional) Re-run the scenario for confirmation.
6. Tear down: `docker compose -f test/chaos/docker-compose.yaml down -v`

---

## 9. Deliverables & close criteria

### Artifacts

- `test/chaos/` harness (compose, toxiproxy config, 5 scenario directories, runner, shared helpers, README)
- `docs/chaos/scenarios.md` — the master matrix with observed behaviour per scenario
- 5 scenario `README.md` files — each a 1-page runbook (symptoms, diagnosis, remediation)

### Closes when

- All 5 scenarios have their trigger.sh + verify.sh committed
- All 5 have been executed at least once and their `verify.sh` outcome documented in `scenarios.md`
- Pass scenarios: criteria satisfied, committed results included
- Fail scenarios (if any): documented as findings with clear ownership — do NOT auto-fix governance code (D7); file as a gap and move on
- The master README lets a reviewer run any scenario from scratch using only the docs

---

## 10. Decision log

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | toxiproxy + shell scripts | Go-native, proven for DB chaos; shell handles container-level events cleanly |
| D2 | Tier 1 only (no code changes) | Notifier/memory-engine failure injection requires failure-injecting stub variants — defer |
| D3 | `test/chaos/` new dir | Clean isolation from 3.5.A load and 3.5.B observability harnesses |
| D4 | Shell scripts + SQL/HTTP assertions | Ship-now operability — each scenario is operator-runnable without Go test harness |
| D5 | Matrix + per-scenario runbook | Incident-ready artefacts; the runbooks are what's useful at 3 AM |
| D6 | Every criterion boolean and explicit | Pass/fail must be unambiguous; narrative assessments are rejected |
| D7 | No governance code fixes under this track | Observe + document failure behaviour; fixes (e.g. lease reconciliation) are separate tracks |
| D8 | OTel disabled in chaos compose | Isolate failure behaviour from telemetry behaviour |
| D9 | Single-fault scenarios only | v1 hardening; multi-fault composition is a future track |
| D10 | Ports shifted to 5435 / 8083 | Run alongside 3.5.A / 3.5.B if debugging |

---

## 11. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| toxiproxy itself is a SPOF — if it hangs, chaos scripts can't clean up | Every trigger.sh has a matching cleanup path (toxic DELETE or `docker start`) run unconditionally via `trap EXIT` |
| A scenario reveals a real bug mid-run | Document it. Do NOT fix in this track (D7). File as a gap. Proceed with the remaining scenarios. |
| Lease reconciliation gap (S4) is ambiguous pass/fail | Pre-declared in the spec as an **observation**, not a gate. Record the observed behaviour, flag for a separate track to decide remediation. |
| Assertion commands depend on toxiproxy admin being reachable | Shared helper `wait_healthy` polls both pg and toxiproxy:8474 before scenarios run |
| ms-cotizacion-* port conflicts | Doc README lists the ports used (5435, 8083, 8474) — none overlap with 3.5.A or 3.5.B, but check anyway |
| Governance pool size unknown → S3 may not reliably saturate | Verify governance pool config first (pgxpool default is 4 max-conns per host — confirm at execution). Tune the flood accordingly. |

---

## 12. Follow-ups (deferred from this track)

- **Tier 2 scenarios** — notifier failure, memory-engine timeout. Need failure-injecting stubs.
- **Lease reconciliation on restart** — currently documented as stale in S4. Separate track to decide recovery policy.
- **Automated chaos in CI** — a smoke subset of the chaos matrix gating PRs. Needs Go test harness.
- **Multi-fault composition** — e.g. pg slow + notifier slow at once. Richer chaos engineering.
- **Chaos under OTel-enabled stack** — verify observability telemetry doesn't amplify failure impact (e.g. trace export blocking on a dying collector).

---

## 13. Ready-to-plan checklist

- [x] 5 scenarios defined with mechanism, trigger, expected, verification, pass/fail
- [x] Tooling + harness location + port allocation concrete
- [x] Toxiproxy topology diagrammed and config file defined
- [x] Directory layout locked
- [x] Out of scope explicit (including D7 no-fix rule)
- [x] Close criteria measurable
- [x] Deliverables named (5 trigger.sh, 5 verify.sh, 5 runbook READMEs, 1 matrix doc, harness README)
- [x] Lease gap in S4 pre-declared as observation (not a pass gate)
- [x] User precision honoured: every scenario has trigger / verification / explicit pass-fail

Spec ready for plan generation.
