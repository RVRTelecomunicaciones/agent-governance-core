package memory

import (
	"context"
	"log/slog"

	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

var _ outbound.MemoryContextProvider = (*StubMemoryContextProvider)(nil)

// StubMemoryContextProvider is a degradable stub that returns empty context
// when the memory-engine is unavailable. It logs clearly for traceability.
type StubMemoryContextProvider struct {
	logger *slog.Logger
}

// NewStubMemoryContextProvider creates a new StubMemoryContextProvider with the given logger.
func NewStubMemoryContextProvider(logger *slog.Logger) *StubMemoryContextProvider {
	return &StubMemoryContextProvider{logger: logger}
}

// GetRelevantContext always returns an empty MemoryContext and logs a warning.
func (p *StubMemoryContextProvider) GetRelevantContext(ctx context.Context, taskID shared.TaskID, query string) (*routing.MemoryContext, error) {
	p.logger.WarnContext(ctx, "memory-engine unavailable, returning empty context",
		"task_id", taskID.String(),
		"query", query,
		"component", "memory_context_provider",
	)
	return &routing.MemoryContext{}, nil
}
