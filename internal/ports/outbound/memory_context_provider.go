package outbound

import (
	"context"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// MemoryContextProvider defines the integration point with the memory engine.
// This dependency is degradable — implementations should handle unavailability gracefully.
type MemoryContextProvider interface {
	GetRelevantContext(ctx context.Context, taskID shared.TaskID, query string) (*routing.MemoryContext, error)
}
