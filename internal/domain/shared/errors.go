package shared

import "errors"

var (
	ErrEmptyID       = errors.New("id must not be empty")
	ErrInvalidULID   = errors.New("id must be a valid ULID")
	ErrZeroTimestamp = errors.New("timestamp must not be zero")
)
