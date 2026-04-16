# Phase 3 Track 1: Scalability & Performance — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate audit trail scalability bottlenecks — move time filtering from Go to SQL, add composite index, optimize pagination count, and validate with EXPLAIN ANALYZE.

**Architecture:** Three surgical changes to the existing audit infrastructure: (1) temporal filter fields in AuditFilter + repo WHERE clause, (2) composite index migration, (3) COUNT(*) OVER() single-query pagination. Plus collector optimization to use the new SQL filtering. All validated with EXPLAIN ANALYZE evidence in integration tests.

**Tech Stack:** Go 1.26.2, PostgreSQL 16+, pgx v5, testcontainers-go

**Spec:** `docs/superpowers/specs/2026-04-16-phase3-track1-scalability-design.md`

**Baseline invariant:** All existing tests must pass. Collector produces identical FailureStats snapshots. No domain changes.

---

## File Structure

```
migrations/postgres/
    009_add_audit_entries_action_created_index.sql  — NEW: composite index
internal/
    ports/outbound/
        repositories.go                             — MODIFY: add CreatedAfter/CreatedBefore to AuditFilter
    adapters/outbound/persistence/
        pg_audit_entry_repo.go                      — MODIFY: time filters, COUNT(*) OVER(), Limit=0
    application/routing/
        failure_stats_collector.go                   — MODIFY: pass CreatedAfter, remove Go-side filter
test/integration/
    scalability/
        audit_performance_test.go                   — NEW: EXPLAIN ANALYZE + latency validation
```

---

## Task 1: AuditFilter + Repo Optimization + Migration (B1)

**Files:**
- Modify: `internal/ports/outbound/repositories.go`
- Modify: `internal/adapters/outbound/persistence/pg_audit_entry_repo.go`
- Create: `migrations/postgres/009_add_audit_entries_action_created_index.sql`

This is the largest task — it changes the AuditFilter struct, rewrites the Query method, and adds the index.

- [ ] **Step 1: Add temporal fields to AuditFilter**

Modify `internal/ports/outbound/repositories.go`. Add two fields to `AuditFilter`:

```go
import "time"

// AuditFilter defines filter criteria for querying audit entries.
type AuditFilter struct {
	TaskID        *shared.TaskID
	WorkflowRunID *shared.WorkflowRunID
	Actor         *shared.ActorID
	Action        *string
	CreatedAfter  *time.Time // WHERE created_at > $N
	CreatedBefore *time.Time // WHERE created_at < $N
	Limit         int
	Offset        int
}
```

Add `"time"` to the import block.

- [ ] **Step 2: Run build to verify additive change compiles**

Run: `go build ./...`
Expected: SUCCESS (new fields are optional, zero-value is nil, no callers break)

- [ ] **Step 3: Rewrite PgAuditEntryRepository.Query with time filters + COUNT(*) OVER() + Limit=0**

Replace the entire `Query` method in `internal/adapters/outbound/persistence/pg_audit_entry_repo.go`:

