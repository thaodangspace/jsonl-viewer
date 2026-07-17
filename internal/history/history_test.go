package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	orig := cursor{SessionID: "sess-1", ByteOffset: 12345, FileSize: 99999}
	encoded := EncodeCursor(orig.SessionID, orig.ByteOffset, orig.FileSize)
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.SessionID != orig.SessionID {
		t.Fatalf("sessionID: got %q, want %q", decoded.SessionID, orig.SessionID)
	}
	if decoded.ByteOffset != orig.ByteOffset {
		t.Fatalf("byteOffset: got %d, want %d", decoded.ByteOffset, orig.ByteOffset)
	}
	if decoded.FileSize != orig.FileSize {
		t.Fatalf("fileSize: got %d, want %d", decoded.FileSize, orig.FileSize)
	}
}

func TestDecodeCursor_Invalid(t *testing.T) {
	tests := []string{
		"not-base64!!!",
		"", // empty
		EncodeCursor("", 0, 0), // empty session
	}
	for i, tc := range tests {
		_, err := DecodeCursor(tc)
		if err == nil {
			t.Fatalf("[%d] expected error for cursor %q", i, tc)
		}
	}
}

// --- Pi agent turn detection ---

func TestIsPiRenderableUser(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "normal user message",
			line:     `{"type":"message","id":"m1","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
			expected: true,
		},
		{
			name:     "user with image and text",
			line:     `{"type":"message","id":"m2","message":{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","data":"aa","mimeType":"image/png"}]}}`,
			expected: true,
		},
		{
			name:     "assistant message",
			line:     `{"type":"message","id":"m3","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
			expected: false,
		},
		{
			name:     "toolResult message",
			line:     `{"type":"message","id":"m4","message":{"role":"toolResult","toolCallId":"tc1","content":"ok"}}`,
			expected: false,
		},
		{
			name:     "empty text content",
			line:     `{"type":"message","id":"m5","message":{"role":"user","content":[{"type":"text","text":""}]}}`,
			expected: false,
		},
		{
			name:     "whitespace-only text",
			line:     `{"type":"message","id":"m6","message":{"role":"user","content":[{"type":"text","text":"   "}]}}`,
			expected: false,
		},
		{
			name:     "no content array",
			line:     `{"type":"message","id":"m7","message":{"role":"user","content":[]}}`,
			expected: false,
		},
		{
			name:     "session event (not a message)",
			line:     `{"type":"session","version":2,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp"}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPiRenderableUser([]byte(tt.line))
			if got != tt.expected {
				t.Fatalf("isPiRenderableUser() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// --- Claude turn detection ---

func TestIsClaudeRenderableUser(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "text user message",
			line:     `{"type":"user","uuid":"u1","message":{"role":"user","content":"hello world"}}`,
			expected: true,
		},
		{
			name:     "isMeta user message (skip)",
			line:     `{"type":"user","uuid":"u2","isMeta":true,"message":{"role":"user","content":"system thing"}}`,
			expected: false,
		},
		{
			name:     "isSidechain user message (skip)",
			line:     `{"type":"user","uuid":"u3","isSidechain":true,"message":{"role":"user","content":"side convo"}}`,
			expected: false,
		},
		{
			name:     "tool_result user message (array content, skip)",
			line:     `{"type":"user","uuid":"u4","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"result"}]}}`,
			expected: false,
		},
		{
			name:     "empty content",
			line:     `{"type":"user","uuid":"u5","message":{"role":"user","content":""}}`,
			expected: false,
		},
		{
			name:     "assistant type",
			line:     `{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
			expected: false,
		},
		{
			name:     "system type",
			line:     `{"type":"system","uuid":"s1","message":{"role":"user","content":"system prompt"}}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isClaudeRenderableUser([]byte(tt.line))
			if got != tt.expected {
				t.Fatalf("isClaudeRenderableUser() = %v, want %v (line: %s)", got, tt.expected, tt.line)
			}
		})
	}
}

// --- Codex turn detection ---

func TestIsCodexRenderableUser(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "normal user message",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`,
			expected: true,
		},
		{
			name:     "environment context (skip)",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/tmp</cwd>\n</environment_context>"}]}}`,
			expected: false,
		},
		{
			name:     "environment context without closing bracket (skip)",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>"}]}}`,
			expected: false,
		},
		{
			name:     "assistant message",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`,
			expected: false,
		},
		{
			name:     "function_call (not a message)",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"function_call","name":"read","arguments":"{}"}}`,
			expected: false,
		},
		{
			name:     "session_meta (not response_item)",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp","thread_source":"user"}}`,
			expected: false,
		},
		{
			name:     "empty text",
			line:     `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":""}]}}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCodexRenderableUser([]byte(tt.line))
			if got != tt.expected {
				t.Fatalf("isCodexRenderableUser() = %v, want %v (line: %s)", got, tt.expected, tt.line)
			}
		})
	}
}

// --- Scan turns backward ---

func TestScanTurnsBackward_Pi(t *testing.T) {
	// 25 user messages, expect 20 turns, has_more=true
	lines := make([]string, 25)
	for i := 0; i < 25; i++ {
		lines[i] = `{"type":"message","id":"m` + strconv.Itoa(i) + `","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"prompt ` + strconv.Itoa(i) + `"}]}}` + "\n"
	}
	path := writeLines(t, lines...)

	fi, err := os.Stat(path)
	check(t, err)
	fileSize := fi.Size()

	f, err := os.Open(path)
	check(t, err)
	defer f.Close()

	startOffset, turnsFound, err := scanTurnsBackward(f, fileSize, fileSize, 20, "pi")
	check(t, err)
	if turnsFound != 20 {
		t.Fatalf("turnsFound = %d, want 20", turnsFound)
	}
	if startOffset <= 0 {
		t.Fatalf("startOffset = %d, want > 0", startOffset)
	}

	// The first 5 lines should be before startOffset
	f2, err := os.Open(path)
	check(t, err)
	defer f2.Close()

	buf := make([]byte, startOffset)
	n, err := f2.ReadAt(buf, 0)
	check(t, err)
	prefixLines := strings.Count(string(buf[:n]), "\n")
	if prefixLines != 5 {
		t.Fatalf("prefix lines = %d, want 5 (cursor should skip first 5 user messages)", prefixLines)
	}
}

func TestScanTurnsBackward_Pi_ShortSession(t *testing.T) {
	// 3 user messages, expect all 3
	lines := make([]string, 3)
	for i := 0; i < 3; i++ {
		lines[i] = `{"type":"message","id":"m` + strconv.Itoa(i) + `","message":{"role":"user","content":[{"type":"text","text":"p` + strconv.Itoa(i) + `"}]}}` + "\n"
	}
	path := writeLines(t, lines...)
	fi, err := os.Stat(path)
	check(t, err)
	f, err := os.Open(path)
	check(t, err)
	defer f.Close()

	startOffset, turnsFound, err := scanTurnsBackward(f, fi.Size(), fi.Size(), 20, "pi")
	check(t, err)
	if turnsFound != 3 {
		t.Fatalf("turnsFound = %d, want 3", turnsFound)
	}
	if startOffset != 0 {
		t.Fatalf("startOffset = %d, want 0 (all turns included)", startOffset)
	}
}

func TestScanTurnsBackward_EmptyFile(t *testing.T) {
	path := writeLines(t) // no lines
	fi, err := os.Stat(path)
	check(t, err)
	f, err := os.Open(path)
	check(t, err)
	defer f.Close()

	_, turnsFound, err := scanTurnsBackward(f, fi.Size(), fi.Size(), 20, "pi")
	check(t, err)
	if turnsFound != 0 {
		t.Fatalf("turnsFound = %d, want 0", turnsFound)
	}
	if turnsFound != 0 {
		t.Fatalf("turnsFound = %d, want 0", turnsFound)
	}
}

func TestScanTurnsBackward_Claude_SkipsToolResults(t *testing.T) {
	// Claude user with tool_result should not be counted as a renderable turn
	lines := []string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"real prompt"}}` + "\n",
		`{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"text","text":"on it"},{"type":"tool_use","id":"tu1","name":"read","input":{}}]}}` + "\n",
		`{"type":"user","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"file contents"}]}}` + "\n",
		`{"type":"user","uuid":"u3","message":{"role":"user","content":"second prompt"}}` + "\n",
	}
	path := writeLines(t, lines...)
	fi, err := os.Stat(path)
	check(t, err)
	f, err := os.Open(path)
	check(t, err)
	defer f.Close()

	_, turnsFound, err := scanTurnsBackward(f, fi.Size(), fi.Size(), 20, "claude")
	check(t, err)
	if turnsFound != 2 {
		t.Fatalf("turnsFound = %d, want 2 (only real prompts, not tool_results)", turnsFound)
	}
}

