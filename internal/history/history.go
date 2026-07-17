// Package history provides cursor-based paginated history retrieval for agent session JSONL files.
//
// It reads session files backward to find conversation turn boundaries, then decodes
// the selected range forward through agent-specific normalizers to produce chronological,
// renderable events without loading the entire file.
package history

// Service provides paginated history access for agent session files.
// It is stateless; each call is self-contained and reads from the filesystem.
type Service struct {
	// DefaultLimit is the number of turns per page when no limit is specified.
	DefaultLimit int

	// MaxPageTurns caps the requested limit.
	MaxPageTurns int

	// MaxRecordSize is the safety bound for a single JSONL record in bytes.
	// Records exceeding this are included atomically but logged.
	MaxRecordSize int64
}

// NewService creates a history Service with sensible defaults.
func NewService() *Service {
	return &Service{
		DefaultLimit:  20,
		MaxPageTurns:  50,
		MaxRecordSize: 16 * 1024 * 1024, // 16 MB
	}
}
