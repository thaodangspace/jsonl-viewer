# agent-reader — Architecture

## Overview
agent-reader is a desktop + browser app for reading and interacting with AI coding agent session logs (Pi, Claude Code, Codex). It consists of a Go backend server and a Svelte 5 frontend.

## Key Architecture Patterns

### Session History (Lazy Loading)
History is delivered via HTTP cursor pagination, not WebSocket replay.

- **HTTP endpoint**: `GET /api/sessions/{id}/history?limit=20&cursor=...`
  - Returns `{ events, next_cursor, has_more, snapshot }`
  - Cursor is `base64({sessionID}|{byteOffset}|{fileSize})`
  - Errors: 404 (not found), 400 (invalid cursor), 409 (history changed/truncated)

- **History service**: `internal/history/`
  - `LoadLatest(sessionID, agent, filePath, limit)` — scans backward from EOF, counts conversation turns, decodes forward through agent decoder
  - `LoadPrevious(...)` — same but starts from cursor's byte offset
  - Turn boundaries: a "renderable user message" (not tool results, not meta, not env context)
  - Backward scanner uses 64KB chunks, agent-specific classifiers (`isPiRenderableUser`, etc.)
  - Forward decode reuses `internal/jsonl/*Decoder` instances

- **Pagination unit**: Conversation turns (user message + subsequent assistant/tool activity), not raw JSONL lines

### WebSocket
WebSocket is reserved for **live events only** (streaming deltas, new messages, status changes).

- **Hub** (`internal/hub/`): subscribe replaces previous subscription (one session per client)
- **No history replay**: `onSubscribe` callback no longer invoked by hub
- **Watcher** (`internal/watcher/`): on first open of an existing file, seeks to EOF to avoid re-emitting pre-existing content as live events

### Live Event Handoff
When a session is selected:
1. Reset history state and clear messages
2. Subscribe to WebSocket (live events start flowing)
3. Fetch latest history page via HTTP
4. During fetch, live events are buffered in `events.js`
5. After fetch: transform, dedup, set messages, drain buffer, merge buffered events
6. After initial load: live events flow directly through `onWSMessage` with dedup via `makeEventKey`

### Deduplication
- `seenEvents` Set in `events.js` tracks `{sessionID}:{eventID}` keys
- History module uses `_seenKeys` Set with same key format
- Transform: `frontend/src/lib/history/transform.js` — batch event→message view model
- Dedup: `frontend/src/lib/history/dedup.js` — `makeEventKey(sessionID, event)`, `dedupBatch`

### Scroll Anchoring
- Infinite scroll upward: scroll within 200px of top triggers `loadOlderPage`
- Anchor capture: record `scrollHeight` before prepend, adjust `scrollTop` after DOM update
- Components: `LoadingHistory`, `EndOfHistory`, `HistoryError`

### Frontend State
- `frontend/src/lib/history/state.svelte.js` — Svelte rune module managing:
  - `loadGeneration` (staleness guard)
  - `nextCursor`, `hasMore` (pagination)
  - `initialLoading`, `olderLoading` (UI flags)
  - `historyError`, `_seenKeys`
- `frontend/src/lib/api/sessions.js` — `fetchSessionHistory(id, {limit, cursor, signal})`

## File Layout
```
internal/
  history/          — Cursor-paginated session history
  hub/              — WebSocket client management
  watcher/          — Filesystem watchers (pi, claude, codex)
  jsonl/            — Agent-specific JSONL decoders
  server/           — HTTP server, API handlers
  rpc/              — RPC session management
  readtracker/      — Session read tracking (SQLite)
  tmux/             — tmux session attachment
  llm/              — Local LLM client (translation)
  fsbrowse/         — Filesystem browsing service
frontend/src/lib/
  history/          — Lazy-load history state, transform, dedup
  stores/           — Svelte writable stores
  actions/          — Session selection, RPC actions
  components/       — UI components
  api/              — API client functions
  utils/            — Scroll, language detection, events
```

## Agent Formats
- **Pi**: JSONL with `type: "message"`, nested `message.role`, content blocks
- **Claude Code**: JSONL with `type: "user"/"assistant"`, tool_use/tool_result in content blocks, `isMeta`/`isSidechain` flags
- **Codex**: JSONL with `type: "response_item"`, `session_meta`, `turn_context`, environment context messages excluded

## Frontend UI Architecture

- **Component library**: [Bits UI](https://bits-ui.com) (Svelte 5 headless primitives) — Dialogs, DropdownMenus, Collapsibles, Command palettes, Tooltips, and Buttons use Bits UI for accessible, controlled behavior (focus trapping, Escape/outside-click dismissal, ARIA).
- **Icons**: [unplugin-icons](https://github.com/unplugin/unplugin-icons) with `@iconify-json/lucide` — every Lucide icon is a compile-time `~icons/lucide/<name>` import; `@lucide/svelte` is removed.
- **UI wrappers**: `frontend/src/lib/components/ui/` contains thin project-owned wrappers (`Button.svelte`, `Tooltip.svelte`, `DialogSurface.svelte`, `Icon.svelte`) that expose full Bits UI behavior while centralizing shared theme classes. Do not build broad wrapper frameworks — keep wrappers thin.
- **Theme**: Catppuccin-like `ctp-*` design tokens in `app.css` `@theme` block. All Bits UI surface styling (overlay, content, item, trigger) is applied via `app.css` attribute selectors (`[data-bits-*]`). Do not inline-override Bits UI data attributes in individual components.
- **Manual interaction verification**: Each dialog, dropdown, command palette, collapsible, and tooltip was verified manually against the Phase 2–5 check matrices. New interaction surfaces must use Bits UI primitives and must not reintroduce custom backdrops, manual Escape plumbing, body scroll locks, or coordinate-calculated positioning.

## Testing
- Go: `GOCACHE=/tmp/go-cache go test ./...` (requires Go 1.25+)
- Frontend: `cd frontend && npm run build`
- Manual: `make run` for browser, `make run-desktop` for Tauri