func TestScanTurnsBackward_Codex_SkipsEnvironmentContext(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/tmp</cwd>\n</environment_context>"}]}}` + "\n",
		`{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"real prompt"}]}}` + "\n",
	}
	path := writeLines(t, lines...)
	fi, err := os.Stat(path)
	check(t, err)
	f, err := os.Open(path)
	check(t, err)
	defer f.Close()

	_, turnsFound, err := scanTurnsBackward(f, fi.Size(), fi.Size(), 20, "codex")
	check(t, err)
	if turnsFound != 1 {
		t.Fatalf("turnsFound = %d, want 1 (environment context skipped)", turnsFound)
	}
}

func TestScanTurnsBackward_PartialTrailingLine(t *testing.T) {
	// File without trailing newline in the final line should still work
	content := `{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"p1"}]}}` + "\n" +
		`{"type":"message","id":"m2","message":{"role":"user","content":[{"type":"text","text":"p2"}]}}`
	path := filepath.Join(t.TempDir(), "partial.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	check(t, err)
	f, err := os.Open(path)
	check(t, err)
	defer f.Close()

	_, turnsFound, err := scanTurnsBackward(f, fi.Size(), fi.Size(), 20, "pi")
	check(t, err)
	if turnsFound != 2 {
		t.Fatalf("turnsFound = %d, want 2", turnsFound)
	}
}

// --- Page tests ---

func TestLoadLatest_20Turns_Pi(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 25)

	page, err := svc.LoadLatest("test-session", "pi", path, 20)
	check(t, err)
	if !page.HasMore {
		t.Fatal("expected HasMore = true for 25-turn session with limit 20")
	}
	if page.NextCursor == "" {
		t.Fatal("expected non-empty NextCursor")
	}

	var events []json.RawMessage
	if err := json.Unmarshal(page.Events, &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}
	// Should have at least the 20 user messages (plus possibly other events between them)
	if len(events) < 20 {
		t.Fatalf("events count = %d, want >= 20", len(events))
	}

	// Verify cursor can be used for previous page
	page2, err := svc.LoadPrevious("test-session", "pi", path, page.NextCursor, 20)
	check(t, err)
	if page2.HasMore {
		t.Fatal("expected HasMore = false for remaining 5 turns")
	}
	var events2 []json.RawMessage
	json.Unmarshal(page2.Events, &events2)
	if len(events2) < 5 {
		t.Fatalf("second page events = %d, want >= 5", len(events2))
	}
}

