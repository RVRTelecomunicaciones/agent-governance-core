// Package metrics provides OTel metric instrument definitions and decorator wrappers
// for governance service interfaces.
package metrics

import (
	"go.opentelemetry.io/otel/metric"
)

// Instruments holds all OTel metric instruments used by the governance system.
type Instruments struct {
	TasksSubmitted      metric.Int64Counter
	TasksCompleted      metric.Int64Counter
	RoutingDuration     metric.Float64Histogram
	PolicyDuration      metric.Float64Histogram
	PolicyOutcomes      metric.Int64Counter
	WorkflowDuration    metric.Float64Histogram
	WorkflowTransitions metric.Int64Counter
	ApprovalWait        metric.Float64Histogram
	ExecutionAttempts    metric.Float64Histogram
	ExecutionFailures   metric.Int64Counter
	MemoryDuration      metric.Float64Histogram
	MemoryDegraded      metric.Int64Counter
	CircuitBreakerTransitions metric.Int64Counter
	CircuitBreakerTrips       metric.Int64Counter
}

// NewInstruments creates all 12 metric instruments from the given Meter.
func NewInstruments(meter metric.Meter) (*Instruments, error) {
	tasksSubmitted, err := meter.Int64Counter("governance.tasks.submitted",
		metric.WithDescription("Number of tasks submitted to the governance pipeline"),
	)
	if err != nil {
		return nil, err
	}

	tasksCompleted, err := meter.Int64Counter("governance.tasks.completed",
		metric.WithDescription("Number of tasks that completed processing"),
	)
	if err != nil {
		return nil, err
	}

	routingDuration, err := meter.Float64Histogram("governance.routing.duration_ms",
		metric.WithDescription("Time spent evaluating routing strategies"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	policyDuration, err := meter.Float64Histogram("governance.policy.duration_ms",
		metric.WithDescription("Time spent evaluating policy rules"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	policyOutcomes, err := meter.Int64Counter("governance.policy.outcomes",
		metric.WithDescription("Count of policy evaluation outcomes by type"),
	)
	if err != nil {
		return nil, err
	}

	workflowDuration, err := meter.Float64Histogram("governance.workflow.duration_ms",
		metric.WithDescription("Total workflow execution duration"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	workflowTransitions, err := meter.Int64Counter("governance.workflow.transitions",
		metric.WithDescription("Count of workflow state transitions"),
	)
	if err != nil {
		return nil, err
	}

	approvalWait, err := meter.Float64Histogram("governance.approval.wait_ms",
		metric.WithDescription("Time spent waiting for approval"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	executionAttempts, err := meter.Float64Histogram("governance.execution.attempts",
		metric.WithDescription("Distribution of execution attempt counts"),
	)
	if err != nil {
		return nil, err
	}

	executionFailures, err := meter.Int64Counter("governance.execution.failures",
		metric.WithDescription("Count of execution failures"),
	)
	if err != nil {
		return nil, err
	}

	memoryDuration, err := meter.Float64Histogram("governance.memory.duration_ms",
		metric.WithDescription("Time spent on memory context operations"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	memoryDegraded, err := meter.Int64Counter("governance.memory.degraded",
		metric.WithDescription("Count of degraded memory context events"),
	)
	if err != nil {
		return nil, err
	}

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

	return &Instruments{
		TasksSubmitted:      tasksSubmitted,
		TasksCompleted:      tasksCompleted,
		RoutingDuration:     routingDuration,
		PolicyDuration:      policyDuration,
		PolicyOutcomes:      policyOutcomes,
		WorkflowDuration:    workflowDuration,
		WorkflowTransitions: workflowTransitions,
		ApprovalWait:        approvalWait,
		ExecutionAttempts:    executionAttempts,
		ExecutionFailures:   executionFailures,
		MemoryDuration:      memoryDuration,
		MemoryDegraded:      memoryDegraded,
		CircuitBreakerTransitions: cbTransitions,
		CircuitBreakerTrips:       cbTrips,
	}, nil
}
