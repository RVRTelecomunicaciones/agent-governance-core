# Phase 3 Track 2.2: Circuit Breaker — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an in-memory circuit breaker registry per `(tool_name, agent_role)` that observes failure patterns, exposes state transitions via audit + metrics + tracing + HTTP + SDK, and acts as a SIGNAL (not enforcement).

**Architecture:** New `internal/application/resilience/` package with state machine (CLOSED/OPEN/HALF_OPEN) + thread-safe registry (map+mutex). Integrated into `RegisterAttempt` as a side-effect observer. Observability via 5 surfaces. In-memory only — resets on restart (deliberate).

**Tech Stack:** Go 1.26.2, sync/atomic, no new dependencies

**Spec:** `docs/superpowers/specs/2026-04-16-phase3-track2-circuit-breaker-design.md`

**Baseline invariant:** All existing tests must pass. RegisterAttempt semantics unchanged. No database migrations.

---

## File Structure

```
internal/
  application/
    resilience/
      breaker.go                    — NEW: CircuitBreaker state + transitions
      breaker_test.go                — NEW
      registry.go                    — NEW: CircuitBreakerRegistry (map+mutex)
      registry_test.go                — NEW
      observe.go                     — NEW: Observe() hook called from RegisterAttempt
      snapshot.go                    — NEW: BreakerSnapshot DTO + filter
    workflowrun/
      service.go                     — MODIFY: add breakerRegistry dependency
      register_attempt.go            — MODIFY: call breakerRegistry.Observe(...)
  adapters/
    inbound/
      http/
        breaker_handler.go           — NEW: handleListBreakers
        router.go                    — MODIFY: add GET /api/v1/breakers route
      sdk/
        facade.go                    — MODIFY: ListBreakers, GetBreakerState
      metrics/
        instruments.go               — MODIFY: CircuitBreakerTransitions, CircuitBreakerTrips
  ports/
    inbound/
      query_service.go               — MODIFY: ListBreakers, GetBreakerState
  bootstrap/
    wire.go                          — MODIFY: construct registry, emit startup audit
test/integration/
  circuitbreaker/
    breaker_test.go                  — NEW: e2e tests with real PG
```

---

## Task 1: Breaker State Machine (B1)

**Files:**
- Create: `internal/application/resilience/breaker.go`
- Create: `internal/application/resilience/breaker_test.go`

This task builds the pure state machine with zero dependencies. No registry, no audit, no metrics — just the CircuitBreaker type and its transition logic.

- [ ] **Step 1: Write failing tests for state machine**

