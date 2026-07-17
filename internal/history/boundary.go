package history

import (
	"encoding/json"
	"strings"
)

// isRenderableUserMessage classifies a raw JSONL line as a renderable user message
// for the given agent. A renderable user message is one that, after normalization,
// produces a user-role message with visible text content.
func isRenderableUserMessage(line []byte, agent string) bool {
	switch agent {
	case "claude":
		return isClaudeRenderableUser(line)
	case "codex":
		return isCodexRenderableUser(line)
	default:
		return isPiRenderableUser(line)
	}
}

// isPiRenderableUser detects a pi-agent user message line.
// Format: {"type":"message","message":{"role":"user","content":[...]}}
func isPiRenderableUser(line []byte) bool {
	var ev struct {
		Type    string `json:"type"`
		Message *struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return false
	}
	if ev.Type != "message" || ev.Message == nil {
		return false
	}
	if ev.Message.Role != "user" {
		return false
	}
	// A renderable user message has at least one text block with content
	content := ev.Message.Content
	if len(content) == 0 {
		return false
	}
	for _, block := range content {
		var b struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(block, &b); err != nil {
			continue
		}
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

// isClaudeRenderableUser detects a Claude Code user message that produces a
// renderable user-text event (not tool_result blocks).
// Claude user events with string content are text prompts.
// Claude user events with array content contain tool_result blocks and are NOT renderable.
func isClaudeRenderableUser(line []byte) bool {
	// Quick check: must start with "user" type
	var ev struct {
		Type    string `json:"type"`
		IsMeta  bool   `json:"isMeta"`
		Message *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return false
	}
	if ev.Type != "user" {
		return false
	}

	// isMeta marks system-injected messages that aren't user-authored
	if ev.IsMeta {
		return false
	}
	// isSidechain marks messages in side conversations
	var isSidechain bool
	_ = json.Unmarshal(line, &struct {
		IsSidechain *bool `json:"isSidechain"`
	}{IsSidechain: &isSidechain})
	if isSidechain {
		return false
	}

	if ev.Message == nil {
		return false
	}

	// Content is either a JSON string (text prompt) or a JSON array (tool_result blocks).
	// A renderable user message has non-empty string content.
	raw := strings.TrimSpace(string(ev.Message.Content))
	if len(raw) < 2 || raw[0] != '"' {
		return false
	}
	// Must be a JSON string with non-empty content (at least one character between quotes)
	return len(raw) > 2
}

// isCodexRenderableUser detects a Codex user message that produces a renderable event.
// Codex format: {"type":"response_item","payload":{"type":"message","role":"user","content":[...]}}
// Environment context messages are excluded.
func isCodexRenderableUser(line []byte) bool {
	var env struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return false
	}
	if env.Type != "response_item" {
		return false
	}

	var payload struct {
		Type    string            `json:"type"`
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return false
	}
	if payload.Type != "message" || payload.Role != "user" {
		return false
	}

	if len(payload.Content) == 0 {
		return false
	}

	// Extract text from content blocks, skipping environment_context
	var textParts []string
	for _, block := range payload.Content {
		var b struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(block, &b); err != nil {
			continue
		}
		if b.Type == "input_text" && strings.TrimSpace(b.Text) != "" {
			textParts = append(textParts, b.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(textParts, " "))
	if text == "" {
		return false
	}
	// Exclude pure environment-context messages
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "<environment_context>") ||
		strings.HasPrefix(lower, "<environment_context") {
		return false
	}
	return true
}
