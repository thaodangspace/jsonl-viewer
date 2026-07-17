// Package history provides cursor-based paginated history retrieval for agent session JSONL files.
package history

import "errors"

// Sentinel errors returned by the history service.
var (
	// ErrSessionNotFound is returned when the session file does not exist.
	ErrSessionNotFound = errors.New("session not found")

	// ErrInvalidCursor is returned when a cursor is malformed or belongs to a different session.
	ErrInvalidCursor = errors.New("invalid cursor")

	// ErrHistoryChanged is returned when the session file has been truncated or replaced,
	// making the cursor unsafe to use.
	ErrHistoryChanged = errors.New("history changed: file was truncated or replaced")

	// ErrReadFailed is returned when an unexpected I/O error occurs while reading the session file.
	ErrReadFailed = errors.New("failed to read session file")
)
