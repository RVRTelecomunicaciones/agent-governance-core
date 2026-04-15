package memory

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.MemoryContextProvider = (*StubMemoryContextProvider)(nil)

// StubMemoryContextProvider is a degradable stub that returns empty context
// when the memory-engine is unavailable. It logs clearly for traceability.
type StubMemoryContextProvider struct {
	logger *slog.Logger
	tracer trace.Tracer
}

// NewStubMemoryContextProvider creates a new StubMemoryContextProvider with the given logger.
func NewStubMemoryContextProvider(logger *slog.Logger) *StubMemoryContextProvider {
	return &StubMemoryContextProvider{
		logger: logger,
		tracer: otel.Tracer("governance.memory"),
	}
}

// GetRelevantContext always returns an empty MemoryContext and logs a warning.
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
