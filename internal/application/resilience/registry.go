package resilience

import (
	"sync"
	"time"
)

// CircuitBreakerRegistry holds all active circuit breakers in memory.
// State is not persisted — on restart, the registry begins empty.
type CircuitBreakerRegistry struct {
	mu                 sync.Mutex
	breakers           map[BreakerKey]*CircuitBreaker
	transitionListener TransitionListener
}

// TransitionListener is invoked on every state transition.
type TransitionListener func(t StateTransition)

// NewCircuitBreakerRegistry creates an empty registry.
func NewCircuitBreakerRegistry() *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[BreakerKey]*CircuitBreaker),
	}
}

// SetTransitionListener sets a listener called on every transition.
// Intended to be called once at wiring time.
func (r *CircuitBreakerRegistry) SetTransitionListener(listener TransitionListener) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transitionListener = listener
}

// Get returns a snapshot of the breaker for (tool, role), or nil if none exists.
func (r *CircuitBreakerRegistry) Get(tool, role string) *BreakerSnapshot {
	key := BreakerKey{ToolName: tool, AgentRole: role}

	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.breakers[key]
	if !ok {
		return nil
	}
	snap := snapshot(b)
	return &snap
}

// List returns snapshots of all breakers, optionally filtered.
func (r *CircuitBreakerRegistry) List(filter BreakerFilter) []BreakerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]BreakerSnapshot, 0, len(r.breakers))
	for _, b := range r.breakers {
		if filter.State != nil && b.State != *filter.State {
			continue
		}
		if filter.ToolName != nil && b.Key.ToolName != *filter.ToolName {
			continue
		}
		if filter.AgentRole != nil && b.Key.AgentRole != *filter.AgentRole {
			continue
		}
		out = append(out, snapshot(b))
	}
	return out
}

// snapshot converts an internal *CircuitBreaker into an immutable BreakerSnapshot.
// Must be called while holding r.mu to safely read breaker fields.
func snapshot(b *CircuitBreaker) BreakerSnapshot {
	var openedAt *time.Time
	if b.OpenedAt != nil {
		t := *b.OpenedAt
		openedAt = &t
	}

	sampleSize := len(b.RingBuffer)
	var failureRate float64
	if sampleSize > 0 {
		failures := 0
		for _, isFailure := range b.RingBuffer {
			if isFailure {
				failures++
			}
		}
		failureRate = float64(failures) / float64(sampleSize)
	}

	return BreakerSnapshot{
		ToolName:             b.Key.ToolName,
		AgentRole:            b.Key.AgentRole,
		State:                b.State,
		OpenedAt:             openedAt,
		ConsecutiveFailures:  b.ConsecutiveFailures,
		ConsecutiveSuccesses: b.ConsecutiveSuccesses,
		FailureRate:          failureRate,
		SampleSize:           sampleSize,
		UpdatedAt:            b.UpdatedAt,
	}
}