func TestLoadLatest_ShortSession(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 3)

	page, err := svc.LoadLatest("s", "pi", path, 20)
	check(t, err)
	if page.HasMore {
		t.Fatal("expected HasMore = false for 3-turn session")
	}
	if page.NextCursor != "" {
		t.Fatal("expected empty NextCursor")
	}
}

func TestLoadLatest_EmptySession(t *testing.T) {
	svc := NewService()
	path := writeLines(t)

	page, err := svc.LoadLatest("s", "pi", path, 20)
	check(t, err)
	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) != 0 {
		t.Fatalf("events count = %d, want 0", len(events))
	}
	if page.HasMore {
		t.Fatal("expected HasMore = false for empty session")
	}
}

func TestLoadLatest_NoRenderableMessages(t *testing.T) {
	svc := NewService()
	// Only session metadata, no user messages
	lines := []string{
		`{"type":"session","version":2,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp"}` + "\n",
		`{"type":"model_change","id":"mc1","timestamp":"2026-01-01T00:00:01Z","provider":"anthropic","modelId":"sonnet"}` + "\n",
	}
	path := writeLines(t, lines...)

	page, err := svc.LoadLatest("s", "pi", path, 20)
	check(t, err)
	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) != 0 {
		t.Fatalf("events count = %d, want 0", len(events))
	}
}

func TestLoadPrevious_WrongSessionID(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 25)

	page, err := svc.LoadLatest("session-A", "pi", path, 20)
	check(t, err)
	if page.NextCursor == "" {
		t.Fatal("expected cursor")
	}

	_, err = svc.LoadPrevious("session-B", "pi", path, page.NextCursor, 5)
	if err == nil {
		t.Fatal("expected error for wrong session ID")
	}
}

