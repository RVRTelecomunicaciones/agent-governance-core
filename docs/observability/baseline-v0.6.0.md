# Load Baseline — v0.6.0

| Field       | Value                                                                                       |
|-------------|---------------------------------------------------------------------------------------------|
| Date        | 2026-04-16                                                                                  |
| Commit      | bc7d96d                                                                                     |
| Host        | Apple M1 Pro, 8 CPU cores, 16 GB RAM — single developer machine                            |
| Harness     | docker-compose: PostgreSQL 16-alpine + governance binary (`test/load/Dockerfile.governance`); stub memory-engine; stub notifier; OTel disabled |
| Tool        | k6 (brew install k6)                                                                        |
| Purpose     | Establish the first measured performance baseline for the three primary governance flows prior to Phase 3.5.B capacity work |

---

## Scope of this iteration

**Smoke + load only. Soak testing is deferred — see Follow-ups.**

Three flows were exercised at smoke scale (60 s, low VU) and load scale (5 min, 5× VU). No soak runs were executed in this iteration. All soak rows in the summary table below are marked accordingly.

---

## Summary table

| Flow         | Stage | VUs | Duration | Iterations  | Reqs    | Failed %  | P50    | P95    | P99     |
|--------------|-------|-----|----------|-------------|---------|-----------|--------|--------|---------|
| happy_path   | smoke |  10 | 60 s     | 4 710       | 23 550  | 0.00%     | 4.79ms | 8.28ms | 10.93ms |
| happy_path   | load  |  50 | 5 min    | 110 766     | 553 830 | 0.00%     | 4.8ms  | 16.6ms | 26.8ms  |
| happy_path   | soak  |  —  | —        | —           | —       | —         | —      | —      | —       |
| dlq_flow     | smoke |   5 | 60 s     | 2 325       | 18 600  | 12.50%†   | 3.44ms | 5.66ms | 7.33ms  |
| dlq_flow     | load  |  25 | 5 min    | 57 153      | 457 224 | 12.50%†   | 3.1ms  | 8.8ms  | 13.3ms  |
| dlq_flow     | soak  |  —  | —        | —           | —       | —         | —      | —      | —       |
| breaker_flow | smoke |   5 | 60 s     | 2 325       | 18 600  | 0.00%     | 3.42ms | 5.74ms | 7.45ms  |
| breaker_flow | load  |  25 | 5 min    | 57 529      | 460 232 | 0.00%     | 3.0ms  | 8.5ms  | 14.5ms  |
| breaker_flow | soak  |  —  | —        | —           | —       | —         | —      | —      | —       |

† dlq_flow 12.50% "failed" requests are expected terminal rejections: the governance state machine quarantines the workflow after retry budget exhaustion. All k6 checks passed. These are not real errors.

**Soak rows: DEFERRED — see Follow-ups.**

---

## Per-flow findings

### happy_path

**Sustained throughput (load):** 369 iter/s at 50 VU over 5 minutes. At smoke scale (10 VU, 60 s) the flow sustained 78 iter/s. Scaling from 10 → 50 VU (5×) produced 4.7× throughput — sub-linear in the healthy direction.

**Latency scaling:** P99 moved from 10.93 ms (smoke) to 26.8 ms (load), a 2.4× increase for a 5× VU increase. P50 was essentially flat (4.79 ms → 4.8 ms). No knee was observed in the measured range.

**First saturation cause:** PostgreSQL active connections peaked at 6–9 across all runs. The Go pgx connection pool was never saturated at this load level. No apparent bottleneck was reached within the measured window. Saturation point in this environment remains undetermined.

**Soak observations:** DEFERRED — see Follow-ups.

```
# Load raw summary (commit bc7d96d, file: happy_path-load-20260416T230900Z.json)
VUs=50  duration=5m  iterations=110766  reqs=553830
failed=0.00%  p50=4.8ms  p95=16.6ms  p99=26.8ms  throughput≈369 iter/s
pg active_conn_peak=6-9  deadlocks=0
```

---

### dlq_flow

**Sustained throughput (load):** 190 iter/s at 25 VU over 5 minutes. At smoke scale (5 VU, 60 s) the flow sustained 39 iter/s. Scaling from 5 → 25 VU (5×) produced 4.9× throughput — essentially linear.

