package resilience

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsListenerDeps holds the metric instruments required by MetricsListener.
type MetricsListenerDeps struct {
	Transitions metric.Int64Counter
	Trips       metric.Int64Counter
}

// MetricsListener returns a TransitionListener that records metrics.
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
