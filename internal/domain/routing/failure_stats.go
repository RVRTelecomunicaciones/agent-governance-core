package routing

import "time"

// StatsKey identifies a failure rate bucket by strategy, role, and category.
type StatsKey struct {
	Strategy RoutingStrategy
	Role     string
	Category string
}

// FailureRate holds total executions, failure count, and pre-computed rate.
type FailureRate struct {
	Total    int
	Failures int
	Rate     float64
}

// FailureStats holds pre-aggregated failure data at multiple granularity levels.
type FailureStats struct {
	ByStrategyRoleCategory map[StatsKey]FailureRate
	ByStrategyCategory     map[StatsKey]FailureRate
	ByStrategy             map[RoutingStrategy]FailureRate
	ComputedAt             time.Time
	Window                 time.Duration
}
