# Phase 2 Track 1: Observability + Telemetry — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add distributed tracing (OTel spans), operational metrics (counters/histograms), and lifecycle telemetry to the governance pipeline — with zero impact when disabled.

**Architecture:** Hybrid instrumentation — decorator pattern for UC-level spans/metrics on inbound ports, inline spans for detail operations (memory, notifier). Lifecycle metrics emitted inline in terminal-state UCs. Tracing outer, metrics inner in decorator stack. Single `OTEL_ENABLED` flag; no-op when false.

**Tech Stack:** Go 1.26.2, go.opentelemetry.io/otel v1.43+, sdktrace, sdkmetric, otlptracehttp, otlpmetrichttp

**Spec:** `docs/superpowers/specs/2026-04-15-phase2-track1-observability-design.md`

**Baseline invariant:** All 488 Phase 1 tests must continue to pass. `OTEL_ENABLED=false` must produce identical behavior to Phase 1.

---

## Block Execution Map

| Task | Block | Parallel with | Done criteria |
|---|---|---|---|
| T1: OTel setup + config | B1 | None (foundation) | `go build ./...` passes, setup returns no-op when disabled |
| T2: Tracing decorators | B2 | T3, T4 | All 5 decorator files compile + tests pass |
| T3: Metrics instruments + decorators | B3 | T2, T4 | All metric instruments defined, 2 decorators + tests pass |
| T4: Inline spans + lifecycle metrics | B4 | T2, T3 | Memory/notifier spans work, lifecycle metrics emit on terminal |
| T5: Wiring + main.go | B5 | None | Full build, all 488+ tests pass, conditional decorator wrapping works |
| T6: Integration test | B6 | None | Span tree verified, trace_id in audit, metrics emitted |

### Dependency Graph

```
T1 (OTel setup)
 ├── T2 (tracing decorators) ─── parallel
 ├── T3 (metrics decorators) ─── parallel
 └── T4 (inline + lifecycle) ─── parallel
         │
         T5 (wiring)
         │
         T6 (integration test)
```

---

## File Structure

```
internal/
  infrastructure/
    observability/
      otel.go                         — SetupOTel function
      otel_test.go                    — setup/teardown + no-op tests
    config/
      config.go                       — MODIFY: add OTelEnabled field
  adapters/
    inbound/
      tracing/
        governance_traced.go          — TracedGovernanceService decorator
        governance_traced_test.go
        workflow_traced.go            — TracedWorkflowControl decorator
        workflow_traced_test.go
        approval_traced.go            — TracedApprovalService decorator
        escalation_traced.go          — TracedEscalationPort decorator
        query_traced.go               — TracedQueryService decorator
      metrics/
        instruments.go                — all OTel instrument definitions
        governance_metrics.go         — MetricsGovernanceService decorator
        governance_metrics_test.go
        workflow_metrics.go           — MetricsWorkflowControl decorator
        workflow_metrics_test.go
    outbound/
      memory/
        stub_provider.go              — MODIFY: add inline span
      events/
        callback_notifier.go          — MODIFY: add inline spans
  application/
    workflowrun/
      service.go                      — MODIFY: add metrics field
      register_attempt.go             — MODIFY: emit lifecycle metrics
      kill_workflow.go                — MODIFY: emit lifecycle metrics
      start_workflow.go               — MODIFY: emit lifecycle metrics on deny
    approvals/
      resolve_approval.go             — MODIFY: emit lifecycle metrics
  bootstrap/
    wire.go                           — MODIFY: OTel setup + decorator wrapping
cmd/
  agent-governance-core/
    main.go                           — MODIFY: OTel shutdown
test/
  integration/
    observability/
      otel_integration_test.go        — span tree + audit correlation
```

---

## Task 1: OTel Setup + Config (B1)

**Files:**
- Create: `internal/infrastructure/observability/otel.go`
- Create: `internal/infrastructure/observability/otel_test.go`
- Modify: `internal/infrastructure/config/config.go`

- [ ] **Step 1: Add OTel dependencies to go.mod**

```bash
cd /Users/russell/Documents/2026/agent-governance-core
go get go.opentelemetry.io/otel@latest
go get go.opentelemetry.io/otel/sdk@latest
go get go.opentelemetry.io/otel/sdk/metric@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@latest
go mod tidy
```

- [ ] **Step 2: Write failing test for OTel setup**

