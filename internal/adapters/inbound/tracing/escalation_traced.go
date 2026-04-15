package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	escalationdomain "github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

var _ inbound.EscalationPort = (*TracedEscalationPort)(nil)

// TracedEscalationPort wraps an EscalationPort with OpenTelemetry tracing.
type TracedEscalationPort struct {
	next   inbound.EscalationPort
	tracer trace.Tracer
}

// NewTracedEscalationPort creates a new tracing decorator for EscalationPort.
func NewTracedEscalationPort(next inbound.EscalationPort, tracer trace.Tracer) *TracedEscalationPort {
	return &TracedEscalationPort{next: next, tracer: tracer}
}

func (t *TracedEscalationPort) TriggerEscalation(
	ctx context.Context,
	taskID shared.TaskID,
	condition escalationdomain.EscalationCondition,
	target escalationdomain.EscalationTarget,
) (*escalationdomain.EscalationTrigger, error) {
	ctx, span := t.tracer.Start(ctx, "EscalationPort.TriggerEscalation",
		trace.WithAttributes(
			attribute.String("governance.action", "TriggerEscalation"),
			attribute.String("governance.task_id", string(taskID)),
		),
	)
	defer span.End()

	result, err := t.next.TriggerEscalation(ctx, taskID, condition, target)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, nil
}