**Latency scaling:** P99 moved from 7.33 ms (smoke) to 13.3 ms (load), a 1.8× increase for a 5× VU increase. P50 declined slightly (3.44 ms → 3.1 ms), consistent with connection reuse warming up under sustained load.

**First saturation cause:** Same as happy_path — pg connections peaked at 6–9, pool was not saturated. No apparent bottleneck in the measured range.

**Soak observations:** DEFERRED — see Follow-ups.

```
# Load raw summary (commit bc7d96d, file: dlq_flow-load-20260416T231412Z.json)
VUs=25  duration=5m  iterations=57153  reqs=457224
failed=12.50%†  p50=3.1ms  p95=8.8ms  p99=13.3ms  throughput≈190 iter/s
pg active_conn_peak=6-9  deadlocks=0
† expected terminal rejections — all k6 checks passed
```

---

### breaker_flow

**Sustained throughput (load):** 192 iter/s at 25 VU over 5 minutes. At smoke scale (5 VU, 60 s) the flow sustained 39 iter/s. Scaling from 5 → 25 VU (5×) produced 4.9× throughput — essentially linear.

**Latency scaling:** P99 moved from 7.45 ms (smoke) to 14.5 ms (load), a 1.9× increase for a 5× VU increase. P50 declined from 3.42 ms to 3.0 ms, same connection-warmup pattern as dlq_flow.

**First saturation cause:** Same observation — pg connections held at 6–9, pool not saturated. No apparent bottleneck in the measured range.

**Soak observations:** DEFERRED — see Follow-ups.

```
# Load raw summary (commit bc7d96d, file: breaker_flow-load-20260416T231924Z.json)
VUs=25  duration=5m  iterations=57529  reqs=460232
failed=0.00%  p50=3.0ms  p95=8.5ms  p99=14.5ms  throughput≈192 iter/s
pg active_conn_peak=6-9  deadlocks=0
```

---

## Known limits

**happy_path:** No ceiling was reached in the measured range. At 50 VU over 5 minutes the service delivered 369 iter/s with P99 at 26.8 ms and zero errors. The pg connection pool peaked at 9 active connections and never showed contention. The throughput scaling was sub-linear (4.7× for 5× VUs), which is consistent with fixed overheads (connection establishment, HTTP framing) and does not indicate saturation. The actual throughput ceiling on this single-host environment is unknown.

**dlq_flow:** No ceiling was reached in the measured range. At 25 VU over 5 minutes the service delivered 190 iter/s with P99 at 13.3 ms. The 12.5% request failure rate is by design — the governance state machine quarantines the workflow after retry budget exhaustion, and subsequent RegisterAttempt calls return a non-2xx terminal rejection. All k6 scenario checks passed. No unintended errors were observed. The throughput ceiling is unknown.

**breaker_flow:** No ceiling was reached in the measured range. At 25 VU over 5 minutes the service delivered 192 iter/s with P99 at 14.5 ms and zero errors. Throughput and latency scaling tracked dlq_flow closely, as expected given similar flow complexity. The throughput ceiling is unknown.

---

## Bugs / anomalies discovered (D8 fixes)

Three bugs were discovered and fixed in-iteration before the corrected smoke and load runs were taken. The uncorrected smoke run results (commit `f04ac84`) are superseded and must not be used as baseline numbers.

### 1. happy_path.js double-attempt script bug

**What:** The k6 script registered 2 `RegisterAttempt` success calls per iteration. The governance state machine completes (and closes) the workflow after the first success (`ErrTerminalWorkflow` — see `internal/application/workflowrun/register_attempt.go` lines 15–50). The second call received a non-2xx response, producing a false 16.7% error rate.

**When:** Discovered during the first smoke run (commit `f04ac84`, result file `happy_path-smoke-20260416T225531Z.json`).

**Fix (commit `8b5b034`):** Register exactly 1 success attempt per iteration.

**Filed as issue:** In-iteration fix.

---

### 2. URL cardinality explosion in all 3 scripts

