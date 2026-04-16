package resilience

import "time"

// BreakerSnapshot is an immutable view of a breaker's state at a point in time.
type BreakerSnapshot struct {
	ToolName             string
	AgentRole            string
	State                BreakerState
	OpenedAt             *time.Time
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	FailureRate          float64
	SampleSize           int
	UpdatedAt            time.Time
}

// BreakerFilter controls ListBreakers queries.
// ToolName and AgentRole are reserved for future use (v1 supports State only).
type BreakerFilter struct {
	State     *BreakerState
	ToolName  *string
	AgentRole *string
}
