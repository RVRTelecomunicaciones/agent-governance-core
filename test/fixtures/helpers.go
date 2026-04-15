package fixtures

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// newULID generates a fresh ULID string for test IDs.
func newULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// now returns the current time as a shared.Timestamp. Panics if time is zero (never in practice).
func now() shared.Timestamp {
	return shared.MustTimestamp(time.Now())
}
