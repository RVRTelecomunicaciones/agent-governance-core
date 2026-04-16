package resilience

import (
	"context"
	"time"
)

// Observe records an attempt against the breaker for (tool, role) and returns a
// transition if state changed. Returns nil if tool or role is empty (skip).
//
// The transition listener is invoked OUTSIDE the registry mutex. We capture the
// listener pointer while holding the lock, then release the lock before calling
// it. This avoids deadlocks when listeners are slow, reentrant (e.g. call back
// into the registry), or acquire other locks themselves.
func (r *CircuitBreakerRegistry) Observe(ctx context.Context, tool, role string, success bool, at time.Time) (*StateTransition, error) {
	if tool == "" || role == "" {
		return nil, nil // skip — no identifiable combination
	}
	_ = ctx // reserved for future

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

	// Call listener outside the mutex to avoid deadlocks with slow or
	// reentrant listeners.
	if transition != nil && listener != nil {
		listener(*transition)
	}
	return transition, nil
}

// ChainListeners composes multiple TransitionListeners into one. Each is called
// synchronously in order. nil listeners are skipped.
func ChainListeners(listeners ...TransitionListener) TransitionListener {
	return func(t StateTransition) {
		for _, l := range listeners {
			if l != nil {
				l(t)
			}
		}
	}
}
