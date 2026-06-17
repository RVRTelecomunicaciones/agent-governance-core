package routing

import (
	"context"
	"log/slog"
	"time"

	domainrouting "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

const (
	// DefaultRefreshInterval is how often the collector re-queries the audit trail.
	DefaultRefreshInterval = 5 * time.Minute

	// DefaultStatsWindow is the lookback window for failure data.
	DefaultStatsWindow = 48 * time.Hour
)

// FailureStatsCollector periodically queries the audit trail and builds
// aggregated FailureStats snapshots that it publishes to a FailureStatsStore.
type FailureStatsCollector struct {
	store    *FailureStatsStore
	repo     outbound.AuditEntryRepository
	interval time.Duration
	window   time.Duration
	logger   *slog.Logger
}

// NewFailureStatsCollector creates a new collector.
func NewFailureStatsCollector(
	store *FailureStatsStore,
	repo outbound.AuditEntryRepository,
	interval time.Duration,
	window time.Duration,
	logger *slog.Logger,
) *FailureStatsCollector {
	return &FailureStatsCollector{
		store:    store,
		repo:     repo,
		interval: interval,
		window:   window,
		logger:   logger,
	}
}

// Refresh triggers an immediate stats refresh. Useful for testing.
func (c *FailureStatsCollector) Refresh(ctx context.Context) {
	c.refresh(ctx)
}

// Start runs the collector loop. It performs an immediate refresh, then
// refreshes on each tick until ctx is cancelled.
func (c *FailureStatsCollector) Start(ctx context.Context) {
	c.refresh(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

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