```go
// internal/application/resilience/breaker_test.go
package resilience

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBreaker_InitialState_Closed(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	assert.Equal(t, StateClosed, b.State)
	assert.Equal(t, 0, b.ConsecutiveFailures)
	assert.Nil(t, b.OpenedAt)
}

func TestBreaker_ConsecutiveFailuresTrip(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	now := time.Now()

	// 3 consecutive failures → OPEN
	transition := b.Observe(false, now)
	assert.Nil(t, transition, "1st failure should not trip")
	transition = b.Observe(false, now.Add(time.Second))
	assert.Nil(t, transition, "2nd failure should not trip")
	transition = b.Observe(false, now.Add(2*time.Second))
	require := func(t *testing.T, cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	require(t, transition != nil, "3rd failure should trip")
	assert.Equal(t, StateClosed, transition.From)
	assert.Equal(t, StateOpen, transition.To)
	assert.Equal(t, TripReasonConsecutive, transition.TripReason)
	assert.Equal(t, StateOpen, b.State)
	assert.NotNil(t, b.OpenedAt)
}

func TestBreaker_RatioTrip(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "api", AgentRole: "reviewer"})
	now := time.Now()

	// Alternate success/failure to avoid consecutive trip.
	// Pattern: S F S F F F F F F F F F = 11 failures / 12 attempts over 12 events
	// But consecutive would trip first with 3 failures. So we need to break consecutive
	// with successes while accumulating enough failures for ratio > 0.5 over 20.
	// We need: sample_size >= 5, failure_rate > 0.5, avoid 3 consecutive failures.
	// Pattern: F F S F F S F F S F F S F F S  = 10F/5S = 15 events, ratio 10/15 = 0.67
	// But 2 consecutive max — OK.
	pattern := []bool{
		false, false, true, // 2 fails, break with success
		false, false, true,
		false, false, true,
		false, false, true,
		false, false, true,
	}
	var last *StateTransition
	for i, isSuccess := range pattern {
		last = b.Observe(isSuccess, now.Add(time.Duration(i)*time.Second))
	}

	// After this pattern, failure_rate = 10/15 = 0.67 > 0.5, sample >= 5
	// Should be OPEN via ratio (consecutive was reset by successes)
	assert.Equal(t, StateOpen, b.State, "ratio should trip breaker")
	require := func(t *testing.T, cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	require(t, last != nil, "last observation should produce a transition")
	assert.Equal(t, TripReasonRatio, last.TripReason)
}

func TestBreaker_NoTripBelowMinSamples(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "api", AgentRole: "reviewer"})
	now := time.Now()

	// 2 failures only — below min_samples=5, and below consecutive=3
	b.Observe(false, now)
	b.Observe(false, now.Add(time.Second))

	assert.Equal(t, StateClosed, b.State, "below min_samples, should stay CLOSED")
}

func TestBreaker_OpenToHalfOpen_AfterCooldown(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	now := time.Now()

	// Trip the breaker
	b.Observe(false, now)
	b.Observe(false, now.Add(time.Second))
	b.Observe(false, now.Add(2*time.Second))
	assert.Equal(t, StateOpen, b.State)

	// Wait past cooldown (60s), then observe
	afterCooldown := now.Add(OpenCooldown + time.Second)
	transition := b.Observe(false, afterCooldown)

	// OPEN → HALF_OPEN, then the failure flips HALF_OPEN → OPEN
	// So we expect two transitions in one observation... but we emit only the last one
	// Actually per spec: cooldown elapsed → transition to HALF_OPEN on next observation,
	// then the failure evaluates. We need to think about this carefully.
	//
	// Per spec: "After 60 seconds in OPEN state, the next observation transitions to HALF_OPEN."
	// Then the observation itself is processed. If it's a failure in HALF_OPEN → OPEN again.
	//
	// So the transition emitted should reflect the final state.
	// We test this by checking the final state:
	require := func(t *testing.T, cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	require(t, transition != nil, "should produce a transition")
	// Transition chain: OPEN → HALF_OPEN → OPEN. The final state is OPEN.
	// The transition emitted captures the "meaningful" change. We emit the HALF_OPEN→OPEN
	// (the failure caused re-trip) since the cooldown transition is bookkeeping.
	// For simplicity, the breaker may emit the OPEN→HALF_OPEN and let RegisterAttempt
	// observe whether a subsequent failure flips it back.
	// We'll define: Observe returns the NET final transition from the starting state.
	// Starting state: OPEN. Final state: OPEN. If they match, transition may be nil.
	// BUT the cooldown crossing is still an important event.
	//
	// To keep tests deterministic, use a success attempt for the cooldown test:
}

func TestBreaker_OpenToHalfOpen_WithSuccessAfterCooldown(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	now := time.Now()

	// Trip the breaker
	b.Observe(false, now)
	b.Observe(false, now.Add(time.Second))
	b.Observe(false, now.Add(2*time.Second))
	assert.Equal(t, StateOpen, b.State)

	// Wait past cooldown, then observe a success
	afterCooldown := now.Add(OpenCooldown + time.Second)
	transition := b.Observe(true, afterCooldown)

	require := func(t *testing.T, cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	require(t, transition != nil, "should transition out of OPEN")
	assert.Equal(t, StateOpen, transition.From)
	assert.Equal(t, StateHalfOpen, transition.To)
	assert.Equal(t, StateHalfOpen, b.State)
	assert.Equal(t, 1, b.ConsecutiveSuccesses)
}

func TestBreaker_HalfOpenToClosed_ThreeSuccesses(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	now := time.Now()

	// Trip
	for i := 0; i < 3; i++ {
		b.Observe(false, now.Add(time.Duration(i)*time.Second))
	}

	// Cooldown elapsed
	t0 := now.Add(OpenCooldown + time.Second)
	b.Observe(true, t0)
	assert.Equal(t, StateHalfOpen, b.State)
	assert.Equal(t, 1, b.ConsecutiveSuccesses)

	b.Observe(true, t0.Add(time.Second))
	assert.Equal(t, StateHalfOpen, b.State)
	assert.Equal(t, 2, b.ConsecutiveSuccesses)

	transition := b.Observe(true, t0.Add(2*time.Second))
	require := func(t *testing.T, cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	require(t, transition != nil, "3rd success should close breaker")
	assert.Equal(t, StateHalfOpen, transition.From)
	assert.Equal(t, StateClosed, transition.To)
	assert.Equal(t, StateClosed, b.State)
	assert.Equal(t, 3, transition.ConsecutiveSuccesses)
}

func TestBreaker_HalfOpenToOpen_OnFailure(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	now := time.Now()

	// Trip
	for i := 0; i < 3; i++ {
		b.Observe(false, now.Add(time.Duration(i)*time.Second))
	}

	// Enter half-open via success
	t0 := now.Add(OpenCooldown + time.Second)
	b.Observe(true, t0)
	assert.Equal(t, StateHalfOpen, b.State)

	// Single failure → back to OPEN
	transition := b.Observe(false, t0.Add(time.Second))
	require := func(t *testing.T, cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	require(t, transition != nil, "failure in half-open should re-open")
	assert.Equal(t, StateHalfOpen, transition.From)
	assert.Equal(t, StateOpen, transition.To)
	assert.Equal(t, StateOpen, b.State)

	// Cooldown should be reset
	expectedOpenedAt := t0.Add(time.Second)
	assert.Equal(t, expectedOpenedAt, *b.OpenedAt)
}

func TestBreaker_SuccessResetsConsecutiveFailures(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "api", AgentRole: "reviewer"})
	now := time.Now()

	b.Observe(false, now)
	b.Observe(false, now.Add(time.Second))
	assert.Equal(t, 2, b.ConsecutiveFailures)

	b.Observe(true, now.Add(2*time.Second))
	assert.Equal(t, 0, b.ConsecutiveFailures)
	assert.Equal(t, StateClosed, b.State)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/application/resilience/... -v -count=1`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement breaker.go with state machine**

```go
// internal/application/resilience/breaker.go
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
		// Success — check if we've reached the probe threshold
		if b.ConsecutiveSuccesses >= HalfOpenProbeSuccesses {
			b.State = StateClosed
			b.OpenedAt = nil
			b.ConsecutiveSuccesses = 0
			return b.makeTransition(from, StateClosed, "", at)
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/application/resilience/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Verify no regressions**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/application/resilience/breaker.go internal/application/resilience/breaker_test.go
git commit -m "feat(breaker): add circuit breaker state machine with hybrid trip"
```

---

## Task 2: Registry + Observe Hook (B2)

**Files:**
- Create: `internal/application/resilience/registry.go`
- Create: `internal/application/resilience/observe.go`
- Create: `internal/application/resilience/snapshot.go`
- Create: `internal/application/resilience/registry_test.go`

