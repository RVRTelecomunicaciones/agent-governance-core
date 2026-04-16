# Phase 3 Track 1: Scalability & Performance — Design Spec

**Date**: 2026-04-16
**Status**: Approved
**Scope**: Phase 3 Track 1
**Baseline**: v0.3.0 (Phase 1 + Track 1 OTel + Track 2 Adaptive Routing)
**Stack**: Go 1.26.2, PostgreSQL 16+, pgx v5

---

## 1. Objective

Eliminate the immediate scalability bottlenecks in the audit trail and `FailureStatsCollector`, and prepare foundations for high-volume growth.

### Problems addressed

| Problem | Severity | Component |
|---|---|---|
| Collector fetches all entries and filters by time in Go | High | FailureStatsCollector |
| No composite index for the collector's query pattern | High | PostgreSQL |
| Audit query does separate COUNT(*) roundtrip | Medium | PgAuditEntryRepository |
| No retention/archive strategy for unbounded growth | Low (future) | Design |
| No preaggregate path for failure stats | Low (future) | Design |

### Target volume

Design for **medium volume** (100K-1M audit entries/month) with foundations for high scale. Do NOT over-engineer for extreme volume yet.

---

## 2. Changes

### 2.1 AuditFilter: Add temporal filters

Add `CreatedAfter` and `CreatedBefore` fields to the `AuditFilter` struct:

```go
type AuditFilter struct {
    TaskID        *shared.TaskID
    WorkflowRunID *shared.WorkflowRunID
    Actor         *shared.ActorID
    Action        *string
    CreatedAfter  *time.Time  // NEW — WHERE created_at > $N
    CreatedBefore *time.Time  // NEW — WHERE created_at < $N
    Limit         int
    Offset        int
}
```

The `PgAuditEntryRepository.Query` method adds these as WHERE conditions when present:

```sql
WHERE action = $1 AND created_at > $2 AND created_at < $3
```

No breaking changes — existing callers that don't set these fields get the same behavior as before (no time filter).

### 2.2 FailureStatsCollector: Filter by time in SQL

**Current code (collector refresh):**
```go
filter := outbound.AuditFilter{
    Action: &action,
    Limit:  10000,
}
entries, _, err := c.repo.Query(ctx, filter)
// Then filters in Go: if entry.CreatedAt().Before(cutoff) { continue }
```

**New code:**
```go
cutoff := time.Now().Add(-c.window)
filter := outbound.AuditFilter{
    Action:       &action,
    CreatedAfter: &cutoff,
    Limit:        0, // no artificial limit — time window bounds the result set
}
entries, _, err := c.repo.Query(ctx, filter)
// No more Go-side time filtering — SQL does it
```

Changes:
1. Pass `CreatedAfter` to the filter
2. Remove the Go-side `cutoff` filtering loop
3. Set `Limit: 0` (or a high safety limit like 100000) — the time window naturally bounds results
4. Remove the `cutoff` variable and the `entry.CreatedAt().Before(cutoff)` check

### 2.3 Composite index for collector query

New migration: `009_add_audit_entries_action_created_index.sql`

```sql
-- +goose Up
CREATE INDEX idx_audit_entries_action_created ON audit_entries(action, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_entries_action_created;
```

This index covers the collector's exact query pattern: `WHERE action = 'attempt_registered' AND created_at > $1`. PostgreSQL will use an index range scan instead of a full table scan or bitmap merge.

### 2.4 COUNT(*) OVER() optimization in audit query

**Note:** This is an optimization to validate with EXPLAIN ANALYZE evidence, not an assumed universal improvement. In some query plans, `COUNT(*) OVER()` may not outperform the two-query approach (e.g. when the planner can short-circuit the count via an index-only scan). The integration test must compare both approaches and keep the faster one.

**Current code (PgAuditEntryRepository.Query):**
```go
// Query 1: COUNT
countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_entries %s", whereClause)
// Query 2: DATA
dataQuery := fmt.Sprintf("SELECT ... FROM audit_entries %s ORDER BY created_at DESC LIMIT $N OFFSET $M", whereClause)
```

Two roundtrips. The COUNT scans the entire filtered result set even though we only need one page of data.

**Proposed new code — single query with window function:**
```go
dataQuery := fmt.Sprintf(`
    SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
           COUNT(*) OVER() AS total_count
    FROM audit_entries %s
    ORDER BY created_at DESC
    LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)
```

Each row includes `total_count`. Read it from the first row. Single roundtrip, PostgreSQL computes the count as part of the same query plan.

If the result set is empty, `total_count` is not available — return `(nil, 0, nil)`.

### 2.5 AuditFilter: Limit=0 handling

Currently `Limit` is always appended to the query. With the collector now passing `Limit: 0`, we need to handle this:

- `Limit > 0` — apply LIMIT as before
- `Limit == 0` — no LIMIT clause (return all matching rows within the time window)

**Important:** `Limit=0` is a controlled exception for technical/infrastructure callers like the `FailureStatsCollector` where a time window naturally bounds the result set. It is NOT intended for user-facing query paths. The HTTP handler for `GET /api/v1/audit` should enforce a default limit (e.g. 100) and a maximum limit (e.g. 1000) regardless of what the consumer sends. This enforcement lives in the HTTP adapter, not in the repo.

---

## 3. Performance Validation (mandatory)

Every improvement must be validated with evidence:

### 3.1 EXPLAIN ANALYZE: Collector query

Before (without composite index, without time filter in SQL):
```sql
EXPLAIN ANALYZE
SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at
FROM audit_entries
WHERE action = 'attempt_registered'
ORDER BY created_at DESC
LIMIT 10000;
```

After (with composite index, with time filter):
```sql
EXPLAIN ANALYZE
SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at
FROM audit_entries
WHERE action = 'attempt_registered' AND created_at > NOW() - INTERVAL '48 hours'
ORDER BY created_at DESC;
```

Expected: Index Scan on `idx_audit_entries_action_created` instead of Seq Scan or Bitmap Scan.

### 3.2 EXPLAIN ANALYZE: Audit query with COUNT(*) OVER()

```sql
EXPLAIN ANALYZE
SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
       COUNT(*) OVER() AS total_count
