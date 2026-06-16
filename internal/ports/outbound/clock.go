package outbound

import "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"

// Clock abstracts time for testability.
type Clock interface {
	Now() shared.Timestamp
}