```go
// internal/infrastructure/observability/otel_test.go
package observability_test

import (
	"context"
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/infrastructure/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestSetupOTel_Disabled(t *testing.T) {
	shutdown, err := observability.SetupOTel(context.Background(), false, "test-service")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown should be a no-op — no error
	err = shutdown(context.Background())
	assert.NoError(t, err)

	// Global tracer should be no-op
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.False(t, span.SpanContext().IsValid(), "span should not be valid when disabled")
	span.End()
}

func TestSetupOTel_Enabled_NoEndpoint(t *testing.T) {
	// When enabled but no OTLP endpoint is configured, setup should still succeed
	// (the exporter will fail on export, not on setup)
	shutdown, err := observability.SetupOTel(context.Background(), true, "test-service")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Global tracer should produce valid spans
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.True(t, span.SpanContext().IsValid(), "span should be valid when enabled")
	span.End()

	// Shutdown should flush without error (no data to send is fine)
	err = shutdown(context.Background())
	assert.NoError(t, err)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/infrastructure/observability/... -v -count=1`
Expected: FAIL — package doesn't exist

- [ ] **Step 4: Implement SetupOTel**

```go
// internal/infrastructure/observability/otel.go
package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// SetupOTel initializes OpenTelemetry tracing and metrics.
// If enabled=false, returns a no-op shutdown — zero impact on the application.
// If enabled=true, configures TracerProvider and MeterProvider with OTLP exporters
// using standard OTel env vars (OTEL_EXPORTER_OTLP_ENDPOINT, etc.).
func SetupOTel(ctx context.Context, enabled bool, serviceName string) (shutdown func(context.Context) error, err error) {
	if !enabled {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating otel resource: %w", err)
	}

	// Trace exporter — uses OTEL_EXPORTER_OTLP_ENDPOINT etc.
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Metric exporter
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		tp.Shutdown(ctx)
		return nil, fmt.Errorf("creating metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		if err := tp.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutting down tracer: %w", err)
		}
		if err := mp.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutting down meter: %w", err)
		}
		return nil
	}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/infrastructure/observability/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Add OTelEnabled to config**

Modify `internal/infrastructure/config/config.go`:

Add to `Config` struct:
```go
OTelEnabled bool
```

Add to `Load()` return:
```go
OTelEnabled: envBool("OTEL_ENABLED", false),
```

Add helper:
```go
func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return fallback
}
```

Update the doc comment at the top of the file to include `OTEL_ENABLED`.

- [ ] **Step 7: Verify full build + existing tests pass**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS (488+ tests, zero regressions)

- [ ] **Step 8: Commit**

```bash
git add internal/infrastructure/observability/ internal/infrastructure/config/config.go go.mod go.sum
git commit -m "feat(otel): add OTel setup with OTEL_ENABLED flag and OTLP exporters"
```

---

## Task 2: Tracing Decorators (B2)

**Files:**
- Create: `internal/adapters/inbound/tracing/governance_traced.go`
- Create: `internal/adapters/inbound/tracing/governance_traced_test.go`
- Create: `internal/adapters/inbound/tracing/workflow_traced.go`
- Create: `internal/adapters/inbound/tracing/workflow_traced_test.go`
- Create: `internal/adapters/inbound/tracing/approval_traced.go`
- Create: `internal/adapters/inbound/tracing/escalation_traced.go`
- Create: `internal/adapters/inbound/tracing/query_traced.go`

Each decorator implements its inbound port interface, wraps a `next` implementation, and adds OTel spans.

- [ ] **Step 1: Write failing test for TracedGovernanceService**

```go
// internal/adapters/inbound/tracing/governance_traced_test.go
package tracing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/adapters/inbound/tracing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTracer(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyntheticBatchSpanProcessor(exporter))
	t.Cleanup(func() { tp.Shutdown(context.Background()) })
	return exporter, tp
}

func TestTracedGovernanceService_SubmitTask_Success(t *testing.T) {
	exporter, tp := setupTracer(t)

	mockTask := &task.Task{} // use fixtures.NewTestTask() if available
	mock := &mockGovernanceService{
		submitResult: mockTask,
	}

	traced := tracing.NewTracedGovernanceService(mock, tp.Tracer("test"))
	result, err := traced.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "test task",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})

	require.NoError(t, err)
	assert.Equal(t, mockTask, result)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GovernanceService.SubmitTask", spans[0].Name)

	// Verify attributes
	attrs := spans[0].Attributes
	assertHasAttribute(t, attrs, "governance.action", "SubmitTask")
	assertHasAttribute(t, attrs, "governance.task_type", "development")
	assertHasAttribute(t, attrs, "governance.task_scope", "file")
}