FROM audit_entries
WHERE task_id = '...'
ORDER BY created_at DESC
LIMIT 20 OFFSET 0;
```

Expected: Single execution plan (no separate count subquery).

### 3.3 Integration test: Collector refresh latency

Seed 1000+ audit entries in testcontainers PG, measure collector refresh time before and after the optimization. Log both measurements.

### 3.4 Integration test: Audit query latency

Seed audit entries, measure query latency with the COUNT(*) OVER() approach vs the two-query approach.

---

## 4. Future Strategies (documented, not implemented)

### 4.1 Preaggregate failure stats

When the collector query becomes too slow even with the composite index (likely at >5M entries in the 48h window — very high volume), introduce a preaggregate table:

```sql
CREATE TABLE failure_stats_hourly (
    hour          TIMESTAMPTZ NOT NULL,
    strategy      TEXT NOT NULL,
    agent_role    TEXT NOT NULL,
    category      TEXT NOT NULL,
    total         INTEGER NOT NULL,
    failures      INTEGER NOT NULL,
    PRIMARY KEY (hour, strategy, agent_role, category)
);
```

Populated by a background job that aggregates per-hour from audit_entries. The collector reads from this table instead of scanning individual entries. The `FailureStatsStore` interface doesn't change — only the collector's data source.

### 4.2 Retention / archive policy

When audit_entries exceeds a storage threshold (e.g. >50GB):

1. **Archive**: Copy entries older than N months to cold storage (S3/GCS as Parquet or JSON lines)
2. **Delete**: Remove archived entries from the primary table
3. **Partitioning**: If retention becomes frequent, partition by month (`audit_entries_2026_04`, etc.) for efficient drop-partition cleanup

The append-only invariant remains — entries are never modified. They can be archived and deleted after the retention window.

**Activation trigger:** Before implementing retention, observe these metrics over time:
- Table size (`pg_total_relation_size('audit_entries')`)
- Daily growth rate (rows/day, bytes/day)
- Collector refresh latency trend
- Storage cost

Activate archive/retention when the trend shows a concrete problem, not preemptively.

---

## 5. File Structure

### New files

```
migrations/postgres/
    009_add_audit_entries_action_created_index.sql  — composite index
test/integration/
    scalability/
        audit_performance_test.go                   — EXPLAIN ANALYZE + latency validation
```

### Modified files

| File | Change |
|---|---|
| `internal/ports/outbound/repositories.go` | Add `CreatedAfter`, `CreatedBefore` to AuditFilter |
| `internal/adapters/outbound/persistence/pg_audit_entry_repo.go` | Time filter in WHERE, COUNT(*) OVER(), Limit=0 handling |
| `internal/application/routing/failure_stats_collector.go` | Pass CreatedAfter, remove Go-side time filter |

### NOT modified

- Domain layer — untouched
- Port interfaces (except AuditFilter struct) — untouched
- Application layer (except collector) — untouched
- Inbound adapters — untouched
- Bootstrap/config — untouched

---

## 6. Implementation Blocks

| Block | Depends on | Scope |
|---|---|---|
| **B1: AuditFilter + repo optimization** | Nothing | AuditFilter fields, pg_audit_entry_repo changes, migration |
| **B2: Collector optimization** | B1 | collector.go: pass CreatedAfter, remove Go-side filter |
| **B3: Performance validation** | B1+B2 | Integration tests with EXPLAIN ANALYZE + latency measurements |
| **B4: Future strategy docs** | Nothing | Document preaggregate + retention in spec (already in this doc) |

B1 and B4 can run in parallel. B2 depends on B1. B3 depends on B1+B2.

```
B1 (AuditFilter + repo + migration)
 │    B4 (docs — already done in this spec)
 │
 B2 (collector optimization)
 │
 B3 (performance validation tests)
```

---

## 7. Baseline Invariants

- All existing tests continue to pass
- No changes to domain or application logic (except collector optimization)
- AuditFilter changes are additive (new optional fields)
- Collector produces identical FailureStats snapshots (same data, faster query)
- No schema changes to existing tables (only new index)
- `go build ./...` succeeds
- Binary starts and works identically

---

## Appendix: Decisions Summary

| # | Decision | Rationale |
|---|---|---|
| D1 | Design for medium volume with high-scale foundations | No clear volume estimate, pragmatic middle ground |
| D2 | Filter by time in SQL, not Go | Eliminates the main bottleneck — fetching unnecessary rows |
| D3 | Composite index on (action, created_at) | Covers collector's exact query pattern |
| D4 | COUNT(*) OVER() instead of separate count | Single roundtrip, same query plan |
| D5 | Limit=0 means no limit | Safe because time window bounds results; collector doesn't need artificial cap |
| D6 | EXPLAIN ANALYZE mandatory | Improvements must be validated with evidence, not assumptions |
| D7 | Preaggregate and retention documented, not implemented | Not needed at medium volume; foundations ready when needed |
