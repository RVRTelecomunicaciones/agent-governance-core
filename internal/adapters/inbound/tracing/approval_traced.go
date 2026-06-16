package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
)

var _ inbound.ApprovalService = (*TracedApprovalService)(nil)

// TracedApprovalService wraps an ApprovalService with OpenTelemetry tracing.
type TracedApprovalService struct {
	next   inbound.ApprovalService
	tracer trace.Tracer
}

// NewTracedApprovalService creates a new tracing decorator for ApprovalService.
func NewTracedApprovalService(next inbound.ApprovalService, tracer trace.Tracer) *TracedApprovalService {
	return &TracedApprovalService{next: next, tracer: tracer}
}

func (t *TracedApprovalService) ResolveApproval(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
	ctx, span := t.tracer.Start(ctx, "ApprovalService.ResolveApproval",
		trace.WithAttributes(
			attribute.String("governance.action", "ResolveApproval"),
			attribute.String("governance.approval_request_id", string(input.ApprovalRequestID)),
			attribute.Bool("governance.approved", input.Approved),
		),
	)
	defer span.End()

	result, err := t.next.ResolveApproval(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, nil
}

func (t *TracedApprovalService) GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error) {
	ctx, span := t.tracer.Start(ctx, "ApprovalService.GetPendingApprovals",
		trace.WithAttributes(
			attribute.String("governance.action", "GetPendingApprovals"),
		),
	)
	defer span.End()

	result, err := t.next.GetPendingApprovals(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.String("governance.outcome", "success"))

	return result, nil
}
