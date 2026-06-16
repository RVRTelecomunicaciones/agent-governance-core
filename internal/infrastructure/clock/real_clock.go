package clock

import (
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// RealClock provides the actual system time.
type RealClock struct{}

// Now returns the current time as a shared.Timestamp.
func (c RealClock) Now() shared.Timestamp {
	return shared.MustTimestamp(time.Now())
}