- [ ] **Step 1: Implement snapshot DTOs**

```go
// internal/application/resilience/snapshot.go
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
```

- [ ] **Step 2: Write failing registry tests**

```go
// internal/application/resilience/registry_test.go
package resilience

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_CreatesBreakerOnFirstObserve(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()

	_, err := r.Observe(ctx, "shell", "implementer", true, time.Now())
	require.NoError(t, err)

	snap := r.Get("shell", "implementer")
	require.NotNil(t, snap)
	assert.Equal(t, "shell", snap.ToolName)
	assert.Equal(t, "implementer", snap.AgentRole)
	assert.Equal(t, StateClosed, snap.State)
}

func TestRegistry_ReusesExistingBreaker(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	r.Observe(ctx, "shell", "implementer", false, now)
	r.Observe(ctx, "shell", "implementer", false, now.Add(time.Second))
	snap := r.Get("shell", "implementer")
	require.NotNil(t, snap)
	assert.Equal(t, 2, snap.ConsecutiveFailures)
}

func TestRegistry_IndependentPerKey(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// Trip one
	for i := 0; i < 3; i++ {
		r.Observe(ctx, "shell", "implementer", false, now.Add(time.Duration(i)*time.Second))
	}
	// Leave the other CLOSED
	r.Observe(ctx, "git", "reviewer", true, now)

	shellSnap := r.Get("shell", "implementer")
	gitSnap := r.Get("git", "reviewer")
	assert.Equal(t, StateOpen, shellSnap.State)
	assert.Equal(t, StateClosed, gitSnap.State)
}

func TestRegistry_SkipsWhenToolNameEmpty(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()

	transition, err := r.Observe(ctx, "", "implementer", false, time.Now())
	require.NoError(t, err)
	assert.Nil(t, transition)

	// No breaker should be created
	all := r.List(BreakerFilter{})
	assert.Empty(t, all)
}

func TestRegistry_SkipsWhenAgentRoleEmpty(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()

	transition, err := r.Observe(ctx, "shell", "", false, time.Now())
	require.NoError(t, err)
	assert.Nil(t, transition)

	all := r.List(BreakerFilter{})
	assert.Empty(t, all)
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()

	var wg sync.WaitGroup
	// 100 goroutines observing different keys in parallel
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tool := "tool" + string(rune('a'+n%5))
			role := "role" + string(rune('a'+n%3))
			r.Observe(ctx, tool, role, n%2 == 0, time.Now())
		}(i)
	}
	wg.Wait()

	// If the test doesn't race or panic, concurrency is safe.
	all := r.List(BreakerFilter{})
	assert.NotEmpty(t, all)
}

func TestRegistry_List_FilterByState(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// Trip one breaker
	for i := 0; i < 3; i++ {
		r.Observe(ctx, "shell", "implementer", false, now.Add(time.Duration(i)*time.Second))
	}
	// Leave another CLOSED
	r.Observe(ctx, "git", "reviewer", true, now)

	openState := StateOpen
	openBreakers := r.List(BreakerFilter{State: &openState})
	assert.Len(t, openBreakers, 1)
	assert.Equal(t, "shell", openBreakers[0].ToolName)

	closedState := StateClosed
	closedBreakers := r.List(BreakerFilter{State: &closedState})
	assert.Len(t, closedBreakers, 1)
	assert.Equal(t, "git", closedBreakers[0].ToolName)

	all := r.List(BreakerFilter{})
	assert.Len(t, all, 2)
}

func TestRegistry_Get_MissingReturnsNil(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	snap := r.Get("nonexistent", "none")
	assert.Nil(t, snap)
}
```

- [ ] **Step 3: Implement registry + observe**

```go
// internal/application/resilience/registry.go
package resilience

import (
	"sync"
	"time"
)

// CircuitBreakerRegistry holds all active circuit breakers in memory.
// State is not persisted — on restart, the registry begins empty.
type CircuitBreakerRegistry struct {
	mu       sync.Mutex
	breakers map[BreakerKey]*CircuitBreaker
	// transitionListener is invoked (synchronously) whenever a breaker state changes.
	// May be nil; when non-nil, called inside the mutex — keep it fast and non-blocking.
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
// Intended to be called once at wiring time. Not safe under concurrent sets.
func (r *CircuitBreakerRegistry) SetTransitionListener(listener TransitionListener) {
	r.transitionListener = listener
}

// Get returns a snapshot of the breaker for (tool, role), or nil if none exists.
func (r *CircuitBreakerRegistry) Get(tool, role string) *BreakerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := BreakerKey{ToolName: tool, AgentRole: role}
	b, ok := r.breakers[key]
	if !ok {
		return nil
	}
	return snapshot(b)
}

// List returns snapshots of all breakers, optionally filtered.
func (r *CircuitBreakerRegistry) List(filter BreakerFilter) []BreakerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]BreakerSnapshot, 0, len(r.breakers))
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
		result = append(result, *snapshot(b))
	}
	return result
}

func snapshot(b *CircuitBreaker) *BreakerSnapshot {
	failures := 0
	for _, f := range b.RingBuffer {
		if f {
			failures++
		}
	}
	var rate float64
	if len(b.RingBuffer) > 0 {
		rate = float64(failures) / float64(len(b.RingBuffer))
	}
	var openedAt *time.Time
	if b.OpenedAt != nil {
		v := *b.OpenedAt
		openedAt = &v
	}
	return &BreakerSnapshot{
		ToolName:             b.Key.ToolName,
		AgentRole:            b.Key.AgentRole,
		State:                b.State,
		OpenedAt:             openedAt,
		ConsecutiveFailures:  b.ConsecutiveFailures,
		ConsecutiveSuccesses: b.ConsecutiveSuccesses,
		FailureRate:          rate,
		SampleSize:           len(b.RingBuffer),
		UpdatedAt:            b.UpdatedAt,
	}
}
```

