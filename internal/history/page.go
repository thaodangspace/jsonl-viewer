package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"agent-reader/internal/jsonl"
)

// Page is a single page of conversation history in chronological order.
type Page struct {
	Events     json.RawMessage `json:"events"`      // array of normalized event objects
	NextCursor string          `json:"next_cursor"`  // cursor for the previous page, or empty
	HasMore    bool            `json:"has_more"`     // true if older history exists before this page
	Snapshot   Snapshot        `json:"snapshot"`
}

// Snapshot contains metadata about the session at the time the page was created.
type Snapshot struct {
	LineCount int `json:"line_count"`
}

// LoadLatest returns the most recent page of conversation turns.
func (s *Service) LoadLatest(sessionID, agent, filePath string, limit int) (*Page, error) {
	if limit <= 0 {
		limit = s.DefaultLimit
	}
	if limit > s.MaxPageTurns {
		limit = s.MaxPageTurns
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("%w: stat: %v", ErrReadFailed, err)
	}

	fileSize := fi.Size()
	if fileSize == 0 {
		return s.emptyPage(), nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %v", ErrReadFailed, err)
	}
	defer f.Close()

	// Scan backward from EOF to find the last `limit` renderable turns
	startOffset, turnsFound, err := scanTurnsBackward(f, fileSize, fileSize, limit, agent)
	if err != nil {
		return nil, fmt.Errorf("%w: backward scan: %v", ErrReadFailed, err)
	}

	// If no renderable turns found, return empty page
	if startOffset < 0 || turnsFound == 0 {
		return s.emptyPage(), nil
	}

	// Read forward from startOffset, producing normalized chronological events
	events, err := s.decodeForward(agent, filePath, startOffset, fileSize)
	if err != nil {
		return nil, err
	}

	// Wrap events in JSON array
	eventsJSON := s.marshalEvents(events)

	// Count total lines in the session file for the snapshot
	lineCount, _ := countFileLines(filePath)

	hasMore := turnsFound >= limit && startOffset > 0
	var nextCursor string
	if hasMore {
		nextCursor = EncodeCursor(sessionID, startOffset, fileSize)
	}

	return &Page{
		Events:     eventsJSON,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Snapshot:   Snapshot{LineCount: lineCount},
	}, nil
}

// LoadPrevious returns the page of conversation turns immediately before the given cursor.
func (s *Service) LoadPrevious(sessionID, agent, filePath, cursorStr string, limit int) (*Page, error) {
	if limit <= 0 {
		limit = s.DefaultLimit
	}
	if limit > s.MaxPageTurns {
		limit = s.MaxPageTurns
	}

	cs, err := DecodeCursor(cursorStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if cs.SessionID != sessionID {
		return nil, fmt.Errorf("%w: cursor belongs to session %q, not %q", ErrInvalidCursor, cs.SessionID, sessionID)
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("%w: stat: %v", ErrReadFailed, err)
	}

	// Detect truncation: if the file is now smaller than when the cursor was created,
	// the byte offset may point past EOF or into unrelated content.
	if fi.Size() < cs.FileSize {
		return nil, ErrHistoryChanged
	}

	// Clamp cursor offset to current file size (file may have grown but our cursor
	// offset must still be within bounds).
	endOffset := cs.ByteOffset
	if endOffset > fi.Size() {
		endOffset = fi.Size()
	}
	if endOffset <= 0 {
		return s.emptyPage(), nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %v", ErrReadFailed, err)
	}
	defer f.Close()

	// Scan backward from the cursor's byte offset
	startOffset, turnsFound, err := scanTurnsBackward(f, fi.Size(), endOffset, limit, agent)
	if err != nil {
		return nil, fmt.Errorf("%w: backward scan: %v", ErrReadFailed, err)
	}

	if startOffset < 0 || turnsFound == 0 {
		return s.emptyPage(), nil
	}

	// Read forward from startOffset to endOffset (the cursor boundary)
	events, err := s.decodeForward(agent, filePath, startOffset, endOffset)
	if err != nil {
		return nil, err
	}

	eventsJSON := s.marshalEvents(events)

	lineCount, _ := countFileLines(filePath)

	hasMore := turnsFound >= limit && startOffset > 0
	var nextCursor string
	if hasMore {
		nextCursor = EncodeCursor(sessionID, startOffset, fi.Size())
	}

	return &Page{
		Events:     eventsJSON,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Snapshot:   Snapshot{LineCount: lineCount},
	}, nil
}

// decodeForward reads the file from startOffset to endOffset and produces
// normalized, chronological events using the agent-appropriate decoder.
func (s *Service) decodeForward(agent, filePath string, startOffset, endOffset int64) ([]json.RawMessage, error) {
	switch agent {
	case "claude":
		return s.decodeClaudeForward(filePath, startOffset, endOffset)
	case "codex":
		return s.decodeCodexForward(filePath, startOffset, endOffset)
	default:
		return s.decodePiForward(filePath, startOffset, endOffset)
	}
}

func (s *Service) decodePiForward(filePath string, startOffset, endOffset int64) ([]json.RawMessage, error) {
	dec, err := jsonl.NewDecoder(filePath, startOffset)
	if err != nil {
		return nil, fmt.Errorf("%w: create decoder: %v", ErrReadFailed, err)
	}
	defer dec.Close()

	var events []json.RawMessage
	for {
		if dec.Offset() >= endOffset {
			break
		}
		event, err := dec.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: decode: %v", ErrReadFailed, err)
		}
		if event == nil || event.Raw == nil {
			continue
		}
		events = append(events, event.Raw)
	}
	return events, nil
}

func (s *Service) decodeClaudeForward(filePath string, startOffset, endOffset int64) ([]json.RawMessage, error) {
	dec, err := jsonl.NewClaudeDecoder(filePath, startOffset)
	if err != nil {
		return nil, fmt.Errorf("%w: create claude decoder: %v", ErrReadFailed, err)
	}
	defer dec.Close()

	var events []json.RawMessage
	for {
		if dec.Offset() >= endOffset {
			break
		}
		event, err := dec.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: claude decode: %v", ErrReadFailed, err)
		}
		if event == nil || event.Raw == nil {
			continue
		}
		events = append(events, event.Raw)
	}
	return events, nil
}

func (s *Service) decodeCodexForward(filePath string, startOffset, endOffset int64) ([]json.RawMessage, error) {
	dec, err := jsonl.NewCodexDecoder(filePath, startOffset)
	if err != nil {
		return nil, fmt.Errorf("%w: create codex decoder: %v", ErrReadFailed, err)
	}
	defer dec.Close()

	var events []json.RawMessage
	for {
		if dec.Offset() >= endOffset {
			break
		}
		event, err := dec.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: codex decode: %v", ErrReadFailed, err)
		}
		if event == nil || event.Raw == nil {
			continue
		}
		events = append(events, event.Raw)
	}
	return events, nil
}

func (s *Service) marshalEvents(events []json.RawMessage) json.RawMessage {
	if len(events) == 0 {
		return json.RawMessage("[]")
	}
	// Build JSON array efficiently
	buf := make([]byte, 0, 1024)
	buf = append(buf, '[')
	for i, ev := range events {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, ev...)
	}
	buf = append(buf, ']')
	return json.RawMessage(buf)
}

func (s *Service) emptyPage() *Page {
	return &Page{
		Events:     json.RawMessage("[]"),
		NextCursor: "",
		HasMore:    false,
		Snapshot:   Snapshot{LineCount: 0},
	}
}

// countFileLines counts the total number of lines in a file.
func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