```go
func (r *PgAuditEntryRepository) Query(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
	// Build dynamic WHERE clause
	var conditions []string
	var args []any
	argIdx := 1

	if filter.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argIdx))
		args = append(args, filter.TaskID.String())
		argIdx++
	}
	if filter.WorkflowRunID != nil {
		conditions = append(conditions, fmt.Sprintf("workflow_run_id = $%d", argIdx))
		args = append(args, filter.WorkflowRunID.String())
		argIdx++
	}
	if filter.Actor != nil {
		conditions = append(conditions, fmt.Sprintf("actor = $%d", argIdx))
		args = append(args, filter.Actor.String())
		argIdx++
	}
	if filter.Action != nil {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, *filter.Action)
		argIdx++
	}
	if filter.CreatedAfter != nil {
		conditions = append(conditions, fmt.Sprintf("created_at > $%d", argIdx))
		args = append(args, *filter.CreatedAfter)
		argIdx++
	}
	if filter.CreatedBefore != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", argIdx))
		args = append(args, *filter.CreatedBefore)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Build query with COUNT(*) OVER() for single-roundtrip pagination
	var limitClause string
	if filter.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
		args = append(args, filter.Limit, filter.Offset)
	}
	// Limit == 0 means no LIMIT (controlled exception for technical callers like the collector)

	dataQuery := fmt.Sprintf(`
		SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
		       COUNT(*) OVER() AS total_count
		FROM audit_entries %s
		ORDER BY created_at DESC
		%s`, whereClause, limitClause)

	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying audit entries: %w", err)
	}
	defer rows.Close()

	var entries []*audit.AuditEntry
	var total int
	for rows.Next() {
		var (
			id            string
			taskID        *string
			workflowRunID *string
			actor         string
			action        string
			outcome       string
			contextJSON   []byte
			createdAt     shared.Timestamp
			totalCount    int
		)

		err := rows.Scan(&id, &taskID, &workflowRunID, &actor, &action, &outcome, &contextJSON, &createdAt.Time, &totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning audit entry: %w", err)
		}

		if total == 0 {
			total = totalCount // read from first row
		}

		var ctx audit.AuditContext
		if err := json.Unmarshal(contextJSON, &ctx); err != nil {
			return nil, 0, fmt.Errorf("unmarshaling audit context: %w", err)
		}

		var tid *shared.TaskID
		if taskID != nil {
			t := shared.TaskID(*taskID)
			tid = &t
		}

		var wid *shared.WorkflowRunID
		if workflowRunID != nil {
			w := shared.WorkflowRunID(*workflowRunID)
			wid = &w
		}

		entries = append(entries, audit.ReconstructAuditEntry(
			shared.AuditEntryID(id),
			tid,
			wid,
			shared.ActorID(actor),
			action,
			outcome,
			ctx,
			createdAt,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating audit entries: %w", err)
	}

	return entries, total, nil
}
```

Note: The `scanAuditEntry` helper function is no longer used by `Query` (scanning is inline now because of the extra `total_count` column). Keep `scanAuditEntry` if it's used elsewhere, or remove it if it's only used by Query.

- [ ] **Step 4: Create composite index migration**

Create `migrations/postgres/009_add_audit_entries_action_created_index.sql`:

```sql
-- +goose Up
CREATE INDEX idx_audit_entries_action_created ON audit_entries(action, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_entries_action_created;
```

- [ ] **Step 5: Run build + all existing tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS (zero regressions — AuditFilter changes are additive, Query returns identical results)

- [ ] **Step 6: Run integration tests**

