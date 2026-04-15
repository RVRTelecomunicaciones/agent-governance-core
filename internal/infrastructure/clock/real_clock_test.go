package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRealClock_Now(t *testing.T) {
	c := RealClock{}

	before := time.Now()
	ts := c.Now()
	after := time.Now()

	assert.False(t, ts.IsZero(), "timestamp should not be zero")
	assert.False(t, ts.Time.Before(before), "timestamp should not be before the call")
	assert.False(t, ts.Time.After(after), "timestamp should not be after the call")
}