func TestTracedGovernanceService_SubmitTask_Error(t *testing.T) {
	exporter, tp := setupTracer(t)

	expectedErr := errors.New("validation failed")
	mock := &mockGovernanceService{submitErr: expectedErr}

	traced := tracing.NewTracedGovernanceService(mock, tp.Tracer("test"))
	_, err := traced.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:  task.TypeDevelopment,
		Title: "test",
		Scope: task.ScopeFile,
	})

	assert.ErrorIs(t, err, expectedErr)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

// assertHasAttribute checks that an attribute with the given key and string value exists.
func assertHasAttribute(t *testing.T, attrs []attribute.KeyValue, key, value string) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			assert.Equal(t, value, a.Value.AsString())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

// mockGovernanceService for testing
type mockGovernanceService struct {
	submitResult *task.Task
	submitErr    error
	// Add other fields as needed for other methods
}

func (m *mockGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	return m.submitResult, m.submitErr
}
// Implement remaining GovernanceService methods returning nil/zero values
```

- [ ] **Step 2: Implement TracedGovernanceService**

```go
// internal/adapters/inbound/tracing/governance_traced.go
package tracing

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var _ inbound.GovernanceService = (*TracedGovernanceService)(nil)

type TracedGovernanceService struct {
	next   inbound.GovernanceService
	tracer trace.Tracer
}

func NewTracedGovernanceService(next inbound.GovernanceService, tracer trace.Tracer) *TracedGovernanceService {
	return &TracedGovernanceService{next: next, tracer: tracer}
}

func (t *TracedGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.SubmitTask",
		trace.WithAttributes(
			attribute.String("governance.action", "SubmitTask"),
			attribute.String("governance.task_type", string(input.Type)),
			attribute.String("governance.task_scope", string(input.Scope)),
		),
	)
	defer span.End()

	result, err := t.next.SubmitTask(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.task_id", result.ID().String()),
		attribute.String("governance.risk_level", string(result.RiskLevel())),
		attribute.String("governance.outcome", "created"),
	)
	return result, nil
}

func (t *TracedGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.ProcessTask",
		trace.WithAttributes(
			attribute.String("governance.action", "ProcessTask"),
			attribute.String("governance.task_type", string(input.Type)),
			attribute.String("governance.task_scope", string(input.Scope)),
		),
	)
	defer span.End()

	result, err := t.next.ProcessTask(ctx, input, action)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.task_id", result.Task.ID().String()),
		attribute.String("governance.outcome", "processed"),
	)
	return result, nil
}

func (t *TracedGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.RouteTask",
		trace.WithAttributes(
			attribute.String("governance.action", "RouteTask"),
			attribute.String("governance.task_id", taskID.String()),
		),
	)
	defer span.End()

	result, err := t.next.RouteTask(ctx, taskID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.strategy", string(result.SelectedStrategy())),
		attribute.Bool("governance.strategy_overridden", result.EvaluatedStrategies()[0].Overridden),
		attribute.String("governance.agent_role", string(result.SelectedAgentRole())),
		attribute.String("governance.outcome", string(result.SelectedStrategy())),
	)
	return result, nil
}

func (t *TracedGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.EvaluatePolicy",
		trace.WithAttributes(
			attribute.String("governance.action", "EvaluatePolicy"),
			attribute.String("governance.task_id", taskID.String()),
		),
	)
	defer span.End()

	result, err := t.next.EvaluatePolicy(ctx, taskID, action)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.policy_outcome", string(result.Outcome())),
		attribute.String("governance.outcome", string(result.Outcome())),
	)
	return result, nil
}