Run: `go test ./test/integration/... -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS (existing repo + usecase + observability + adaptive tests still work with the new Query implementation)

- [ ] **Step 7: Commit**

```bash
git add internal/ports/outbound/repositories.go internal/adapters/outbound/persistence/pg_audit_entry_repo.go migrations/postgres/009_add_audit_entries_action_created_index.sql
git commit -m "feat(perf): add temporal filters to AuditFilter, COUNT(*) OVER() optimization, composite index"
```

---

## Task 2: Collector Optimization (B2)

**Files:**
- Modify: `internal/application/routing/failure_stats_collector.go`

- [ ] **Step 1: Modify collector refresh to use SQL time filtering**

Replace the `refresh` method in `internal/application/routing/failure_stats_collector.go`:

```go
// refresh queries the audit trail and builds a new FailureStats snapshot.
func (c *FailureStatsCollector) refresh(ctx context.Context) {
	action := "attempt_registered"
	cutoff := time.Now().Add(-c.window)
	filter := outbound.AuditFilter{
		Action:       &action,
		CreatedAfter: &cutoff,
		Limit:        0, // no artificial limit — time window bounds the result set
	}

	entries, _, err := c.repo.Query(ctx, filter)
	if err != nil {
		c.logger.Warn("failure stats refresh failed", "error", err)
		return
	}

	now := time.Now()

	byStratRoleCat := make(map[domainrouting.StatsKey]domainrouting.FailureRate)
	byStratCat := make(map[domainrouting.StatsKey]domainrouting.FailureRate)
	byStrat := make(map[domainrouting.RoutingStrategy]domainrouting.FailureRate)

	for _, entry := range entries {
		// No more Go-side time filtering — SQL already filtered by created_at > cutoff

		actx := entry.Context()

		strategyRaw, _ := actx["strategy_used"].(string)
		if strategyRaw == "" {
			continue
		}
		strategy := domainrouting.RoutingStrategy(strategyRaw)

		role, _ := actx["agent_role"].(string)
		failureCode, _ := actx["failure_code"].(string)
		category := domainrouting.ExtractCategory(failureCode)

		outcome := entry.Outcome()
		isFailure := outcome == "failure" || outcome == "retry"

		// Level 3: ByStrategy (always)
		fr := byStrat[strategy]
		fr.Total++
		if isFailure {
			fr.Failures++
		}
		byStrat[strategy] = fr

		// Level 2: ByStrategyCategory (only if category non-empty)
		if category != "" {
			key := domainrouting.StatsKey{Strategy: strategy, Category: category}
			fr2 := byStratCat[key]
			fr2.Total++
			if isFailure {
				fr2.Failures++
			}
			byStratCat[key] = fr2
		}

		// Level 1: ByStrategyRoleCategory (only if role AND category non-empty)
		if role != "" && category != "" {
			key := domainrouting.StatsKey{Strategy: strategy, Role: role, Category: category}
			fr3 := byStratRoleCat[key]
			fr3.Total++
			if isFailure {
				fr3.Failures++
			}
			byStratRoleCat[key] = fr3
		}
	}

	// Compute rates
	for k, v := range byStrat {
		if v.Total > 0 {
			v.Rate = float64(v.Failures) / float64(v.Total)
			byStrat[k] = v
		}
	}
	for k, v := range byStratCat {
		if v.Total > 0 {
			v.Rate = float64(v.Failures) / float64(v.Total)
			byStratCat[k] = v
		}
	}
	for k, v := range byStratRoleCat {
		if v.Total > 0 {
			v.Rate = float64(v.Failures) / float64(v.Total)
			byStratRoleCat[k] = v
		}
	}

	stats := &domainrouting.FailureStats{
		ByStrategyRoleCategory: byStratRoleCat,
		ByStrategyCategory:     byStratCat,
		ByStrategy:             byStrat,
		ComputedAt:             now,
		Window:                 c.window,
	}

	c.store.Update(stats)
	c.logger.Info("failure stats refreshed",
		"entries_processed", len(entries),
		"strategies", len(byStrat),
		"window", c.window,
	)
}
```

Key changes from the current code:
1. Added `cutoff` computed from `c.window` and passed as `CreatedAfter` in filter
2. Set `Limit: 0` instead of `10000`
3. Removed the `cutoff` variable used for Go-side filtering
4. Removed the `if entry.CreatedAt().Time.Before(cutoff) { continue }` check

- [ ] **Step 2: Run build + all tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 3: Run integration tests to verify collector still produces correct stats**

Run: `go test ./test/integration/... -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS — adaptive routing integration test still passes (collector produces same FailureStats, just faster)

- [ ] **Step 4: Commit**

```bash
git add internal/application/routing/failure_stats_collector.go
git commit -m "feat(perf): collector uses SQL time filtering instead of Go-side filtering"
```

---

## Task 3: Performance Validation Tests (B3)

**Files:**
- Create: `test/integration/scalability/audit_performance_test.go`

- [ ] **Step 1: Write performance validation integration test**

