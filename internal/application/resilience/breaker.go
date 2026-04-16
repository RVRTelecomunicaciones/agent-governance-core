package resilience

import "time"

// Breaker thresholds — constants in v1, configurable later.
const (
	ConsecutiveFailuresThreshold = 3
	RatioFailureThreshold        = 0.5
	RatioWindowSize              = 20
	RatioMinSamples              = 5
	OpenCooldown                 = 60 * time.Second
	HalfOpenProbeSuccesses       = 3
)

// BreakerState represents the current state of a circuit breaker.
type BreakerState string

const (
	StateClosed   BreakerState = "closed"
	StateOpen     BreakerState = "open"
	StateHalfOpen BreakerState = "half_open"
)

// TripReason identifies which condition caused CLOSED → OPEN.
type TripReason string

const (
	TripReasonConsecutive TripReason = "consecutive"
	TripReasonRatio       TripReason = "ratio"
)

// BreakerKey identifies a circuit breaker uniquely by (tool, agent_role).
type BreakerKey struct {
	ToolName  string
	AgentRole string
}

// CircuitBreaker holds the state for one (tool, agent_role) combination.
type CircuitBreaker struct {
	Key                  BreakerKey
	State                BreakerState
	OpenedAt             *time.Time
	ConsecutiveFailures  int
	ConsecutiveSuccesses int    // used during HALF_OPEN
	RingBuffer           []bool // last N: true=failure, false=success (oldest first)
	UpdatedAt            time.Time
}

// StateTransition describes a state change produced by an Observe call.
type StateTransition struct {
	Key                  BreakerKey
	From                 BreakerState
	To                   BreakerState
	TripReason           TripReason // only set when CLOSED → OPEN
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	FailureRate          float64
	SampleSize           int
	At                   time.Time
}

// NewCircuitBreaker creates a breaker in CLOSED state.
func NewCircuitBreaker(key BreakerKey) *CircuitBreaker {
	return &CircuitBreaker{
		Key:        key,
		State:      StateClosed,
		RingBuffer: make([]bool, 0, RatioWindowSize),
		UpdatedAt:  time.Now(),
	}
}

// Observe records an attempt result and returns a transition if state changed.
// Returns nil when no state change occurred.
func (b *CircuitBreaker) Observe(success bool, at time.Time) *StateTransition {
	from := b.State
	b.UpdatedAt = at

	// Update ring buffer
	b.appendToRingBuffer(!success)

	// Update counters
	if success {
		b.ConsecutiveFailures = 0
		if b.State == StateHalfOpen {
			b.ConsecutiveSuccesses++
		} else {
			b.ConsecutiveSuccesses = 0
		}
	} else {
		b.ConsecutiveFailures++
		b.ConsecutiveSuccesses = 0
	}

	// Evaluate transitions based on current state
	switch b.State {
	case StateClosed:
		if tripReason, shouldTrip := b.evaluateTrip(); shouldTrip {
			b.State = StateOpen
			b.OpenedAt = &at
			return b.makeTransition(from, StateOpen, tripReason, at)
		}

	case StateOpen:
		// Check if cooldown elapsed
		if b.OpenedAt != nil && at.Sub(*b.OpenedAt) >= OpenCooldown {
			// Transition to HALF_OPEN, then process the current observation
			b.State = StateHalfOpen
			b.ConsecutiveSuccesses = 0
			if success {
				b.ConsecutiveSuccesses = 1
			}

			// If the current observation is a failure, re-trip immediately
			if !success {
				b.State = StateOpen
				b.OpenedAt = &at
				b.ConsecutiveSuccesses = 0
				return b.makeTransition(StateHalfOpen, StateOpen, "", at)
			}

			// Check if this single success is enough (it isn't — need 3)
			if b.ConsecutiveSuccesses >= HalfOpenProbeSuccesses {
				b.State = StateClosed
				b.OpenedAt = nil
				b.ConsecutiveSuccesses = 0
				return b.makeTransition(from, StateClosed, "", at)
			}
			return b.makeTransition(from, StateHalfOpen, "", at)
		}
		// Still in OPEN, no transition
		return nil

	case StateHalfOpen:
		if !success {
			// Failure in HALF_OPEN → back to OPEN, reset cooldown
			b.State = StateOpen
			b.OpenedAt = &at
			b.ConsecutiveSuccesses = 0
			return b.makeTransition(from, StateOpen, "", at)
		}
		// Success — check if we've reached the probe threshold.
		// Capture counter BEFORE reset so the transition reports the threshold value.
		if b.ConsecutiveSuccesses >= HalfOpenProbeSuccesses {
			b.State = StateClosed
			b.OpenedAt = nil
			transition := b.makeTransition(from, StateClosed, "", at)
			b.ConsecutiveSuccesses = 0
			return transition
		}
		return nil
	}

	return nil
}

// evaluateTrip checks whether CLOSED state should trip to OPEN.
func (b *CircuitBreaker) evaluateTrip() (TripReason, bool) {
	// Consecutive threshold
	if b.ConsecutiveFailures >= ConsecutiveFailuresThreshold {
		return TripReasonConsecutive, true
	}

	// Ratio threshold
	sampleSize := len(b.RingBuffer)
	if sampleSize >= RatioMinSamples {
		failures := 0
		for _, isFailure := range b.RingBuffer {
			if isFailure {
				failures++
			}
		}
		rate := float64(failures) / float64(sampleSize)
		if rate > RatioFailureThreshold {
			return TripReasonRatio, true
		}
	}

	return "", false
}

// appendToRingBuffer adds an entry, trimming oldest when full.
func (b *CircuitBreaker) appendToRingBuffer(isFailure bool) {
	if len(b.RingBuffer) >= RatioWindowSize {
		b.RingBuffer = b.RingBuffer[1:]
	}
	b.RingBuffer = append(b.RingBuffer, isFailure)
}

// failureRate returns the current ratio in the ring buffer.
func (b *CircuitBreaker) failureRate() float64 {
	if len(b.RingBuffer) == 0 {
		return 0
	}
	failures := 0
	for _, f := range b.RingBuffer {
		if f {
			failures++
		}
	}
	return float64(failures) / float64(len(b.RingBuffer))
}

func (b *CircuitBreaker) makeTransition(from, to BreakerState, reason TripReason, at time.Time) *StateTransition {
	return &StateTransition{
		Key:                  b.Key,
		From:                 from,
		To:                   to,
		TripReason:           reason,
		ConsecutiveFailures:  b.ConsecutiveFailures,
		ConsecutiveSuccesses: b.ConsecutiveSuccesses,
		FailureRate:          b.failureRate(),
		SampleSize:           len(b.RingBuffer),
		At:                   at,
	}
}