```go
// internal/application/resilience/observe.go
package resilience

import (
	"context"
	"time"
)

// Observe records an attempt against the breaker for (tool, role) and returns a
// transition if state changed. Returns nil if tool or role is empty (skip).
func (r *CircuitBreakerRegistry) Observe(ctx context.Context, tool, role string, success bool, at time.Time) (*StateTransition, error) {
	if tool == "" || role == "" {
		return nil, nil // skip — no identifiable combination
	}
	_ = ctx // reserved for future (tracing, tenant context, etc.)

	key := BreakerKey{ToolName: tool, AgentRole: role}

	r.mu.Lock()
	b, ok := r.breakers[key]
	if !ok {
		b = NewCircuitBreaker(key)
		r.breakers[key] = b
	}
	transition := b.Observe(success, at)
	listener := r.transitionListener
	r.mu.Unlock()

	if transition != nil && listener != nil {
		listener(*transition)
	}
	return transition, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/application/resilience/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/application/resilience/registry.go internal/application/resilience/observe.go internal/application/resilience/snapshot.go internal/application/resilience/registry_test.go
git commit -m "feat(breaker): add CircuitBreakerRegistry with map+mutex and transition listener"
```

---

## Task 3: RegisterAttempt Integration (B3)

**Files:**
- Modify: `internal/application/workflowrun/service.go`
- Modify: `internal/application/workflowrun/register_attempt.go`

- [ ] **Step 1: Add breakerRegistry dependency to WorkflowRunService**

Modify `internal/application/workflowrun/service.go`:

```go
package workflowrun

import (
	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

type WorkflowRunService struct {
	wfRepo           outbound.WorkflowRunRepository
	leaseRepo        outbound.ExecutionLeaseRepository
	taskRepo         outbound.TaskRepository
	routingRepo      outbound.RoutingDecisionRepository
	policyRepo       outbound.PolicyDecisionRepository
	approvalRepo     outbound.ApprovalRequestRepository
	idGen            outbound.IDGenerator
	clock            outbound.Clock
	audit            outbound.AuditRecorder
	notifier         outbound.GovernanceNotifier
	lifecycleMetrics *LifecycleMetrics
	breakerRegistry  *resilience.CircuitBreakerRegistry // NEW — may be nil
}

func NewWorkflowRunService(
	wfRepo outbound.WorkflowRunRepository,
	leaseRepo outbound.ExecutionLeaseRepository,
	taskRepo outbound.TaskRepository,
	routingRepo outbound.RoutingDecisionRepository,
	policyRepo outbound.PolicyDecisionRepository,
	approvalRepo outbound.ApprovalRequestRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	audit outbound.AuditRecorder,
	notifier outbound.GovernanceNotifier,
	lifecycleMetrics *LifecycleMetrics,
	breakerRegistry *resilience.CircuitBreakerRegistry, // NEW
) *WorkflowRunService {
	return &WorkflowRunService{
		wfRepo:           wfRepo,
		leaseRepo:        leaseRepo,
		taskRepo:         taskRepo,
		routingRepo:      routingRepo,
		policyRepo:       policyRepo,
		approvalRepo:     approvalRepo,
		idGen:            idGen,
		clock:            clock,
		audit:            audit,
		notifier:         notifier,
		lifecycleMetrics: lifecycleMetrics,
		breakerRegistry:  breakerRegistry,
	}
}
```

The new parameter is **optional** (may be nil). When nil, the breaker integration is a no-op, ensuring backwards compatibility.

- [ ] **Step 2: Add breaker observation in RegisterAttempt**

Modify `internal/application/workflowrun/register_attempt.go`. After the lifecycle metrics block, add:

```go
// Observe circuit breaker (signal-only, side-effect)
if s.breakerRegistry != nil && result.ToolName != nil && result.AgentRole != nil {
	success := result.Status == execution.AttemptStatusSuccess
	// Ignore error and transition return here — listener will handle side effects.
	_, _ = s.breakerRegistry.Observe(ctx, *result.ToolName, *result.AgentRole, success, now.Time)
}
```

Note: `now` is already in scope as `shared.Timestamp`. Use `now.Time` to get the underlying `time.Time`.

- [ ] **Step 3: Update ALL existing call sites to pass nil for breakerRegistry**

The constructor signature changed. Find all callers:

```bash
rg "NewWorkflowRunService\(" --files-with-matches
```

Update each caller to pass `nil` as the last argument:
- `internal/bootstrap/wire.go` (will be updated properly in Task 6)
- `internal/application/workflowrun/service_test.go` (if any tests construct directly)
- `test/integration/usecases/process_task_test.go`
- `test/integration/adaptive/adaptive_routing_test.go`
- `test/integration/quarantine/quarantine_test.go`
- Any others found by grep

- [ ] **Step 4: Add a unit test verifying Observe is called from RegisterAttempt**

Add to `internal/application/workflowrun/service_test.go` (or wherever the WorkflowRunService tests live):