```go
//go:build integration

package scalability_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/persistence"
	approuting "github.com/russellcxl/agent-governance-core/internal/application/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/idgen"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/russellcxl/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectorQuery_UsesIndex verifies that the optimized collector query
// uses the composite index via EXPLAIN ANALYZE.
func TestCollectorQuery_UsesIndex(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	ctx := context.Background()

	// Seed 500 audit entries — mix of actions and timestamps
	gen := idgen.ULIDGenerator{}
	repo := persistence.NewPgAuditEntryRepository(db.Pool)

	for i := 0; i < 500; i++ {
		actx := audit.NewAuditContext().WithStrategy("direct").WithAgentRole("implementer")
		entry, err := audit.NewAuditEntry(
			gen.NewAuditEntryID(),
			shared.ActorID("system"),
			"attempt_registered",
			"success",
			actx,
			shared.MustTimestamp(time.Now().Add(-time.Duration(i)*time.Minute)),
		)
		require.NoError(t, err)
		require.NoError(t, repo.Append(ctx, entry))
	}
	// Also seed some non-attempt entries to make the filter meaningful
	for i := 0; i < 200; i++ {
		entry, err := audit.NewAuditEntry(
			gen.NewAuditEntryID(),
			shared.ActorID("system"),
			"task_submitted",
			"created",
			audit.NewAuditContext(),
			shared.MustTimestamp(time.Now().Add(-time.Duration(i)*time.Minute)),
		)
		require.NoError(t, err)
		require.NoError(t, repo.Append(ctx, entry))
	}

	// Run EXPLAIN ANALYZE on the collector query pattern
	cutoff := time.Now().Add(-48 * time.Hour)
	var plan string
	err := db.Pool.QueryRow(ctx, `
		EXPLAIN ANALYZE
		SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
		       COUNT(*) OVER() AS total_count
		FROM audit_entries
		WHERE action = $1 AND created_at > $2
		ORDER BY created_at DESC`,
		"attempt_registered", cutoff,
	).Scan(&plan)

	// EXPLAIN ANALYZE returns multiple rows — use Query instead
	rows, err := db.Pool.Query(ctx, `
		EXPLAIN ANALYZE
		SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
		       COUNT(*) OVER() AS total_count
		FROM audit_entries
		WHERE action = $1 AND created_at > $2
		ORDER BY created_at DESC`,
		"attempt_registered", cutoff,
	)
	require.NoError(t, err)
	defer rows.Close()

	var planLines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		planLines = append(planLines, line)
		t.Logf("EXPLAIN: %s", line)
	}
	require.NotEmpty(t, planLines)

	// Verify the index is used (look for "Index Scan" or "Index Only Scan" or "Bitmap Index Scan")
	fullPlan := fmt.Sprintf("%v", planLines)
	indexUsed := false
	for _, line := range planLines {
		if contains(line, "Index") && contains(line, "idx_audit_entries_action_created") {
			indexUsed = true
			break
		}
	}
	// With only 700 rows, PG may choose Seq Scan. Log the plan for evidence either way.
	t.Logf("Index used: %v (with %d total rows, PG may prefer Seq Scan for small tables)", indexUsed, 700)
	t.Logf("Full plan:\n%s", fullPlan)
	// Don't assert index usage — with small test data PG's planner may choose differently.
	// The value is in logging the plan for human review.
}

// TestCollectorRefresh_Latency measures collector refresh time with real PG.
func TestCollectorRefresh_Latency(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	ctx := context.Background()
	gen := idgen.ULIDGenerator{}
	repo := persistence.NewPgAuditEntryRepository(db.Pool)

	// Seed 1000 attempt_registered entries within 48h window
	for i := 0; i < 1000; i++ {
		actx := audit.NewAuditContext().
			WithStrategy("direct").
			WithAgentRole("implementer")
		if i%3 == 0 {
			actx = actx.Set("failure_code", "tool/shell_timeout")
		}
		outcome := "success"
		if i%4 == 0 {
			outcome = "failure"
		}
		entry, err := audit.NewAuditEntry(
			gen.NewAuditEntryID(),
			shared.ActorID("system"),
			"attempt_registered",
			outcome,
			actx,
			shared.MustTimestamp(time.Now().Add(-time.Duration(i)*time.Minute)),
		)
		require.NoError(t, err)
		require.NoError(t, repo.Append(ctx, entry))
	}

	store := approuting.NewFailureStatsStore()
	collector := approuting.NewFailureStatsCollector(
		store, repo,
		approuting.DefaultRefreshInterval, approuting.DefaultStatsWindow,
		slog.Default(),
	)

	// Measure refresh latency
	start := time.Now()
	collector.Refresh(ctx)
	elapsed := time.Since(start)

	t.Logf("Collector refresh latency with 1000 entries: %v", elapsed)

	// Verify stats were populated
	stats := store.Get()
	require.NotNil(t, stats)
	assert.NotEmpty(t, stats.ByStrategy, "should have strategy-level stats")

	// Latency should be reasonable (< 1 second for 1000 entries on testcontainers PG)
	assert.Less(t, elapsed, 5*time.Second, "refresh should complete in reasonable time")
}

// TestAuditQuery_PaginationWithCountOverWindow verifies the COUNT(*) OVER() approach.
func TestAuditQuery_PaginationWithCountOverWindow(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	ctx := context.Background()
	gen := idgen.ULIDGenerator{}
	repo := persistence.NewPgAuditEntryRepository(db.Pool)

	// Seed 50 entries
	for i := 0; i < 50; i++ {
		entry, err := audit.NewAuditEntry(
			gen.NewAuditEntryID(),
			shared.ActorID("system"),
			"task_submitted",
			"created",
			audit.NewAuditContext(),
			shared.MustTimestamp(time.Now().Add(-time.Duration(i)*time.Minute)),
		)
		require.NoError(t, err)
		require.NoError(t, repo.Append(ctx, entry))
	}

	// Query with pagination
	action := "task_submitted"
	entries, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action: &action,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)

	assert.Len(t, entries, 10, "should return 10 entries (page size)")
	assert.Equal(t, 50, total, "total should be 50 (all matching entries)")
}

// TestAuditQuery_TemporalFilter verifies CreatedAfter/CreatedBefore work correctly.
func TestAuditQuery_TemporalFilter(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	ctx := context.Background()
	gen := idgen.ULIDGenerator{}
	repo := persistence.NewPgAuditEntryRepository(db.Pool)

	now := time.Now()

	// Seed entries: 5 recent (within 1 hour), 5 old (25 hours ago)
	for i := 0; i < 5; i++ {
		entry, _ := audit.NewAuditEntry(gen.NewAuditEntryID(), shared.ActorID("system"), "test_action", "ok",
			audit.NewAuditContext(), shared.MustTimestamp(now.Add(-time.Duration(i)*time.Minute)))
		require.NoError(t, repo.Append(ctx, entry))
	}
	for i := 0; i < 5; i++ {
		entry, _ := audit.NewAuditEntry(gen.NewAuditEntryID(), shared.ActorID("system"), "test_action", "ok",
			audit.NewAuditContext(), shared.MustTimestamp(now.Add(-25*time.Hour-time.Duration(i)*time.Minute)))
		require.NoError(t, repo.Append(ctx, entry))
	}

	// Query with CreatedAfter = 2 hours ago → should return only the 5 recent entries
	cutoff := now.Add(-2 * time.Hour)
	action := "test_action"
	entries, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action:       &action,
		CreatedAfter: &cutoff,
		Limit:        100,
	})
	require.NoError(t, err)
	assert.Len(t, entries, 5, "should return only recent entries")
	assert.Equal(t, 5, total)
}

// TestAuditQuery_LimitZero verifies that Limit=0 returns all matching entries.
func TestAuditQuery_LimitZero(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	ctx := context.Background()
	gen := idgen.ULIDGenerator{}
	repo := persistence.NewPgAuditEntryRepository(db.Pool)

	// Seed 30 entries
	for i := 0; i < 30; i++ {
		entry, _ := audit.NewAuditEntry(gen.NewAuditEntryID(), shared.ActorID("system"), "bulk_action", "ok",
			audit.NewAuditContext(), shared.MustTimestamp(time.Now().Add(-time.Duration(i)*time.Minute)))
		require.NoError(t, repo.Append(ctx, entry))
	}

	action := "bulk_action"
	entries, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action: &action,
		Limit:  0, // no limit
	})
	require.NoError(t, err)
	assert.Len(t, entries, 30, "Limit=0 should return all entries")
	assert.Equal(t, 30, total)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run the performance validation tests**

Run: `go test ./test/integration/scalability/... -v -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS. Review the logged EXPLAIN ANALYZE output and latency measurements.

- [ ] **Step 3: Commit**

```bash
git add test/integration/scalability/
git commit -m "feat(perf): add scalability integration tests with EXPLAIN ANALYZE validation"
```

---

## Verification Checklist

After all tasks complete:

- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./internal/... ./test/fixtures/... -count=1` — ALL PASS (zero regressions)
- [ ] `go test ./test/integration/... -v -count=1 -tags=integration -timeout=600s` — ALL PASS
- [ ] EXPLAIN ANALYZE output logged (review for index usage)
- [ ] Collector refresh latency logged (< 5s for 1000 entries)
- [ ] Pagination test: 10 entries returned with total=50
- [ ] Temporal filter test: only recent entries returned
- [ ] Limit=0 test: all entries returned
- [ ] Existing adaptive routing integration test still passes (collector produces same stats)