func TestLoadPrevious_TruncatedFile(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 10)

	page, err := svc.LoadLatest("s", "pi", path, 5)
	check(t, err)
	if page.NextCursor == "" {
		t.Fatal("expected cursor")
	}

	// Truncate the file
	if err := os.Truncate(path, 100); err != nil {
		t.Fatal(err)
	}

	_, err = svc.LoadPrevious("s", "pi", path, page.NextCursor, 5)
	if err != ErrHistoryChanged {
		t.Fatalf("expected ErrHistoryChanged, got %v", err)
	}
}

func TestLoadPrevious_AppendSafeCursor(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 10)

	page, err := svc.LoadLatest("s", "pi", path, 5)
	check(t, err)
	if page.NextCursor == "" {
		t.Fatal("expected cursor")
	}

	// Append more turns
	moreLines := makeMultiTurnLines("pi", 20, 23) // turns 20-22
	appendLines(t, path, moreLines...)

	// Cursor should still work — bytes before the cursor haven't changed
	page2, err := svc.LoadPrevious("s", "pi", path, page.NextCursor, 5)
	check(t, err)
	if page2.HasMore {
		t.Fatal("expected HasMore=false after loading remaining original turns (old cursor still valid)")
	}
}

func TestLoadLatest_SessionNotFound(t *testing.T) {
	svc := NewService()
	_, err := svc.LoadLatest("s", "pi", "/nonexistent/path.jsonl", 20)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestLoadPrevious_SessionNotFound(t *testing.T) {
	svc := NewService()
	cursor := EncodeCursor("s", 100, 500)
	_, err := svc.LoadPrevious("s", "pi", "/nonexistent/path.jsonl", cursor, 20)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestLoadLatest_LimitCap(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 60)

	// Request 100 but should be capped to 50
	page, err := svc.LoadLatest("s", "pi", path, 100)
	check(t, err)
	if !page.HasMore {
		t.Fatal("expected HasMore = true with 60-turn session capped at 50")
	}

	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) < 50 {
		t.Fatalf("events = %d, want >= 50 (cap respected)", len(events))
	}
}

// --- Claude page tests ---

func TestLoadLatest_Claude(t *testing.T) {
	svc := NewService()
	path := makeClaudeSession(t, 15)

	page, err := svc.LoadLatest("claude-sess", "claude", path, 20)
	check(t, err)
	if page.HasMore {
		t.Fatal("expected HasMore = false for 15 turns")
	}

	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) < 15 {
		t.Fatalf("events = %d, want >= 15", len(events))
	}
}

func TestLoadLatest_Claude_TurnBoundaryDoesNotSplitToolSequence(t *testing.T) {
	svc := NewService()
	// Build a session where the boundary falls near a tool call sequence
	lines := make([]string, 0, 50)
	for i := 0; i < 22; i++ {
		lines = append(lines, claudeUserLine(i)+"\n")
		lines = append(lines, claudeAssistantLine(i)+"\n")
	}
	path := writeLines(t, lines...)

	page, err := svc.LoadLatest("s", "claude", path, 20)
	check(t, err)

	// The returned events should start at a user message, not mid-turn
	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) == 0 {
		t.Fatal("expected events")
	}

	// First event should be a user message
	var first struct {
		Type    string `json:"type"`
		Message *struct {
			Role string `json:"role"`
		} `json:"message"`
	}
	if err := json.Unmarshal(events[0], &first); err != nil {
		t.Fatalf("unmarshal first event: %v", err)
	}
	if first.Message == nil || first.Message.Role != "user" {
		t.Fatalf("first event role = %v, want user", first.Message)
	}
}

// --- Codex page tests ---

func TestLoadLatest_Codex(t *testing.T) {
	svc := NewService()
	path := makeCodexSession(t, 10)

	page, err := svc.LoadLatest("codex-sess", "codex", path, 20)
	check(t, err)
	if page.HasMore {
		t.Fatal("expected HasMore = false for 10 turns")
	}

	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) < 10 {
		t.Fatalf("events = %d, want >= 10", len(events))
	}
}

// --- Cursor validation ---

func TestDecodeCursor_InvalidFormats(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{"empty", ""},
		{"bad base64", "!!!not-base64!!!"},
		{"missing parts without separator", "bm9wZXNlcGFyYXRvcg"},
		{"negative offset", encodeCursorParts("sess", -1, 100)},
		{"negative size", encodeCursorParts("sess", 0, -100)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCursor(tt.cursor)
			if err == nil {
				t.Fatalf("expected error for cursor %q", tt.cursor)
			}
		})
	}
}