```go
func TestRegisterAttempt_ObservesBreakerOnFailure(t *testing.T) {
	// Setup: build service with a real CircuitBreakerRegistry
	registry := resilience.NewCircuitBreakerRegistry()
	h := newServiceTestHarness(t)
	h.svc.breakerRegistry = registry // inject

	// Seed a workflow in running state
	wf := // ... setup a running workflow
	// ...

	// Register a failure with tool+role
	stage := shared.StageRuntime
	code := "tool/shell_timeout"
	retryable := true
	tool := "shell"
	role := "implementer"
	result := execution.AttemptResult{
		Status:       execution.AttemptStatusFailure,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
		ToolName:     &tool,
		AgentRole:    &role,
	}

	_, err := h.svc.RegisterAttempt(context.Background(), wf.ID, result)
	require.NoError(t, err)

	// Verify breaker was observed
	snap := registry.Get("shell", "implementer")
	require.NotNil(t, snap)
	assert.Equal(t, 1, snap.ConsecutiveFailures)
}

func TestRegisterAttempt_SkipsBreakerWhenRegistryNil(t *testing.T) {
	h := newServiceTestHarness(t)
	h.svc.breakerRegistry = nil // no registry

	// Should not panic, should proceed normally
	wf := // ... setup a running workflow
	result := // ... failure with tool+role
	_, err := h.svc.RegisterAttempt(context.Background(), wf.ID, result)
	require.NoError(t, err)
}

func TestRegisterAttempt_SkipsBreakerWhenToolNameMissing(t *testing.T) {
	registry := resilience.NewCircuitBreakerRegistry()
	h := newServiceTestHarness(t)
	h.svc.breakerRegistry = registry

	// Result without ToolName — should skip breaker
	result := // ... failure without tool
	_, err := h.svc.RegisterAttempt(context.Background(), wf.ID, result)
	require.NoError(t, err)

	all := registry.List(resilience.BreakerFilter{})
	assert.Empty(t, all, "no breaker should be created without tool_name")
}
```

Note: adapt the test helpers to the actual test setup pattern in the file. READ the existing `service_test.go` before writing tests to use the correct harness/mock patterns.

- [ ] **Step 5: Run build + all tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS (zero regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/application/workflowrun/ internal/bootstrap/wire.go test/integration/
git commit -m "feat(breaker): wire CircuitBreakerRegistry into RegisterAttempt (signal observation)"
```

---

## Task 4: Observability — Audit Entries + Metrics (B4)

**Files:**
- Modify: `internal/adapters/inbound/metrics/instruments.go`
- Create: `internal/application/resilience/audit_listener.go`
- Create: `internal/application/resilience/metrics_listener.go`

The transition listener from Task 2 is the hook for side effects. We implement two listeners: one for audit, one for metrics.

- [ ] **Step 1: Add breaker metric instruments**

Modify `internal/adapters/inbound/metrics/instruments.go`:

Add to the `Instruments` struct:
```go
CircuitBreakerTransitions metric.Int64Counter
CircuitBreakerTrips       metric.Int64Counter
```

Add to `NewInstruments`:
```go
cbTransitions, err := meter.Int64Counter("governance.circuit_breaker.transitions",
	metric.WithDescription("Number of circuit breaker state transitions"),
)
if err != nil {
	return nil, err
}

cbTrips, err := meter.Int64Counter("governance.circuit_breaker.trips",
	metric.WithDescription("Number of CLOSED→OPEN transitions by trip reason"),
)
if err != nil {
	return nil, err
}
```

Wire them into the returned `&Instruments{...}` struct.

- [ ] **Step 2: Implement audit listener**

```go
// internal/application/resilience/audit_listener.go
package resilience

import (
	"context"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// AuditListener returns a TransitionListener that writes audit entries.
// Each transition becomes one audit entry. Not tied to task/workflow.
func AuditListener(recorder outbound.AuditRecorder) TransitionListener {
	return func(t StateTransition) {
		// Audit call uses background context — listener runs inside registry mutex
		// and we don't want to block on ctx cancellation of a past request.
		ctx := context.Background()
		action, outcome := actionAndOutcome(t)
		actx := audit.NewAuditContext().
			Set("tool_name", t.Key.ToolName).
			Set("agent_role", t.Key.AgentRole).
			Set("from_state", string(t.From)).
			Set("to_state", string(t.To))

		if t.From == StateClosed && t.To == StateOpen {
			actx = actx.
				Set("consecutive_failures", t.ConsecutiveFailures).
				Set("failure_rate", t.FailureRate).
				Set("sample_size", t.SampleSize).
				Set("trip_reason", string(t.TripReason))
		}
		if t.From == StateOpen && t.To == StateHalfOpen {
			// No-op extra; opened_duration_ms not tracked in transition — can be derived elsewhere
			_ = actx
		}
		if t.To == StateClosed {
			actx = actx.Set("consecutive_successes", t.ConsecutiveSuccesses)
		}

		_ = recorder.Record(ctx, shared.ActorID("system"), action, outcome, actx, nil, nil)
	}
}

func actionAndOutcome(t StateTransition) (string, string) {
	switch {
	case t.From == StateClosed && t.To == StateOpen:
		return "circuit_breaker_opened", string(t.TripReason)
	case t.From == StateOpen && t.To == StateHalfOpen:
		return "circuit_breaker_half_opened", "cooldown_elapsed"
	case t.From == StateHalfOpen && t.To == StateClosed:
		return "circuit_breaker_closed", "probes_successful"
	case t.From == StateHalfOpen && t.To == StateOpen:
		return "circuit_breaker_opened", "probe_failed"
	default:
		return "circuit_breaker_transition", fmt.Sprintf("%s_to_%s", t.From, t.To)
	}
}
```

- [ ] **Step 3: Implement metrics listener**

```go
// internal/application/resilience/metrics_listener.go
package resilience

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsListener returns a TransitionListener that records metrics.
type MetricsListenerDeps struct {
	Transitions metric.Int64Counter
	Trips       metric.Int64Counter
}

func MetricsListener(deps MetricsListenerDeps) TransitionListener {
	return func(t StateTransition) {
		ctx := context.Background()
		deps.Transitions.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("tool_name", t.Key.ToolName),
				attribute.String("agent_role", t.Key.AgentRole),
				attribute.String("from", string(t.From)),
				attribute.String("to", string(t.To)),
			),
		)

		// Additional trips counter only for CLOSED → OPEN
		if t.From == StateClosed && t.To == StateOpen {
			deps.Trips.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("tool_name", t.Key.ToolName),
					attribute.String("agent_role", t.Key.AgentRole),
					attribute.String("trip_reason", string(t.TripReason)),
				),
			)
		}
	}
}
```

- [ ] **Step 4: Helper to compose listeners**

Add to `internal/application/resilience/observe.go`:

```go
// ChainListeners composes multiple TransitionListeners into one. Each is called
// synchronously in order.
func ChainListeners(listeners ...TransitionListener) TransitionListener {
	return func(t StateTransition) {
		for _, l := range listeners {
			if l != nil {
				l(t)
			}
		}
	}
}
```

- [ ] **Step 5: Run build + tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/application/resilience/audit_listener.go internal/application/resilience/metrics_listener.go internal/application/resilience/observe.go internal/adapters/inbound/metrics/instruments.go
git commit -m "feat(breaker): add audit and metrics transition listeners"
```

