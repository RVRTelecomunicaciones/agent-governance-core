//go:build integration

package scalability_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	approuting "github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/infrastructure/idgen"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedAuditEntries inserts count audit entries with the given action,
// each offset by the given duration from now (plus i minutes for spread).
func seedAuditEntries(
	t *testing.T,
	repo *persistence.PgAuditEntryRepository,
	gen *idgen.ULIDGenerator,
	count int,
	action string,
	offset time.Duration,
) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		actx := audit.NewAuditContext().WithStrategy("direct").WithAgentRole("implementer")
		outcome := "success"
		if i%3 == 0 {
			outcome = "failure"
			actx = actx.Set("failure_code", "tool/shell_timeout")
		}
		ts := time.Now().Add(-offset - time.Duration(i)*time.Minute)
		entry, err := audit.NewAuditEntry(
			gen.NewAuditEntryID(),
			shared.ActorID("system"),
			action,
			outcome,
			actx,
			shared.MustTimestamp(ts),
		)
		require.NoError(t, err)
		require.NoError(t, repo.Append(ctx, entry))
	}
}

// TestCollectorQuery_ExplainAnalyze seeds 500+ entries and logs the
// EXPLAIN ANALYZE output for the collector query pattern. The value is
// the evidence in the log, not a hard assertion on scan type (PG may
// choose Seq Scan for small datasets).
func TestCollectorQuery_ExplainAnalyze(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	gen := idgen.ULIDGenerator{}
	ctx := context.Background()

	// Seed 500 attempt_registered entries + 100 other actions for realism.
	seedAuditEntries(t, repo, &gen, 500, "attempt_registered", 0)
	seedAuditEntries(t, repo, &gen, 100, "task_submitted", 0)

	cutoff := time.Now().Add(-48 * time.Hour)

	rows, err := db.Pool.Query(ctx,
		`EXPLAIN ANALYZE
		 SELECT id, task_id, workflow_run_id, actor, action, outcome, context, created_at,
		        COUNT(*) OVER() AS total_count
		 FROM audit_entries
		 WHERE action = $1 AND created_at > $2
		 ORDER BY created_at DESC`,
		"attempt_registered", cutoff,
	)
	require.NoError(t, err)
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
		t.Logf("EXPLAIN: %s", line)
	}
	require.NoError(t, rows.Err())
	assert.NotEmpty(t, plan, "EXPLAIN ANALYZE should return a query plan")
}

// TestCollectorRefresh_Latency measures the end-to-end time for a Refresh
// with 1000 entries in the 48h window. Asserts it completes in under 5s
// (generous for testcontainers PG) and verifies the stats store is populated.
func TestCollectorRefresh_Latency(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	gen := idgen.ULIDGenerator{}
	ctx := context.Background()

	// Seed 1000 attempt_registered entries within the 48h window.
	seedAuditEntries(t, repo, &gen, 1000, "attempt_registered", 0)

	statsStore := approuting.NewFailureStatsStore()
	logger := slog.Default()
	collector := approuting.NewFailureStatsCollector(
		statsStore, repo, 5*time.Minute, 48*time.Hour, logger,
	)

	start := time.Now()
	collector.Refresh(ctx)
	elapsed := time.Since(start)

	t.Logf("Collector.Refresh latency: %v (1000 entries)", elapsed)
	assert.Less(t, elapsed, 5*time.Second, "Refresh should complete in under 5s")

	stats := statsStore.Get()
	require.NotNil(t, stats, "stats store should be populated after refresh")
	assert.NotEmpty(t, stats.ByStrategy, "ByStrategy should have data")
	t.Logf("Stats: strategies=%d, window=%s, computed_at=%s",
		len(stats.ByStrategy), stats.Window, stats.ComputedAt.Format(time.RFC3339))
}

// TestPagination_CountOverWindow seeds 50 entries with the same action and
// verifies that Limit=10, Offset=0 returns exactly 10 entries with total=50.
func TestPagination_CountOverWindow(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	gen := idgen.ULIDGenerator{}
	ctx := context.Background()

	action := fmt.Sprintf("pagination_test_%d", time.Now().UnixNano())
	seedAuditEntries(t, repo, &gen, 50, action, 0)

	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action: &action,
		Limit:  10,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Len(t, results, 10)
	assert.Equal(t, 50, total)
}

// TestTemporalFilter_CreatedAfter seeds entries at two time ranges and verifies
// that CreatedAfter correctly filters to only recent entries.
func TestTemporalFilter_CreatedAfter(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	gen := idgen.ULIDGenerator{}
	ctx := context.Background()

	action := fmt.Sprintf("temporal_test_%d", time.Now().UnixNano())

	// 5 recent entries (within last hour).
	seedAuditEntries(t, repo, &gen, 5, action, 0)
	// 5 old entries (25 hours ago).
	seedAuditEntries(t, repo, &gen, 5, action, 25*time.Hour)

	cutoff := time.Now().Add(-2 * time.Hour)
	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action:       &action,
		CreatedAfter: &cutoff,
		Limit:        0,
	})
	require.NoError(t, err)
	assert.Len(t, results, 5, "should return only 5 recent entries")
	assert.Equal(t, 5, total, "total should be 5 (only recent)")
}

// TestLimitZero_ReturnsAll seeds 30 entries and verifies that Limit=0
// returns the full result set with no LIMIT clause applied.
func TestLimitZero_ReturnsAll(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgAuditEntryRepository(db.Pool)
	gen := idgen.ULIDGenerator{}
	ctx := context.Background()

	action := fmt.Sprintf("limit_zero_test_%d", time.Now().UnixNano())
	seedAuditEntries(t, repo, &gen, 30, action, 0)

	results, total, err := repo.Query(ctx, outbound.AuditFilter{
		Action: &action,
		Limit:  0,
	})
	require.NoError(t, err)
	assert.Len(t, results, 30, "Limit=0 should return all 30 entries")
	assert.Equal(t, 30, total)
}