func (t *TracedGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	ctx, span := t.tracer.Start(ctx, "GovernanceService.StartWorkflow",
		trace.WithAttributes(
			attribute.String("governance.action", "StartWorkflow"),
			attribute.String("governance.task_id", taskID.String()),
		),
	)
	defer span.End()

	result, err := t.next.StartWorkflow(ctx, taskID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.String("governance.workflow_run_id", result.ID.String()),
		attribute.String("governance.workflow_status", string(result.Status)),
		attribute.String("governance.outcome", string(result.Status)),
	)
	return result, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/adapters/inbound/tracing/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Implement TracedWorkflowControl with tests**

Same decorator pattern for `WorkflowControl` interface. For `RegisterAttempt`, include `governance.attempt_status` attribute. If failure, add `governance.failure_stage` and `governance.failure_category` (extract category from failure_code by splitting on `/`). Omit failure attributes when status is success.

```go
// internal/adapters/inbound/tracing/workflow_traced.go
// TracedWorkflowControl wraps WorkflowControl with tracing spans.

func (t *TracedWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	ctx, span := t.tracer.Start(ctx, "WorkflowControl.RegisterAttempt",
		trace.WithAttributes(
			attribute.String("governance.action", "RegisterAttempt"),
			attribute.String("governance.workflow_run_id", id.String()),
			attribute.String("governance.attempt_status", string(result.Status)),
		),
	)
	defer span.End()

	// Add failure attributes only when applicable (omit on success)
	if result.FailureStage != nil {
		span.SetAttributes(attribute.String("governance.failure_stage", result.FailureStage.String()))
	}
	if result.FailureCode != nil {
		category := extractCategory(*result.FailureCode)
		span.SetAttributes(attribute.String("governance.failure_category", category))
	}

	wf, err := t.next.RegisterAttempt(ctx, id, result)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", string(wf.Status)))
	return wf, nil
}

// extractCategory gets the category prefix from a failure_code (e.g. "tool" from "tool/shell_timeout").
func extractCategory(code string) string {
	if i := strings.Index(code, "/"); i >= 0 {
		return code[:i]
	}
	return code
}
```

KillWorkflow span includes `governance.kill_reason`. PauseWorkflow/ResumeWorkflow are straightforward.

Test file verifies: span name, attributes present, error handling, attribute omission on success.

- [ ] **Step 5: Implement remaining 3 traced decorators**

`TracedApprovalService`, `TracedEscalationPort`, `TracedQueryService` — follow same pattern. These are simpler (fewer methods, fewer attributes). Each gets a compile-time interface check `var _ inbound.XxxService = (*TracedXxxService)(nil)`.

- [ ] **Step 6: Verify build + all tests**

Run: `go build ./... && go test ./internal/adapters/inbound/tracing/... -v -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/adapters/inbound/tracing/
git commit -m "feat(otel): add tracing decorators for all inbound ports"
```

---

## Task 3: Metrics Instruments + Decorators (B3)

**Files:**
- Create: `internal/adapters/inbound/metrics/instruments.go`
- Create: `internal/adapters/inbound/metrics/governance_metrics.go`
- Create: `internal/adapters/inbound/metrics/governance_metrics_test.go`
- Create: `internal/adapters/inbound/metrics/workflow_metrics.go`
- Create: `internal/adapters/inbound/metrics/workflow_metrics_test.go`

- [ ] **Step 1: Define all metric instruments**

```go
// internal/adapters/inbound/metrics/instruments.go
package metrics

import (
	"go.opentelemetry.io/otel/metric"
)

// Instruments holds all governance metric instruments.
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
}

// NewInstruments creates all metric instruments from a Meter.
func NewInstruments(meter metric.Meter) (*Instruments, error) {
	tasksSubmitted, err := meter.Int64Counter("governance.tasks.submitted",
		metric.WithDescription("Tasks ingested"))
	if err != nil {
		return nil, err
	}

	tasksCompleted, err := meter.Int64Counter("governance.tasks.completed",
		metric.WithDescription("Tasks reaching terminal state"))
	if err != nil {
		return nil, err
	}

	routingDuration, err := meter.Float64Histogram("governance.routing.duration_ms",
		metric.WithDescription("Routing evaluation latency in ms"),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}

	policyDuration, err := meter.Float64Histogram("governance.policy.duration_ms",
		metric.WithDescription("Policy evaluation latency in ms"),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}

	policyOutcomes, err := meter.Int64Counter("governance.policy.outcomes",
		metric.WithDescription("Distribution of policy outcomes"))
	if err != nil {
		return nil, err
	}

	workflowDuration, err := meter.Float64Histogram("governance.workflow.duration_ms",
		metric.WithDescription("Total workflow lifetime in ms"),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}

	workflowTransitions, err := meter.Int64Counter("governance.workflow.transitions",
		metric.WithDescription("Transition frequency"))
	if err != nil {
		return nil, err
	}

	approvalWait, err := meter.Float64Histogram("governance.approval.wait_ms",
		metric.WithDescription("Time approval was pending in ms"),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}

	executionAttempts, err := meter.Float64Histogram("governance.execution.attempts",
		metric.WithDescription("Attempts per execution"))
	if err != nil {
		return nil, err
	}

	executionFailures, err := meter.Int64Counter("governance.execution.failures",
		metric.WithDescription("Failure distribution"))
	if err != nil {
		return nil, err
	}

	memoryDuration, err := meter.Float64Histogram("governance.memory.duration_ms",
		metric.WithDescription("Memory-engine query latency in ms"),
		metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}

	memoryDegraded, err := meter.Int64Counter("governance.memory.degraded",
		metric.WithDescription("Times memory-engine was unavailable"))
	if err != nil {
		return nil, err
	}

	return &Instruments{
		TasksSubmitted: tasksSubmitted, TasksCompleted: tasksCompleted,
		RoutingDuration: routingDuration, PolicyDuration: policyDuration,
		PolicyOutcomes: policyOutcomes, WorkflowDuration: workflowDuration,
		WorkflowTransitions: workflowTransitions, ApprovalWait: approvalWait,
		ExecutionAttempts: executionAttempts, ExecutionFailures: executionFailures,
		MemoryDuration: memoryDuration, MemoryDegraded: memoryDegraded,
	}, nil
}
```

- [ ] **Step 2: Implement MetricsGovernanceService decorator**

```go
// internal/adapters/inbound/metrics/governance_metrics.go
package metrics

import (
	"context"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var _ inbound.GovernanceService = (*MetricsGovernanceService)(nil)

type MetricsGovernanceService struct {
	next inbound.GovernanceService
	inst *Instruments
}

func NewMetricsGovernanceService(next inbound.GovernanceService, inst *Instruments) *MetricsGovernanceService {
	return &MetricsGovernanceService{next: next, inst: inst}
}

func (m *MetricsGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	result, err := m.next.SubmitTask(ctx, input)
	if err != nil {
		return nil, err
	}
	m.inst.TasksSubmitted.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("type", string(result.Type())),
			attribute.String("scope", string(result.Scope())),
			attribute.String("risk_level", string(result.RiskLevel())),
		),
	)
	return result, nil
}

func (m *MetricsGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	start := time.Now()
	result, err := m.next.RouteTask(ctx, taskID)
	elapsed := float64(time.Since(start).Milliseconds())
	if err != nil {
		return nil, err
	}
	overridden := "false"
	if len(result.EvaluatedStrategies()) > 0 && result.EvaluatedStrategies()[0].Overridden {
		overridden = "true"
	}
	m.inst.RoutingDuration.Record(ctx, elapsed,
		metric.WithAttributes(
			attribute.String("strategy", string(result.SelectedStrategy())),
			attribute.String("overridden", overridden),
		),
	)
	return result, nil
}

func (m *MetricsGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	start := time.Now()
	result, err := m.next.EvaluatePolicy(ctx, taskID, action)
	elapsed := float64(time.Since(start).Milliseconds())
	if err != nil {
		return nil, err
	}
	outcomeAttr := attribute.String("outcome", string(result.Outcome()))
	m.inst.PolicyDuration.Record(ctx, elapsed, metric.WithAttributes(outcomeAttr))
	m.inst.PolicyOutcomes.Add(ctx, 1, metric.WithAttributes(outcomeAttr))
	return result, nil
}

// ProcessTask and StartWorkflow delegate without additional per-call metrics
// (their sub-calls already emit metrics individually)
func (m *MetricsGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	return m.next.ProcessTask(ctx, input, action)
}

func (m *MetricsGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return m.next.StartWorkflow(ctx, taskID)
}
```

- [ ] **Step 3: Write MetricsGovernanceService tests**

Test with OTel SDK's in-memory metric reader. Verify counters increment and histograms record values with correct attributes. Verify no metrics emitted on error.

- [ ] **Step 4: Implement MetricsWorkflowControl decorator**

```go
// internal/adapters/inbound/metrics/workflow_metrics.go
// MetricsWorkflowControl emits per-call metrics for workflow control operations.

func (m *MetricsWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	wf, err := m.next.RegisterAttempt(ctx, id, result)
	if err != nil {
		return nil, err
	}
	// execution.attempts histogram
	// Only emit strategy/agent_role if present on result
	attrs := []attribute.KeyValue{}
	if result.StrategyUsed != nil {
		attrs = append(attrs, attribute.String("strategy", *result.StrategyUsed))
	}
	if result.AgentRole != nil {
		attrs = append(attrs, attribute.String("agent_role", *result.AgentRole))
	}
	if len(attrs) > 0 {
		m.inst.ExecutionAttempts.Record(ctx, 1, metric.WithAttributes(attrs...))
	}
	// workflow.transitions counter — emitted for all control operations
	return wf, nil
}
```

Kill/Pause/Resume emit `workflow.transitions` counter with `from` and `to` labels.

- [ ] **Step 5: Write MetricsWorkflowControl tests + verify build**

Run: `go build ./... && go test ./internal/adapters/inbound/metrics/... -v -count=1`

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/inbound/metrics/
git commit -m "feat(otel): add metric instruments and decorator wrappers"
```

---

## Task 4: Inline Spans + Lifecycle Metrics (B4)

**Files:**
- Modify: `internal/adapters/outbound/memory/stub_provider.go`
- Modify: `internal/adapters/outbound/events/callback_notifier.go`
- Modify: `internal/application/workflowrun/service.go`
- Modify: `internal/application/workflowrun/register_attempt.go`
- Modify: `internal/application/workflowrun/kill_workflow.go`
- Modify: `internal/application/workflowrun/start_workflow.go`
- Modify: `internal/application/approvals/resolve_approval.go`

This task has two parts: inline detail spans in adapters, and lifecycle metric emission in UCs.

- [ ] **Step 1: Add inline span to MemoryContextProvider**

Modify `internal/adapters/outbound/memory/stub_provider.go`:

```go
package memory

import (
	"context"
	"log/slog"

	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var _ outbound.MemoryContextProvider = (*StubMemoryContextProvider)(nil)

type StubMemoryContextProvider struct {
	logger *slog.Logger
	tracer trace.Tracer
}

func NewStubMemoryContextProvider(logger *slog.Logger) *StubMemoryContextProvider {
	return &StubMemoryContextProvider{
		logger: logger,
		tracer: otel.Tracer("governance.memory"),
	}
}

func (p *StubMemoryContextProvider) GetRelevantContext(ctx context.Context, taskID shared.TaskID, query string) (*routing.MemoryContext, error) {
	ctx, span := p.tracer.Start(ctx, "MemoryContextProvider.GetRelevantContext",
		trace.WithAttributes(
			attribute.String("governance.task_id", taskID.String()),
			attribute.Bool("governance.memory_available", false),
		),
	)
	defer span.End()

	p.logger.WarnContext(ctx, "memory-engine unavailable, returning empty context",
		"task_id", taskID.String(),
		"query", query,
		"component", "memory_context_provider",
	)
	return &routing.MemoryContext{}, nil
}
```

- [ ] **Step 2: Add inline spans to CallbackNotifier**

Modify `internal/adapters/outbound/events/callback_notifier.go` — add a tracer field, create spans in each On* method:

```go
func NewCallbackNotifier() *CallbackNotifier {
	return &CallbackNotifier{
		tracer: otel.Tracer("governance.notifier"),
	}
}

func (n *CallbackNotifier) OnExecutionReady(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error {
	ctx, span := n.tracer.Start(ctx, "GovernanceNotifier.OnExecutionReady")
	defer span.End()
	if n.onExecutionReady != nil {
		return n.onExecutionReady(ctx, wf, lease)
	}
	return nil
}
// Same for OnApprovalRequired, OnWorkflowTerminated
```

- [ ] **Step 3: Add Instruments to WorkflowRunService**

Modify `internal/application/workflowrun/service.go` — add an optional `*metrics.Instruments` field:

```go
type WorkflowRunService struct {
	// ... existing fields ...
	metrics *metrics.Instruments // nil when OTel disabled
}
```

Update `NewWorkflowRunService` to accept an optional `*metrics.Instruments` parameter. If nil, no lifecycle metrics are emitted. This keeps backwards compatibility — Phase 1 callers pass nil.

**IMPORTANT:** Import the metrics package from `internal/adapters/inbound/metrics`. The `Instruments` struct is shared between decorators and inline emission.

- [ ] **Step 4: Add lifecycle metrics to RegisterAttempt**

Modify `internal/application/workflowrun/register_attempt.go` — after step 9 (notify if terminal), add:

```go
// 10. Emit lifecycle metrics
if s.metrics != nil {
	if wf.Status.IsTerminal() {
		durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
		s.metrics.WorkflowDuration.Record(ctx, durationMs,
			metric.WithAttributes(attribute.String("final_status", string(wf.Status))))
		s.metrics.TasksCompleted.Add(ctx, 1,
			metric.WithAttributes(attribute.String("final_status", string(wf.Status))))
	}
	if result.FailureStage != nil {
		category := extractCategory(*result.FailureCode)
		s.metrics.ExecutionFailures.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("failure_stage", result.FailureStage.String()),
				attribute.String("failure_category", category),
				attribute.Bool("retryable", *result.Retryable),
			))
	}
}
```

Add helper in the same file:
```go
func extractCategory(code string) string {
	if i := strings.Index(code, "/"); i >= 0 {
		return code[:i]
	}
	return code
}
```

- [ ] **Step 5: Add lifecycle metrics to KillWorkflow**

Modify `internal/application/workflowrun/kill_workflow.go` — after audit + notify, add:

```go
if s.metrics != nil {
	durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
	s.metrics.WorkflowDuration.Record(ctx, durationMs,
		metric.WithAttributes(attribute.String("final_status", "killed")))
	s.metrics.TasksCompleted.Add(ctx, 1,
		metric.WithAttributes(attribute.String("final_status", "killed")))
}
```

- [ ] **Step 6: Add lifecycle metrics to StartWorkflow (deny case)**

Modify `internal/application/workflowrun/start_workflow.go` — in the `OutcomeDeny` case, after audit + notify, add:

```go
if s.metrics != nil {
	durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
	s.metrics.WorkflowDuration.Record(ctx, durationMs,
		metric.WithAttributes(attribute.String("final_status", "failed")))
	s.metrics.TasksCompleted.Add(ctx, 1,
		metric.WithAttributes(attribute.String("final_status", "failed")))
}
```

- [ ] **Step 7: Add lifecycle metrics to ResolveApproval**

Modify `internal/application/approvals/resolve_approval.go` — add `metrics *metrics.Instruments` field to `ApprovalService`. After approval resolution, add:

```go
// Approval wait time
if s.metrics != nil {
	waitMs := float64(time.Since(req.CreatedAt().Time).Milliseconds())
	s.metrics.ApprovalWait.Record(ctx, waitMs,
		metric.WithAttributes(attribute.String("resolution", outcome)))
}

// On denied: also emit workflow.duration_ms and tasks.completed
if !input.Approved && s.metrics != nil {
	durationMs := float64(time.Since(wf.CreatedAt.Time).Milliseconds())
	s.metrics.WorkflowDuration.Record(ctx, durationMs,
		metric.WithAttributes(attribute.String("final_status", "failed")))
	s.metrics.TasksCompleted.Add(ctx, 1,
		metric.WithAttributes(attribute.String("final_status", "failed")))
}
```

- [ ] **Step 8: Update constructors in bootstrap to pass Instruments**

This step just ensures the service constructors accept the new metrics field. The actual wiring happens in Task 5. For now, update the constructors to accept `*metrics.Instruments` (nil = no metrics).

- [ ] **Step 9: Verify build + existing tests pass**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL 488 tests still PASS (metrics field is nil in existing tests)

- [ ] **Step 10: Commit**

```bash
git add internal/adapters/outbound/memory/ internal/adapters/outbound/events/ internal/application/workflowrun/ internal/application/approvals/
git commit -m "feat(otel): add inline spans and lifecycle metric emission"
```

---

## Task 5: Wiring + main.go (B5)

**Files:**
- Modify: `internal/bootstrap/wire.go`
- Modify: `cmd/agent-governance-core/main.go`

- [ ] **Step 1: Update bootstrap Wire function**

Modify `internal/bootstrap/wire.go`:

1. Accept `config.Config` instead of just pool+logger (needs OTelEnabled)
2. Call `observability.SetupOTel(ctx, cfg.OTelEnabled, "agent-governance-core")` at the start
3. If OTel enabled, create `Instruments` and wrap services with decorators
4. Pass `Instruments` to WorkflowRunService and ApprovalService constructors
5. Return OTel shutdown function in `App`

```go
type App struct {
	HTTPServer   *httpAdapter.Server
	Facade       *sdk.GovernanceFacade
	Notifier     *events.CallbackNotifier
	OTelShutdown func(context.Context) error
}

func Wire(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, cfg config.Config) (*App, error) {
	// 1. OTel setup
	otelShutdown, err := observability.SetupOTel(ctx, cfg.OTelEnabled, "agent-governance-core")
	if err != nil {
		return nil, fmt.Errorf("otel setup: %w", err)
	}

	// ... existing repo + service wiring ...

	// 2. Create instruments (nil-safe when OTel disabled — instruments still work as no-op)
	var inst *metrics.Instruments
	if cfg.OTelEnabled {
		meter := otel.Meter("governance")
		inst, err = metrics.NewInstruments(meter)
		if err != nil {
			return nil, fmt.Errorf("creating metrics instruments: %w", err)
		}
	}

	// 3. Pass instruments to services that emit lifecycle metrics
	workflowSvc := workflowrun.NewWorkflowRunService(..., inst) // add inst parameter
	approvalSvc := approvals.NewApprovalService(..., inst)       // add inst parameter

	// 4. Wrap with decorators (inside-out: metrics inner, tracing outer)
	var govSvc inbound.GovernanceService = &governanceServiceAdapter{...}
	var wfCtrl inbound.WorkflowControl = workflowSvc
	// ... etc

	if cfg.OTelEnabled {
		tracer := otel.Tracer("governance")
		govSvc = metrics.NewMetricsGovernanceService(govSvc, inst)
		govSvc = tracing.NewTracedGovernanceService(govSvc, tracer)
		wfCtrl = metrics.NewMetricsWorkflowControl(wfCtrl, inst)
		wfCtrl = tracing.NewTracedWorkflowControl(wfCtrl, tracer)
		// approval, escalation, query — tracing only (no metrics decorator needed)
		approvalPort = tracing.NewTracedApprovalService(approvalPort, tracer)
		escalationPort = tracing.NewTracedEscalationPort(escalationPort, tracer)
		queryPort = tracing.NewTracedQueryService(queryPort, tracer)
	}

	return &App{
		HTTPServer:   httpAdapter.NewServer(govSvc, wfCtrl, approvalPort, queryPort, escalationPort),
		Facade:       sdk.NewGovernanceFacade(govSvc, wfCtrl, approvalPort, queryPort, escalationPort),
		Notifier:     notifier,
		OTelShutdown: otelShutdown,
	}, nil
}
```

- [ ] **Step 2: Update main.go for OTel shutdown**

Modify `cmd/agent-governance-core/main.go`:

```go
func run() error {
	cfg := config.Load()
	// ... logger setup ...

	pool, err := database.NewPool(ctx, cfg.DB)
	// ...

	app, err := bootstrap.Wire(ctx, pool, logger, cfg)
	if err != nil {
		return err
	}
	defer app.OTelShutdown(ctx) // Flush OTel on shutdown

	// ... server setup, signal handling, shutdown ...
}
```

- [ ] **Step 3: Verify build + all tests pass**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL tests PASS

- [ ] **Step 4: Verify binary starts with OTEL_ENABLED=false**

```bash
make build
./bin/agent-governance-core  # Should behave exactly like Phase 1
```

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/ cmd/agent-governance-core/main.go
git commit -m "feat(otel): wire OTel decorators and shutdown in bootstrap"
```

---

## Task 6: Integration Test (B6)

**Files:**
- Create: `test/integration/observability/otel_integration_test.go`

- [ ] **Step 1: Write span tree verification test**

```go
//go:build integration

package observability_test

// This test wires the full pipeline with in-memory OTel exporters,
// executes a ProcessTask happy path, and verifies:
// 1. Span tree structure (ProcessTask → SubmitTask → RouteTask → EvaluatePolicy → StartWorkflow)
// 2. trace_id appears in AuditContext of audit entries
// 3. Metrics are emitted (tasks.submitted, routing.duration_ms, policy.outcomes)

// Setup:
// - Create in-memory trace exporter + TracerProvider
// - Create in-memory metric reader + MeterProvider
// - Wire services with tracing + metrics decorators
// - Wire with real PG (testcontainers)
// - Execute ProcessTask
// - Collect spans and verify tree
// - Query audit entries and verify trace_id present
// - Read metrics and verify counters/histograms have data
```

The test should:
1. Set up testcontainers PG (reuse `testhelpers.NewTestDB`)
2. Create OTel in-memory trace exporter + metric reader
3. Wire the full application with decorators enabled
4. Execute a happy-path task (bugfix/file/low risk)
5. Verify span tree has correct parent-child relationships
6. Query audit entries and assert `trace_id` key is present in AuditContext
7. Read metric data and verify `governance.tasks.submitted` counter > 0

- [ ] **Step 2: Run integration test**

Run: `go test ./test/integration/observability/... -v -count=1 -tags=integration`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/observability/
git commit -m "feat(otel): add integration test verifying span tree and audit correlation"
```

---

## Verification Checklist

After all tasks complete:

- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./internal/... ./test/fixtures/... -count=1` — ALL PASS (488+ tests, zero regressions)
- [ ] `go test ./test/integration/... -v -count=1 -tags=integration` — ALL PASS (52+ repo tests + 8 UC tests + OTel test)
- [ ] Binary starts with `OTEL_ENABLED=false` — identical to Phase 1
- [ ] Binary starts with `OTEL_ENABLED=true` — creates spans and metrics (exports to configured endpoint)
- [ ] Span tree: ProcessTask → SubmitTask → RouteTask → EvaluatePolicy → StartWorkflow
- [ ] trace_id appears in AuditContext when OTel enabled
- [ ] Lifecycle metrics emit on terminal states (completed/failed/killed)
- [ ] Failure metrics use `failure_category` (not full code) as label
- [ ] No empty string attributes in spans or metrics