func TestLoadPrevious_InvalidCursor(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 5)

	_, err := svc.LoadPrevious("s", "pi", path, "not-valid-base64!!!", 5)
	if err == nil {
		t.Fatal("expected error for invalid cursor")
	}
	// Should wrap with ErrInvalidCursor
	if !errorsIs(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestLoadPrevious_FileSmallerThanCursor(t *testing.T) {
	svc := NewService()
	path := makeMultiTurnSession(t, "pi", 8)

	_, err := svc.LoadLatest("s", "pi", path, 3)
	check(t, err)

	// Create cursor referencing a much larger file size
	fi, _ := os.Stat(path)
	largeCursor := EncodeCursor("s", 100, fi.Size()+10000)

	_, err = svc.LoadPrevious("s", "pi", path, largeCursor, 5)
	if err != ErrHistoryChanged {
		t.Fatalf("expected ErrHistoryChanged, got %v", err)
	}
}

// --- Helpers ---

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join(lines, "")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp session: %v", err)
	}
	return path
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func makeMultiTurnSession(t *testing.T, agent string, turns int) string {
	t.Helper()
	lines := makeMultiTurnLines(agent, 1, turns)
	return writeLines(t, lines...)
}

func makeMultiTurnLines(agent string, start, end int) []string {
	var lines []string
	for i := start; i <= end; i++ {
		switch agent {
		case "pi":
			lines = append(lines, piUserLine(i)+"\n")
		case "claude":
			lines = append(lines, claudeUserLine(i)+"\n")
			lines = append(lines, claudeAssistantLine(i)+"\n")
		case "codex":
			lines = append(lines, codexUserLine(i)+"\n")
		}
	}
	return lines
}

func piUserLine(n int) string {
	return `{"type":"message","id":"m` + strconv.Itoa(n) + `","timestamp":"2026-01-01T00:00:` +
		pad2(n) + `Z","message":{"role":"user","content":[{"type":"text","text":"prompt ` + strconv.Itoa(n) + `"}]}}`
}

func claudeUserLine(n int) string {
	return `{"type":"user","uuid":"u` + strconv.Itoa(n) + `","message":{"role":"user","content":"claude prompt ` + strconv.Itoa(n) + `"}}`
}

func claudeAssistantLine(n int) string {
	return `{"type":"assistant","uuid":"a` + strconv.Itoa(n) + `","message":{"role":"assistant","content":[{"type":"text","text":"claude response ` + strconv.Itoa(n) + `"}]}}`
}

func codexUserLine(n int) string {
	return `{"timestamp":"2026-01-01T00:00:` + pad2(n) + `Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"codex prompt ` + strconv.Itoa(n) + `"}]}}`
}

func makeClaudeSession(t *testing.T, turns int) string {
	t.Helper()
	var lines []string
	for i := 1; i <= turns; i++ {
		lines = append(lines,
			`{"type":"user","uuid":"u`+strconv.Itoa(i)+`","message":{"role":"user","content":"prompt `+strconv.Itoa(i)+`"}}`+"\n",
			`{"type":"assistant","uuid":"a`+strconv.Itoa(i)+`","message":{"role":"assistant","content":[{"type":"text","text":"response `+strconv.Itoa(i)+`"}]}}`+"\n",
		)
	}
	return writeLines(t, lines...)
}

func makeCodexSession(t *testing.T, turns int) string {
	t.Helper()
	var lines []string
	for i := 1; i <= turns; i++ {
		lines = append(lines,
			`{"timestamp":"2026-01-01T00:00:`+pad2(i)+`Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"codex prompt `+strconv.Itoa(i)+`"}]}}`+"\n",
		)
	}
	return writeLines(t, lines...)
}

// EncodeCursorRaw creates an arbitrary cursor for testing.
func EncodeCursorRaw(sessionID string, byteOffset int64) string {
	return EncodeCursor(sessionID, byteOffset, 0)
}

func encodeCursorParts(sessionID string, byteOffset, fileSize int64) string {
	return EncodeCursor(sessionID, byteOffset, fileSize)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func errorsIs(err, target error) bool {
	for {
		if err == target {
			return true
		}
		// Simple unwrap via string matching for standard errors.Is behavior
		if err.Error() == target.Error() {
			return true
		}
		// Check if any error in the chain matches by prefix
		if strings.Contains(err.Error(), target.Error()) {
			return true
		}
		return false
	}
}