**What:** All three scripts embedded ULIDs directly in request URLs (e.g., `/api/v1/workflows/01HX.../runs/01HX.../attempts`). k6 tagged each unique URL as a separate metric series, producing a high-cardinality warning in the summary output. This would cause memory exhaustion at scale.

**When:** Observed during the first smoke run across all three flows (commit `f04ac84`, result files `*-smoke-20260416T225531Z.json`, `*-smoke-20260416T225700Z.json`, `*-smoke-20260416T225817Z.json`).

**Fix (commit `8b5b034`):** Added `tags: { name: "POST /api/v1/<template>" }` to all parametrized requests to group by endpoint template. The high-cardinality warning disappeared in subsequent runs.

**Filed as issue:** In-iteration fix.

---

### 3. Goose migrations executed via pg initdb.d

**What:** The PostgreSQL container's `docker-entrypoint-initdb.d` directory executed the raw Goose migration SQL files, including the `-- +goose Down` sections. As a result, tables were created and then immediately dropped on container startup, leaving the schema empty.

**When:** Discovered during initial harness setup prior to the first smoke run.

**Fix (commit `f438ccd`):** Added `test/load/pg/init-db.sh`, a shell script that extracts only the `-- +goose Up` sections from each migration file using `awk` and pipes the result to `psql -v ON_ERROR_STOP=1`. The `initdb.d` directory now runs this script instead of the raw SQL files.

**Filed as issue:** In-iteration fix.

---

## Caveats

- **Single host:** All measurements were taken on a single developer Mac (Apple M1 Pro, 8 cores, 16 GB RAM). The governance binary, PostgreSQL, and k6 all ran on the same machine. These numbers are not representative of a networked staging or production environment. CPU and memory contention between components is not isolated.
- **Stub dependencies:** Both the memory-engine and the notifier are stubs. Real downstream latency and failure modes are not reflected in these numbers.
- **OTel disabled:** OpenTelemetry instrumentation was not enabled during any run. CPU and memory overhead from tracing is not captured. Turning OTel on may shift latency and throughput figures.
- **pg_stat_statements not enabled:** Per-query timing breakdowns are not available for this iteration. The saturation cause analysis (pool held at 6–9 connections) is based on `pg_stat_activity` snapshot counts only, not query-level profiling.
- **5-minute runs only:** The load stage ran for 5 minutes per flow. No slow memory, goroutine, or connection leaks would manifest in this window. Soak testing is required before trusting long-run stability.

---

## Follow-ups

1. **Soak baseline:** Run each flow at load VU levels for ≥ 30 minutes. Monitor goroutine count, heap allocations, pg active connections, and idle-in-transaction sessions over time. Establish whether the system is stable or drifts.
2. **pg_stat_statements:** Enable `pg_stat_statements` in the load harness PostgreSQL config. Repeat the load runs and capture per-query timing to identify the slowest queries and confirm or refute the connection-pool saturation hypothesis.
3. **Re-runs on suspicious results:** If soak runs reveal drift or if pg_stat_statements surfaces unexpected query times, re-run the 5-minute load stage with OTel enabled to correlate trace spans with latency percentiles.
4. **Staging host repeat:** Repeat smoke + load + soak on a networked multi-host staging environment (separate pg node, real memory-engine, real notifier). Single-host numbers are useful for regression detection but not for capacity planning.

---

## Hand-off to Phase 3.5.B

Phase 3.5.B capacity work starts from the following anchors established in this iteration:

- **happy_path:** 369 iter/s at 50 VU, P99 26.8 ms, zero errors. Throughput scaling is sub-linear (healthy). No saturation observed.
- **dlq_flow:** 190 iter/s at 25 VU, P99 13.3 ms. Expected 12.5% terminal rejection rate. No unintended errors.
- **breaker_flow:** 192 iter/s at 25 VU, P99 14.5 ms, zero errors.
- **PostgreSQL:** Peak 9 active connections across all runs. Zero deadlocks. Pool was not the bottleneck at these load levels.

Soak data is not yet available. Phase 3.5.B must not assume long-run stability from these numbers alone. The first action in 3.5.B should be to execute soak runs (Follow-up item 1 above) before drawing capacity conclusions or sizing the connection pool for higher loads.
