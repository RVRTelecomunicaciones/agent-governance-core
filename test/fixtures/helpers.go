package fixtures

import (
	"crypto/rand"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/oklog/ulid/v2"
)

// newULID generates a fresh ULID string for test IDs.
func newULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// NewULID generates a fresh ULID string for test IDs (exported for integration tests).
func NewULID() string {
	return newULID()
}

// now returns the current time as a shared.Timestamp. Panics if time is zero (never in practice).
func now() shared.Timestamp {
	return shared.MustTimestamp(time.Now())
}

// Now returns the current time as a shared.Timestamp (exported for integration tests).
func Now() shared.Timestamp {
	return now()
}
