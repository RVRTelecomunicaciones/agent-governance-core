package outbound

import "github.com/russellcxl/agent-governance-core/internal/domain/shared"

// Clock abstracts time for testability.
type Clock interface {
	Now() shared.Timestamp
}
