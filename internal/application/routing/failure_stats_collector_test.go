package routing

import (
	"context"
	"log/slog"
	"testing"
	"time"

	domainaudit "github.com/russellcxl/agent-governance-core/internal/domain/audit"
	domainrouting "github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- inline mock ---

type mockAuditRepo struct {
	entries []*domainaudit.AuditEntry
}

func (m *mockAuditRepo) Append(_ context.Context, _ *domainaudit.AuditEntry) error {
	return nil
}

func (m *mockAuditRepo) Query(_ context.Context, _ outbound.AuditFilter) ([]*domainaudit.AuditEntry, int, error) {
	return m.entries, len(m.entries), nil
}

// --- helper ---

func makeAuditEntry(strategy, role, code, outcome string) *domainaudit.AuditEntry {
	actx := domainaudit.NewAuditContext()
	if strategy != "" {
		actx = actx.WithStrategy(strategy)
	}
	if role != "" {
		actx = actx.WithAgentRole(role)
	}
	if code != "" {
		actx = actx.Set("failure_code", code)
	}

	entry, _ := domainaudit.NewAuditEntry(
		shared.AuditEntryID("01JTEST0000000000000000000"),
		shared.ActorID("system"),
		"attempt_registered",
		outcome,
		actx,
		shared.MustTimestamp(time.Now()),
	)
	return entry
}

// --- store tests ---

func TestStore_NilBeforeRefresh(t *testing.T) {
	store := NewFailureStatsStore()
	assert.Nil(t, store.Get())
}

func TestStore_AtomicUpdate(t *testing.T) {
	store := NewFailureStatsStore()
	stats := &domainrouting.FailureStats{
		ByStrategy: map[domainrouting.RoutingStrategy]domainrouting.FailureRate{
			domainrouting.StrategyDirect: {Total: 10, Failures: 2, Rate: 0.2},
		},
		ComputedAt: time.Now(),
		Window:     48 * time.Hour,
	}

	store.Update(stats)
	got := store.Get()

	require.NotNil(t, got)
	assert.Equal(t, 10, got.ByStrategy[domainrouting.StrategyDirect].Total)
}

// --- collector tests ---

func newTestCollector(entries []*domainaudit.AuditEntry) (*FailureStatsCollector, *FailureStatsStore) {
	store := NewFailureStatsStore()
	repo := &mockAuditRepo{entries: entries}
	logger := slog.Default()
	c := NewFailureStatsCollector(store, repo, DefaultRefreshInterval, DefaultStatsWindow, logger)
	return c, store
}

func TestCollector_RefreshPopulatesAllLevels(t *testing.T) {
	entries := []*domainaudit.AuditEntry{
		makeAuditEntry("direct", "implementer", "tool/shell_timeout", "failure"),
		makeAuditEntry("direct", "implementer", "tool/shell_timeout", "success"),
		makeAuditEntry("direct", "implementer", "runtime/oom", "failure"),
		makeAuditEntry("direct", "reviewer", "tool/shell_timeout", "success"),
		makeAuditEntry("decompose", "architect", "tool/api_error", "failure"),
		makeAuditEntry("decompose", "architect", "tool/api_error", "success"),
	}

	c, store := newTestCollector(entries)
	c.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats)

	// ByStrategy
	assert.Equal(t, 4, stats.ByStrategy[domainrouting.StrategyDirect].Total)
	assert.Equal(t, 2, stats.ByStrategy[domainrouting.StrategyDirect].Failures)
	assert.Equal(t, 2, stats.ByStrategy[domainrouting.RoutingStrategy("decompose")].Total)

	// ByStrategyCategory
	toolDirect := domainrouting.StatsKey{Strategy: "direct", Category: "tool"}
	assert.Equal(t, 3, stats.ByStrategyCategory[toolDirect].Total)
	assert.Equal(t, 1, stats.ByStrategyCategory[toolDirect].Failures)

	runtimeDirect := domainrouting.StatsKey{Strategy: "direct", Category: "runtime"}
	assert.Equal(t, 1, stats.ByStrategyCategory[runtimeDirect].Total)

	// ByStrategyRoleCategory
	implTool := domainrouting.StatsKey{Strategy: "direct", Role: "implementer", Category: "tool"}
	assert.Equal(t, 2, stats.ByStrategyRoleCategory[implTool].Total)
	assert.Equal(t, 1, stats.ByStrategyRoleCategory[implTool].Failures)
}

func TestCollector_EmptyResults(t *testing.T) {
	c, store := newTestCollector(nil)
	c.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats)
	assert.Empty(t, stats.ByStrategy)
	assert.Empty(t, stats.ByStrategyCategory)
	assert.Empty(t, stats.ByStrategyRoleCategory)
}

func TestCollector_SkipsEntriesWithoutStrategy(t *testing.T) {
	entries := []*domainaudit.AuditEntry{
		makeAuditEntry("", "implementer", "tool/timeout", "failure"),
		makeAuditEntry("direct", "implementer", "tool/timeout", "success"),
	}

	c, store := newTestCollector(entries)
	c.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats)
	assert.Len(t, stats.ByStrategy, 1)
	assert.Equal(t, 1, stats.ByStrategy[domainrouting.StrategyDirect].Total)
	assert.Equal(t, 0, stats.ByStrategy[domainrouting.StrategyDirect].Failures)
}

func TestCollector_UsesExtractCategory(t *testing.T) {
	entries := []*domainaudit.AuditEntry{
		makeAuditEntry("direct", "implementer", "tool/shell_timeout", "failure"),
	}

	c, store := newTestCollector(entries)
	c.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats)

	// ExtractCategory("tool/shell_timeout") should yield "tool"
	key := domainrouting.StatsKey{Strategy: "direct", Category: "tool"}
	fr, ok := stats.ByStrategyCategory[key]
	require.True(t, ok, "expected category 'tool' from 'tool/shell_timeout'")
	assert.Equal(t, 1, fr.Total)
	assert.Equal(t, 1, fr.Failures)
}
