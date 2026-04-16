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
	// Pattern: F F S F F S F F S F F S F F S = 10F/5S = 15 events, ratio 10/15 = 0.67
	pattern := []bool{
		false, false, true, // 2 fails, break with success
		false, false, true,
		false, false, true,
		false, false, true,
		false, false, true,
	}
	var last *StateTransition
	for i, isSuccess := range pattern {
		transition := b.Observe(isSuccess, now.Add(time.Duration(i)*time.Second))
		if transition != nil {
			last = transition
		}
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

func TestBreaker_OpenToHalfOpen_WithSuccessAfterCooldown(t *testing.T) {
	b := NewCircuitBreaker(BreakerKey{ToolName: "shell", AgentRole: "implementer"})
	now := time.Now()

	// Trip the breaker
	b.Observe(false, now)
	b.Observe(false, now.Add(time.Second))
	b.Observe(false, now.Add(2*time.Second))
	assert.Equal(t, StateOpen, b.State)

	// Wait past cooldown, then observe a success
	afterCooldown := now.Add(2*time.Second + OpenCooldown + time.Second)
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

	// Cooldown elapsed (breaker opened at now+2s, need > 60s after that)
	t0 := now.Add(2*time.Second + OpenCooldown + time.Second)
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

	// Enter half-open via success (breaker opened at now+2s)
	t0 := now.Add(2*time.Second + OpenCooldown + time.Second)
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
