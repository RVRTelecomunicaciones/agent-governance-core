package resilience

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_CreatesBreakerOnFirstObserve(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// Before any observe, Get returns nil.
	assert.Nil(t, r.Get("shell", "implementer"))

	// First observe should create the breaker (CLOSED, no transition on success).
	transition, err := r.Observe(ctx, "shell", "implementer", true, now)
	require.NoError(t, err)
	assert.Nil(t, transition, "single success in CLOSED should not transition")

	snap := r.Get("shell", "implementer")
	require.NotNil(t, snap, "breaker should exist after first observe")
	assert.Equal(t, "shell", snap.ToolName)
	assert.Equal(t, "implementer", snap.AgentRole)
	assert.Equal(t, StateClosed, snap.State)
	assert.Equal(t, 1, snap.SampleSize)
}

func TestRegistry_ReusesExistingBreaker(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// 3 consecutive failures on the same key should trip that breaker.
	_, err := r.Observe(ctx, "shell", "implementer", false, now)
	require.NoError(t, err)
	_, err = r.Observe(ctx, "shell", "implementer", false, now.Add(time.Second))
	require.NoError(t, err)
	transition, err := r.Observe(ctx, "shell", "implementer", false, now.Add(2*time.Second))
	require.NoError(t, err)

	require.NotNil(t, transition, "3rd failure should trip")
	assert.Equal(t, StateClosed, transition.From)
	assert.Equal(t, StateOpen, transition.To)
	assert.Equal(t, TripReasonConsecutive, transition.TripReason)

	// Only one breaker should exist.
	all := r.List(BreakerFilter{})
	assert.Len(t, all, 1)
	assert.Equal(t, StateOpen, all[0].State)
}

func TestRegistry_IndependentPerKey(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// Key A: 3 failures → OPEN
	_, _ = r.Observe(ctx, "shell", "implementer", false, now)
	_, _ = r.Observe(ctx, "shell", "implementer", false, now.Add(time.Second))
	trA, _ := r.Observe(ctx, "shell", "implementer", false, now.Add(2*time.Second))
	require.NotNil(t, trA)
	assert.Equal(t, StateOpen, trA.To)

	// Key B: single success → CLOSED, no transition
	trB, _ := r.Observe(ctx, "api", "reviewer", true, now.Add(3*time.Second))
	assert.Nil(t, trB)

	snapA := r.Get("shell", "implementer")
	snapB := r.Get("api", "reviewer")

	require.NotNil(t, snapA)
	require.NotNil(t, snapB)
	assert.Equal(t, StateOpen, snapA.State)
	assert.Equal(t, StateClosed, snapB.State)

	// They are different breakers.
	all := r.List(BreakerFilter{})
	assert.Len(t, all, 2)
}

func TestRegistry_SkipsWhenToolNameEmpty(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()

	transition, err := r.Observe(ctx, "", "implementer", false, time.Now())
	require.NoError(t, err)
	assert.Nil(t, transition, "empty tool must be skipped")

	assert.Empty(t, r.List(BreakerFilter{}), "no breaker should be created")
}

func TestRegistry_SkipsWhenAgentRoleEmpty(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()

	transition, err := r.Observe(ctx, "shell", "", false, time.Now())
	require.NoError(t, err)
	assert.Nil(t, transition, "empty role must be skipped")

	assert.Empty(t, r.List(BreakerFilter{}), "no breaker should be created")
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// Count listener invocations to ensure no lost events and no deadlocks.
	var listenerCalls int64
	r.SetTransitionListener(func(_ StateTransition) {
		atomic.AddInt64(&listenerCalls, 1)
	})

	const goroutines = 100
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			// Spread writes across a few keys to exercise shared map access.
			tool := "shell"
			role := "implementer"
			if id%3 == 0 {
				role = "reviewer"
			}
			if id%5 == 0 {
				tool = "api"
			}
			for j := 0; j < opsPerGoroutine; j++ {
				success := (j % 2) == 0
				_, _ = r.Observe(ctx, tool, role, success, now.Add(time.Duration(j)*time.Millisecond))
				// Occasional reads to exercise Get/List under contention.
				if j%10 == 0 {
					_ = r.Get(tool, role)
					_ = r.List(BreakerFilter{})
				}
			}
		}(i)
	}
	wg.Wait()

	// Sanity: at least one breaker exists.
	all := r.List(BreakerFilter{})
	assert.NotEmpty(t, all, "breakers should exist after concurrent access")
	// Listener is called only on transitions; we don't assert an exact count
	// because transition counts depend on interleaving. Just ensure counter
	// access didn't race (atomic) and we reached here without deadlock.
	_ = atomic.LoadInt64(&listenerCalls)
}

func TestRegistry_List_FilterByState(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	// Breaker 1: OPEN (3 consecutive failures).
	_, _ = r.Observe(ctx, "shell", "implementer", false, now)
	_, _ = r.Observe(ctx, "shell", "implementer", false, now.Add(time.Second))
	_, _ = r.Observe(ctx, "shell", "implementer", false, now.Add(2*time.Second))

	// Breaker 2: CLOSED (single success).
	_, _ = r.Observe(ctx, "api", "reviewer", true, now)

	// Breaker 3: CLOSED (two failures — below threshold).
	_, _ = r.Observe(ctx, "git", "implementer", false, now)
	_, _ = r.Observe(ctx, "git", "implementer", false, now.Add(time.Second))

	allUnfiltered := r.List(BreakerFilter{})
	assert.Len(t, allUnfiltered, 3)

	open := StateOpen
	openList := r.List(BreakerFilter{State: &open})
	require.Len(t, openList, 1)
	assert.Equal(t, "shell", openList[0].ToolName)
	assert.Equal(t, "implementer", openList[0].AgentRole)

	closed := StateClosed
	closedList := r.List(BreakerFilter{State: &closed})
	assert.Len(t, closedList, 2)
	for _, snap := range closedList {
		assert.Equal(t, StateClosed, snap.State)
	}

	// HALF_OPEN filter — no breaker is in half-open, should be empty.
	half := StateHalfOpen
	halfList := r.List(BreakerFilter{State: &half})
	assert.Empty(t, halfList)
}

func TestRegistry_Get_MissingReturnsNil(t *testing.T) {
	r := NewCircuitBreakerRegistry()

	assert.Nil(t, r.Get("unknown", "unknown"))
	assert.Nil(t, r.Get("", ""))
}

func TestRegistry_ListenerCalledOnTransition(t *testing.T) {
	r := NewCircuitBreakerRegistry()
	ctx := context.Background()
	now := time.Now()

	var received []StateTransition
	var mu sync.Mutex
	r.SetTransitionListener(func(tr StateTransition) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, tr)
	})

	// 3 consecutive failures should trigger exactly one transition.
	_, _ = r.Observe(ctx, "shell", "implementer", false, now)
	_, _ = r.Observe(ctx, "shell", "implementer", false, now.Add(time.Second))
	_, _ = r.Observe(ctx, "shell", "implementer", false, now.Add(2*time.Second))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1, "expected one CLOSED→OPEN transition")
	assert.Equal(t, StateClosed, received[0].From)
	assert.Equal(t, StateOpen, received[0].To)
	assert.Equal(t, TripReasonConsecutive, received[0].TripReason)
}

func TestChainListeners_CallsAllInOrder(t *testing.T) {
	var order []int
	var mu sync.Mutex
	record := func(n int) TransitionListener {
		return func(_ StateTransition) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, n)
		}
	}

	chained := ChainListeners(record(1), nil, record(2), record(3))
	chained(StateTransition{})

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{1, 2, 3}, order)
}
