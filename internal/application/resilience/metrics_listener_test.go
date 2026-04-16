package resilience

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// --- test helpers ---

func setupMeter(t *testing.T) (metric.Meter, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	return mp.Meter("test"), reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

func newTestDeps(t *testing.T) (MetricsListenerDeps, *sdkmetric.ManualReader) {
	t.Helper()
	meter, reader := setupMeter(t)
	transitions, err := meter.Int64Counter("governance.circuit_breaker.transitions")
	require.NoError(t, err)
	trips, err := meter.Int64Counter("governance.circuit_breaker.trips")
	require.NoError(t, err)
	return MetricsListenerDeps{Transitions: transitions, Trips: trips}, reader
}

// --- tests ---

func TestMetricsListener_ClosedToOpen_IncrementsBothCounters(t *testing.T) {
	deps, reader := newTestDeps(t)
	listener := MetricsListener(deps)

	listener(StateTransition{
		Key:        BreakerKey{ToolName: "shell", AgentRole: "implementer"},
		From:       StateClosed,
		To:         StateOpen,
		TripReason: TripReasonConsecutive,
		At:         time.Now(),
	})

	rm := collectMetrics(t, reader)

	// Transitions counter
	trans := findMetric(rm, "governance.circuit_breaker.transitions")
	require.NotNil(t, trans)
	transSum, ok := trans.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, transSum.DataPoints, 1)
	assert.Equal(t, int64(1), transSum.DataPoints[0].Value)

	attrs := transSum.DataPoints[0].Attributes
	v, found := attrs.Value(attribute.Key("tool_name"))
	assert.True(t, found)
	assert.Equal(t, "shell", v.AsString())
	v, found = attrs.Value(attribute.Key("agent_role"))
	assert.True(t, found)
	assert.Equal(t, "implementer", v.AsString())
	v, found = attrs.Value(attribute.Key("from"))
	assert.True(t, found)
	assert.Equal(t, string(StateClosed), v.AsString())
	v, found = attrs.Value(attribute.Key("to"))
	assert.True(t, found)
	assert.Equal(t, string(StateOpen), v.AsString())

	// Trips counter — should also be incremented
	trips := findMetric(rm, "governance.circuit_breaker.trips")
	require.NotNil(t, trips)
	tripsSum, ok := trips.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, tripsSum.DataPoints, 1)
	assert.Equal(t, int64(1), tripsSum.DataPoints[0].Value)

	tripAttrs := tripsSum.DataPoints[0].Attributes
	v, found = tripAttrs.Value(attribute.Key("trip_reason"))
	assert.True(t, found)
	assert.Equal(t, string(TripReasonConsecutive), v.AsString())
	v, found = tripAttrs.Value(attribute.Key("tool_name"))
	assert.True(t, found)
	assert.Equal(t, "shell", v.AsString())
	v, found = tripAttrs.Value(attribute.Key("agent_role"))
	assert.True(t, found)
	assert.Equal(t, "implementer", v.AsString())
}

func TestMetricsListener_OpenToHalfOpen_OnlyTransitions(t *testing.T) {
	deps, reader := newTestDeps(t)
	listener := MetricsListener(deps)

	listener(StateTransition{
		Key:  BreakerKey{ToolName: "api", AgentRole: "reviewer"},
		From: StateOpen,
		To:   StateHalfOpen,
		At:   time.Now(),
	})

	rm := collectMetrics(t, reader)

	trans := findMetric(rm, "governance.circuit_breaker.transitions")
	require.NotNil(t, trans)
	transSum, ok := trans.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, transSum.DataPoints, 1)
	assert.Equal(t, int64(1), transSum.DataPoints[0].Value)

	// Trips should not have non-zero data points
	trips := findMetric(rm, "governance.circuit_breaker.trips")
	if trips != nil {
		if tripsSum, ok := trips.Data.(metricdata.Sum[int64]); ok {
			for _, dp := range tripsSum.DataPoints {
				assert.Equal(t, int64(0), dp.Value, "trips should not increment on OPEN→HALF_OPEN")
			}
		}
	}
}