---

## Task 5: HTTP + SDK Query Surface (B5)

**Files:**
- Modify: `internal/ports/inbound/query_service.go`
- Create: `internal/adapters/inbound/http/breaker_handler.go`
- Modify: `internal/adapters/inbound/http/router.go`
- Modify: `internal/adapters/inbound/sdk/facade.go`

- [ ] **Step 1: Add ListBreakers/GetBreakerState to QueryService port**

Modify `internal/ports/inbound/query_service.go`:

```go
import (
	// ... existing imports ...
	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
)

type QueryService interface {
	// ... existing methods ...
	ListBreakers(ctx context.Context, filter resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error)
	GetBreakerState(ctx context.Context, tool, agentRole string) (*resilience.BreakerSnapshot, error)
}
```

- [ ] **Step 2: Update queryServiceAdapter in wire.go**

Modify `internal/bootstrap/wire.go` — the `queryServiceAdapter` struct:

```go
type queryServiceAdapter struct {
	// ... existing fields ...
	breakerRegistry *resilience.CircuitBreakerRegistry
}

// ... and the constructor/assignment in Wire ...

// Add methods:
func (q *queryServiceAdapter) ListBreakers(ctx context.Context, filter resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error) {
	if q.breakerRegistry == nil {
		return nil, nil
	}
	return q.breakerRegistry.List(filter), nil
}

func (q *queryServiceAdapter) GetBreakerState(ctx context.Context, tool, agentRole string) (*resilience.BreakerSnapshot, error) {
	if q.breakerRegistry == nil {
		return nil, nil
	}
	return q.breakerRegistry.Get(tool, agentRole), nil
}
```

Note: nil-safe design allows operating with or without a registry (for backwards compat and test harnesses).

- [ ] **Step 3: Update ALL existing implementors of QueryService**

Find all types that implement the interface:
```bash
rg "QueryService" --files-with-matches
```

Add stub methods to mocks (return nil, nil) and implement real delegation in:
- `internal/adapters/inbound/sdk/facade.go` (GovernanceFacade)
- `internal/adapters/inbound/tracing/query_traced.go` (TracedQueryService)
- Any mock implementations in test files

- [ ] **Step 4: Create HTTP handler**

```go
// internal/adapters/inbound/http/breaker_handler.go
package http

import (
	"encoding/json"
	"net/http"

	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
)

type breakerResponse struct {
	ToolName             string     `json:"tool_name"`
	AgentRole            string     `json:"agent_role"`
	State                string     `json:"state"`
	OpenedAt             *string    `json:"opened_at,omitempty"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	FailureRate          float64    `json:"failure_rate"`
	SampleSize           int        `json:"sample_size"`
	UpdatedAt            string     `json:"updated_at"`
}

func (s *Server) handleListBreakers(w http.ResponseWriter, r *http.Request) {
	filter := resilience.BreakerFilter{}

	if stateParam := r.URL.Query().Get("state"); stateParam != "" {
		validStates := map[string]bool{"closed": true, "open": true, "half_open": true}
		if !validStates[stateParam] {
			writeError(w, http.StatusBadRequest, "INVALID_STATE", "state must be one of: closed, open, half_open")
			return
		}
		bs := resilience.BreakerState(stateParam)
		filter.State = &bs
	}
	if toolParam := r.URL.Query().Get("tool_name"); toolParam != "" {
		filter.ToolName = &toolParam
	}
	if roleParam := r.URL.Query().Get("agent_role"); roleParam != "" {
		filter.AgentRole = &roleParam
	}

	snapshots, err := s.queries.ListBreakers(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	items := make([]breakerResponse, 0, len(snapshots))
	for _, s := range snapshots {
		resp := breakerResponse{
			ToolName:            s.ToolName,
			AgentRole:           s.AgentRole,
			State:               string(s.State),
			ConsecutiveFailures: s.ConsecutiveFailures,
			FailureRate:         s.FailureRate,
			SampleSize:          s.SampleSize,
			UpdatedAt:           s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if s.OpenedAt != nil {
			openedStr := s.OpenedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
			resp.OpenedAt = &openedStr
		}
		items = append(items, resp)
	}

	json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"total": len(items),
	})
}
```

- [ ] **Step 5: Register route**

Modify `internal/adapters/inbound/http/router.go`:

```go
// In s.routes(), inside the /api/v1 Route block:
r.Get("/breakers", s.handleListBreakers)
```

- [ ] **Step 6: Add facade methods**

Modify `internal/adapters/inbound/sdk/facade.go`:

```go
func (f *GovernanceFacade) ListBreakers(ctx context.Context, filter resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error) {
	return f.queries.ListBreakers(ctx, filter)
}

