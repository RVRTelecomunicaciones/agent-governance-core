package persistence

import "time"

// mustParseTime parses an RFC3339Nano timestamp string.
// It panics if parsing fails — only use for data written by this package.
func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic("persistence: invalid timestamp in JSONB: " + s)
	}
	return t
}