func TestMetricsListener_HalfOpenToClosed_OnlyTransitions(t *testing.T) {
	deps, reader := newTestDeps(t)
	listener := MetricsListener(deps)

	listener(StateTransition{
		Key:  BreakerKey{ToolName: "api", AgentRole: "reviewer"},
		From: StateHalfOpen,
		To:   StateClosed,
		At:   time.Now(),
	})

	rm := collectMetrics(t, reader)

	trans := findMetric(rm, "governance.circuit_breaker.transitions")
	require.NotNil(t, trans)
	transSum, ok := trans.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, transSum.DataPoints, 1)
	assert.Equal(t, int64(1), transSum.DataPoints[0].Value)

	trips := findMetric(rm, "governance.circuit_breaker.trips")
	if trips != nil {
		if tripsSum, ok := trips.Data.(metricdata.Sum[int64]); ok {
			for _, dp := range tripsSum.DataPoints {
				assert.Equal(t, int64(0), dp.Value, "trips should not increment on HALF_OPEN→CLOSED")
			}
		}
	}
}

func TestMetricsListener_HalfOpenToOpen_OnlyTransitions(t *testing.T) {
	deps, reader := newTestDeps(t)
	listener := MetricsListener(deps)

	listener(StateTransition{
		Key:  BreakerKey{ToolName: "api", AgentRole: "reviewer"},
		From: StateHalfOpen,
		To:   StateOpen,
		At:   time.Now(),
	})

	rm := collectMetrics(t, reader)

	trans := findMetric(rm, "governance.circuit_breaker.transitions")
	require.NotNil(t, trans)
	transSum, ok := trans.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, transSum.DataPoints, 1)
	assert.Equal(t, int64(1), transSum.DataPoints[0].Value)

	// HALF_OPEN→OPEN is NOT a CLOSED→OPEN trip — trips should not increment
	trips := findMetric(rm, "governance.circuit_breaker.trips")
	if trips != nil {
		if tripsSum, ok := trips.Data.(metricdata.Sum[int64]); ok {
			for _, dp := range tripsSum.DataPoints {
				assert.Equal(t, int64(0), dp.Value, "trips should only increment on CLOSED→OPEN")
			}
		}
	}
}

func TestMetricsListener_MultipleTransitions_Accumulate(t *testing.T) {
	deps, reader := newTestDeps(t)
	listener := MetricsListener(deps)

	key := BreakerKey{ToolName: "shell", AgentRole: "implementer"}

	listener(StateTransition{Key: key, From: StateClosed, To: StateOpen, TripReason: TripReasonConsecutive})
	listener(StateTransition{Key: key, From: StateOpen, To: StateHalfOpen})
	listener(StateTransition{Key: key, From: StateHalfOpen, To: StateClosed})
	listener(StateTransition{Key: key, From: StateClosed, To: StateOpen, TripReason: TripReasonRatio})

	rm := collectMetrics(t, reader)

	trans := findMetric(rm, "governance.circuit_breaker.transitions")
	require.NotNil(t, trans)
	transSum, ok := trans.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	// Transitions label set is {tool_name, agent_role, from, to}. The two CLOSED→OPEN
	// transitions collapse into one data point (trip_reason isn't a transitions label),
	// so we get 3 unique data points covering 4 transitions total.
	assert.Len(t, transSum.DataPoints, 3)
	var totalTransitions int64
	for _, dp := range transSum.DataPoints {
		totalTransitions += dp.Value
	}
	assert.Equal(t, int64(4), totalTransitions)

	trips := findMetric(rm, "governance.circuit_breaker.trips")
	require.NotNil(t, trips)
	tripsSum, ok := trips.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	// 2 trips — one with consecutive, one with ratio (different label sets)
	assert.Len(t, tripsSum.DataPoints, 2)

	var totalTrips int64
	for _, dp := range tripsSum.DataPoints {
		totalTrips += dp.Value
	}
	assert.Equal(t, int64(2), totalTrips)
}