func (f *GovernanceFacade) GetBreakerState(ctx context.Context, tool, agentRole string) (*resilience.BreakerSnapshot, error) {
	return f.queries.GetBreakerState(ctx, tool, agentRole)
}
```

Add the `resilience` import.

- [ ] **Step 7: Run build + tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/ports/inbound/query_service.go internal/adapters/inbound/http/breaker_handler.go internal/adapters/inbound/http/router.go internal/adapters/inbound/sdk/facade.go internal/adapters/inbound/tracing/query_traced.go internal/bootstrap/wire.go
git commit -m "feat(breaker): add HTTP GET /api/v1/breakers and SDK ListBreakers/GetBreakerState"
```

---

## Task 6: Wiring + Startup (B6)

**Files:**
- Modify: `internal/bootstrap/wire.go`
- Modify: `cmd/agent-governance-core/main.go` (if needed)

- [ ] **Step 1: Construct registry and wire listeners**

Modify `internal/bootstrap/wire.go`. In the `Wire` function, before creating `workflowSvc`:

```go
// Construct circuit breaker registry
breakerRegistry := resilience.NewCircuitBreakerRegistry()

// Build listeners
var breakerListeners []resilience.TransitionListener
breakerListeners = append(breakerListeners, resilience.AuditListener(auditRecorder))

if cfg.OTelEnabled && instruments != nil {
	breakerListeners = append(breakerListeners, resilience.MetricsListener(resilience.MetricsListenerDeps{
		Transitions: instruments.CircuitBreakerTransitions,
		Trips:       instruments.CircuitBreakerTrips,
	}))
}
breakerRegistry.SetTransitionListener(resilience.ChainListeners(breakerListeners...))
```

Note: `instruments` is the existing `*metrics.Instruments` variable in `Wire`. If the current code uses a different variable name, match it.

- [ ] **Step 2: Pass registry to WorkflowRunService**

Update the `NewWorkflowRunService` call in wire.go to pass `breakerRegistry` as the final argument (replacing the nil from Task 3).

- [ ] **Step 3: Pass registry to queryServiceAdapter**

Update the `queryServiceAdapter` construction to include `breakerRegistry: breakerRegistry`.

- [ ] **Step 4: Emit startup audit + log**

Add, right after constructing the registry:

```go
// Startup: emit operational event (no task/workflow attribution)
logger.Info("circuit breaker registry started (empty state, all breakers begin CLOSED)")
startupCtx := resilienceStartupContext()
startupAudit := audit.NewAuditContext().Set("component", "circuit_breaker")
_ = auditRecorder.Record(startupCtx, shared.ActorID("system"),
	"circuit_breaker_registry_started", "empty", startupAudit, nil, nil)
```

Add helper:
```go
// resilienceStartupContext returns a context suitable for startup-time operational events.
func resilienceStartupContext() context.Context {
	return context.Background()
}
```

Note: if this introduces a cyclic import with `audit` package alias, adapt the alias to match existing imports in `wire.go`.

- [ ] **Step 5: Run build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/... ./test/fixtures/... -count=1`
Expected: ALL PASS

Also run integration tests to verify wiring doesn't break anything:
Run: `go test ./test/integration/... -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/bootstrap/wire.go
git commit -m "feat(breaker): wire circuit breaker registry with audit + metrics listeners + startup event"
```

---

## Task 7: Integration Test (B7)

**Files:**
- Create: `test/integration/circuitbreaker/breaker_test.go`

- [ ] **Step 1: Write end-to-end integration tests**

```go
//go:build integration

package circuitbreaker_test

import (
	"context"
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/application/resilience"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests must wire the FULL stack including the breaker registry with listeners.
// READ test/integration/usecases/process_task_test.go for the wiring template —
// extend setupServices() to include breakerRegistry.

func TestIntegration_BreakerTripsAfterConsecutiveFailures(t *testing.T) {
	svc := setupServicesWithBreaker(t)
	ctx := context.Background()

	// Submit + route + policy (allow) + start
	tk := submitAndStart(t, svc, ctx)

	// Register 3 failures with same tool+role+code
	stage := shared.StageRuntime
	code := "tool/shell_timeout"
	retryable := true
	tool := "shell"
	role := "implementer"
	result := execution.AttemptResult{
		Status:       execution.AttemptStatusFailure,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
		ToolName:     &tool,
		AgentRole:    &role,
	}
	for i := 0; i < 3; i++ {
		wf, err := svc.workflowSvc.RegisterAttempt(ctx, tk.wf.ID, result)
		require.NoError(t, err)
		_ = wf
	}

	// Verify breaker state via registry directly
	snap := svc.breakerRegistry.Get("shell", "implementer")
	require.NotNil(t, snap)
	assert.Equal(t, resilience.StateOpen, snap.State)
	assert.GreaterOrEqual(t, snap.ConsecutiveFailures, 3)

	// Verify audit entry was recorded
	action := "circuit_breaker_opened"
	filter := outbound.AuditFilter{Action: &action, Limit: 100}
	entries, _, err := svc.auditRepo.Query(ctx, filter)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "expected at least one circuit_breaker_opened audit entry")

	found := false
	for _, e := range entries {
		actx := e.Context()
		if actx["tool_name"] == "shell" && actx["agent_role"] == "implementer" {
			found = true
			assert.Equal(t, "consecutive", e.Outcome(), "trip reason should be consecutive")
			break
		}
	}
	assert.True(t, found, "expected audit entry for shell+implementer breaker opening")
}

