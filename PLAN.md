# JSONL Viewer

JSONL Viewer is a read-only browser/desktop viewer for Pi, Claude Code, and Codex session logs.

## Architecture

- Go backend watches the agent session directories and serves session metadata.
- HTTP cursor pagination loads history by conversation turn:
  `GET /api/sessions/{id}/history?limit=20&cursor=...`
- WebSocket is reserved for live events from watched session files; it does not replay history.
- Svelte frontend displays the session list, paginated history, tool calls/results, thinking blocks, and referenced images.
- The server never starts or controls an agent process, terminal, tmux session, or new session.

## Read-only API surface

- `GET /api/sessions`
- `GET /api/sessions/{id}`
- `GET /api/sessions/{id}/history`
- `GET /api/sessions/unread`
- `POST /api/sessions/{id}/mark-read` (local read-tracking bookkeeping)
- `GET /api/images/view`
- `POST /api/translate` (optional display-only translation)
- `/ws` for live watched-file events

There are no RPC, tmux, session-creation, image-upload, filesystem-browsing, or command APIs.
