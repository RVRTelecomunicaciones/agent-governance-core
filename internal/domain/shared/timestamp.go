package shared

import "time"

// Timestamp wraps time.Time with a non-zero constraint.
type Timestamp struct {
	time.Time
}

// NewTimestamp creates a Timestamp, returning an error if t is zero.
func NewTimestamp(t time.Time) (Timestamp, error) {
	if t.IsZero() {
		return Timestamp{}, ErrZeroTimestamp
	}
	return Timestamp{Time: t}, nil
}

// MustTimestamp creates a Timestamp, panicking if t is zero. For tests only.
func MustTimestamp(t time.Time) Timestamp {
	ts, err := NewTimestamp(t)
	if err != nil {
		panic(err)
	}
	return ts
}
