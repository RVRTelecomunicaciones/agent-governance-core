package audit

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// AuditContext holds structured contextual data for audit entries.
// Use builder methods to populate it consistently and prevent key drift.
type AuditContext map[string]any

// NewAuditContext creates an empty AuditContext ready for builder chaining.
func NewAuditContext() AuditContext {
	return make(AuditContext)
}

// WithTraceInfo extracts trace_id and span_id from the OTel span in context.
// If no valid span is present (OTel not configured), no keys are added.
func (c AuditContext) WithTraceInfo(ctx context.Context) AuditContext {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if sc.IsValid() {
		c["trace_id"] = sc.TraceID().String()
		c["span_id"] = sc.SpanID().String()
	}
	return c
}

// WithFailure adds failure information to the audit context.
func (c AuditContext) WithFailure(stage, code string, retryable bool) AuditContext {
	c["failure_stage"] = stage
	c["failure_code"] = code
	c["retryable"] = retryable
	return c
}

// WithToolName sets the tool name in the audit context.
func (c AuditContext) WithToolName(name string) AuditContext {
	c["tool_name"] = name
	return c
}

// WithStrategy sets the strategy used in the audit context.
func (c AuditContext) WithStrategy(strategy string) AuditContext {
	c["strategy_used"] = strategy
	return c
}

// WithAgentRole sets the agent role in the audit context.
func (c AuditContext) WithAgentRole(role string) AuditContext {
	c["agent_role"] = role
	return c
}

// WithLeaseState adds execution lease state information.
func (c AuditContext) WithLeaseState(attemptsUsed, retryBudget int, timeElapsedMs int64) AuditContext {
	c["attempts_used"] = attemptsUsed
	c["retry_budget"] = retryBudget
	c["time_elapsed_ms"] = timeElapsedMs
	return c
}

// Set adds a custom key-value pair to the audit context.
func (c AuditContext) Set(key string, value any) AuditContext {
	c[key] = value
	return c
}