func TestIntegration_ListBreakersViaQueryService(t *testing.T) {
	svc := setupServicesWithBreaker(t)
	ctx := context.Background()

	tk := submitAndStart(t, svc, ctx)

	// Trip one breaker
	stage := shared.StageRuntime
	code := "tool/shell_timeout"
	retryable := true
	tool := "shell"
	role := "implementer"
	failResult := execution.AttemptResult{
		Status: execution.AttemptStatusFailure, FailureStage: &stage,
		FailureCode: &code, Retryable: &retryable, ToolName: &tool, AgentRole: &role,
	}
	for i := 0; i < 3; i++ {
		svc.workflowSvc.RegisterAttempt(ctx, tk.wf.ID, failResult)
	}

	// Query via QueryService port (the real inbound API)
	snapshots, err := svc.querySvc.ListBreakers(ctx, resilience.BreakerFilter{})
	require.NoError(t, err)
	assert.Len(t, snapshots, 1)
	assert.Equal(t, "shell", snapshots[0].ToolName)
	assert.Equal(t, resilience.StateOpen, snapshots[0].State)

	// Filter by state=open
	openState := resilience.StateOpen
	openOnly, err := svc.querySvc.ListBreakers(ctx, resilience.BreakerFilter{State: &openState})
	require.NoError(t, err)
	assert.Len(t, openOnly, 1)
}

func TestIntegration_StartupAuditEntry(t *testing.T) {
	svc := setupServicesWithBreaker(t)
	ctx := context.Background()

	// The setup should have emitted the startup audit during wiring.
	action := "circuit_breaker_registry_started"
	filter := outbound.AuditFilter{Action: &action, Limit: 10}
	entries, _, err := svc.auditRepo.Query(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "expected exactly one startup audit entry")

	e := entries[0]
	assert.Equal(t, "empty", e.Outcome())
	assert.Nil(t, e.TaskID(), "startup event should have nil TaskID")
	assert.Nil(t, e.WorkflowRunID(), "startup event should have nil WorkflowRunID")
	assert.Equal(t, "circuit_breaker", e.Context()["component"])
}

// ------- Test harness -------

type breakerTestServices struct {
	workflowSvc     *workflowrun.WorkflowRunService
	submitSvc       *intake.SubmitTaskService
	routeSvc        *routing.RouteTaskService
	policySvc       *policyeval.EvaluatePolicyService
	querySvc        inbound.QueryService
	breakerRegistry *resilience.CircuitBreakerRegistry
	auditRepo       outbound.AuditEntryRepository
}

func setupServicesWithBreaker(t *testing.T) *breakerTestServices {
	// READ the existing setupServices function in process_task_test.go and extend
	// to:
	// 1. Create resilience.NewCircuitBreakerRegistry()
	// 2. Attach AuditListener(auditRecorder) via ChainListeners
	// 3. Emit the circuit_breaker_registry_started audit entry during setup
	// 4. Pass registry to NewWorkflowRunService as last arg
	// 5. Return the registry in the test services struct
	t.Skip("TODO: copy setupServices from process_task_test.go and extend for breaker")
	return nil
}

type taskSetup struct {
	task *task.Task
	wf   *workflow.WorkflowRun
}

func submitAndStart(t *testing.T, svc *breakerTestServices, ctx context.Context) *taskSetup {
	// Submit, route, policy (allow), start workflow
	// READ process_task_test.go for the exact sequence
	return nil
}
```

Note on the test harness: the subagent implementing this should COPY the setup pattern from `test/integration/usecases/process_task_test.go` (specifically the wiring of all services and the startup audit handling) and adapt it here to include the breaker registry.

- [ ] **Step 2: Run integration tests**

Run: `go test ./test/integration/circuitbreaker/... -v -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS

- [ ] **Step 3: Verify full test suite still passes**

Run: `go test ./test/integration/... -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add test/integration/circuitbreaker/
git commit -m "feat(breaker): add integration tests for trip + list + startup audit"
```

---

## Verification Checklist

After all tasks:

- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./internal/... ./test/fixtures/... -count=1` — ALL PASS (zero regressions)
- [ ] `go test ./test/integration/... -count=1 -tags=integration -timeout=600s` — ALL PASS
- [ ] Breaker starts CLOSED on first observation
- [ ] 3 consecutive failures → OPEN (via consecutive condition)
- [ ] >50% failure rate over 20 with sample≥5 → OPEN (via ratio condition)
- [ ] OPEN + 60s cooldown + observation → HALF_OPEN
- [ ] HALF_OPEN + 3 consecutive successes → CLOSED
- [ ] HALF_OPEN + 1 failure → OPEN, cooldown reset
- [ ] No traffic in HALF_OPEN → state persists indefinitely
- [ ] `tool_name` empty or `agent_role` empty → skip breaker
- [ ] Audit entries emitted on each transition (circuit_breaker_opened/half_opened/closed)
- [ ] Startup audit `circuit_breaker_registry_started` appears exactly once per app start
- [ ] Metrics emitted: `governance.circuit_breaker.transitions` + `governance.circuit_breaker.trips`
- [ ] `GET /api/v1/breakers` returns all breakers; `?state=open` filters correctly
- [ ] Facade `ListBreakers` / `GetBreakerState` work
- [ ] Registry reset to empty on application restart (deliberate)
