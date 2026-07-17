# Tauri + Svelte macOS App — Design Spec

**Date:** 2026-07-08
**Status:** Draft
**Owner:** antoni.phan@vsee.io

## 1. Goal

Ship agent-reader as a native **macOS desktop app** built with **Tauri v2**, reusing
the existing **Svelte 5 + Tailwind 4** frontend and the existing **Go API** (session
watcher, WebSocket stream, Pi/Claude/Codex RPC chat, tmux attach, local-LLM translate).

The app must:

- Launch as a normal `.app` (double-click, Dock icon, no terminal, no `make run`).
- Bundle and manage the Go server automatically as a **sidecar** — the user never
  starts a server manually.
- Preserve today's full feature set: session list, real-time streaming, RPC chat,
  model switching, tmux terminal, filesystem browse, image upload, translation.
- Feel native: menu bar, window state, notifications for unread/finished sessions,
  deep OS integration where cheap.

Non-goals (v1): Windows/Linux builds, App Store distribution, multi-window, auto-update
server protocol changes. These are called out in §11 as future work.

## 2. Background — what exists today

- **Backend:** Go HTTP server (`cmd/server/main.go` → `internal/server/server.go`).
  Default listen `:8081`. Flags: `-addr`, `-sessions`, `-claude-projects`,
  `-codex-sessions`, `-roots`. Reads `.env` from CWD (`ALLOWED_ROOT_FOLDERS`,
  `LMSTUDIO_URL`, `LMSTUDIO_MODEL`).
- **Frontend:** `frontend/` — Svelte 5 (runes), Tailwind 4, Vite 6. Builds to
  `internal/server/static/dist`, served as SPA by Go. Uses **relative URLs**
  (`/api/...`) and `` `${proto}//${location.host}/ws` `` for the WebSocket.
- **API surface** (all under the Go server):

  | Route | Method | Purpose |
  |-------|--------|---------|
  | `/ws` | WS | Event stream (`{type:"event",session_id,data}`) + subscribe/unsubscribe/ping |
  | `/api/sessions` | GET | List sessions (`?page&sort&group_by`) |
  | `/api/sessions/create` | POST | New session (`{cwd}`) |
  | `/api/sessions/unread` | GET | Unread session IDs |
  | `/api/sessions/{id}` | GET | Session detail |
  | `/api/sessions/{id}/mark-read` | POST | Mark read (`{line_count}`) |
  | `/api/rpc/start\|stop\|send` | POST | RPC subprocess lifecycle + prompt |
  | `/api/rpc/get_state\|get_commands\|get_models\|set_model\|cycle_model\|status` | POST/GET | RPC controls |
  | `/api/images/upload\|view` | POST/GET | Image paste/view (base64url path) |
  | `/api/fs/browse\|search\|read` | GET | Filesystem (gated by allowed roots) |
  | `/api/translate` | POST | LM Studio translation |
  | `/api/tmux/sessions[/{name}]`, `/ws/tmux/{name}` | GET/WS | tmux list + attach |

- **External runtime deps the server shells out to:** `pi` (RPC agent), `tmux`
  (terminal attach), LM Studio (optional, translate only). These live on the user's
  machine, **not** bundled.

The frontend is essentially already a SPA. The bulk of this work is (a) packaging and
(b) fixing the handful of places that assume a same-origin server.

## 3. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  agent-reader.app  (Tauri v2, macOS)                          │
│                                                               │
│  ┌───────────────────────────┐   spawn (sidecar)             │
│  │  WKWebView (Svelte SPA)   │ ───────────────┐              │
│  │  tauri://localhost        │                │              │
│  │                           │                ▼              │
│  │  fetch(API_BASE + /api)   │       ┌──────────────────┐    │
│  │  new WebSocket(WS_BASE)   │──────▶│  Go server       │    │
│  └───────────────────────────┘  HTTP │  127.0.0.1:PORT  │    │
│         ▲  invoke('get_config')  /WS  │  (bundled binary)│    │
│         │                             └────────┬─────────┘    │
│  ┌──────┴───────────────────┐                  │ os/exec      │
│  │  Rust core (src-tauri)   │                  ▼              │
│  │  - spawn/kill sidecar    │        pi / tmux / LM Studio    │
│  │  - pick free port        │        (user's machine)         │
│  │  - expose config to JS   │                                 │
│  │  - menu, notifications   │                                 │
│  └──────────────────────────┘                                 │
└───────────────────────────────────────────────────────────────┘
```

**Key decisions:**

1. **Go server as a Tauri sidecar**, not rewritten in Rust. The Go code is the product;
   Rust is a thin host. Ship the compiled Go binary as an `externalBin`, spawn it on
   app start, kill it on exit.
2. **Loopback HTTP/WS, not stdio.** Keep the existing HTTP+WS server as-is. The WebView
   talks to `http://127.0.0.1:<port>`. This means near-zero backend changes.
3. **Dynamic port + handshake.** Rust picks a free port, passes it to the Go binary via
   `-addr 127.0.0.1:<port>`, waits for health, then exposes `{apiBase, wsBase}` to the
   frontend via a Tauri command. Avoids collisions with a user already running `:8081`.
4. **Frontend stays Svelte SPA**, reused nearly verbatim. One new module centralizes the
   base URL so `fetch`/`WebSocket` no longer assume same-origin.

## 4. Repository layout

Add a Tauri app alongside the existing tree (no disruption to `make run` web mode):

```
agent-reader/
├── cmd/server/main.go          # + new flags (see §5.1)
├── internal/…                  # unchanged
├── frontend/                   # Svelte SPA — small edits (§6)
│   └── src/lib/config.js       # NEW: resolves API_BASE/WS_BASE
├── src-tauri/                  # NEW: Tauri v2 Rust host
│   ├── Cargo.toml
│   ├── tauri.conf.json
│   ├── build.rs
│   ├── icons/                  # .icns + png set
│   ├── binaries/               # sidecar: server-<target-triple>
│   └── src/
│       ├── main.rs
│       ├── sidecar.rs          # spawn/health/kill + port pick
│       └── config.rs           # get_config command
└── Makefile                    # + tauri targets (§9)
```

The web build (`internal/server/static/dist`) and desktop build are both driven from the
same `frontend/` source; only the config-resolution module differs at runtime.

## 5. Backend changes (Go) — minimal

### 5.1 Flags / bootstrapping
- Accept `-addr 127.0.0.1:0` and, when port is `0`, **print the chosen port** on a
  known line (`[server] listening on 127.0.0.1:54xxx`) so Rust can parse it — OR simpler:
  Rust picks the port and passes an explicit one. **Chosen: Rust picks the port.**
- Add a `-config <path>` (or rely on env) so the app can point at a config dir instead of
  CWD `.env`. In the bundled app, CWD is unreliable; pass all config via **flags/env from
  Rust** rather than reading `.env` next to the binary. Keep `.env` support for web mode.
- Add `GET /healthz` → `200 {"ok":true}` for the sidecar readiness probe. (Cheap; add if
  not already present.)

### 5.2 CORS / origin
- The WebView origin is `tauri://localhost` (or `http://tauri.localhost`). The Go server
  currently assumes same-origin. Add permissive CORS **for loopback only** and relax the
  WebSocket origin check (gorilla `CheckOrigin`) to allow the Tauri origin. Gate this
  behind a `-desktop` flag so web mode keeps its stricter default.

### 5.3 Config passed by Rust at spawn
Rust supplies, as flags/env: `-addr`, `-sessions`, `-claude-projects`, `-codex-sessions`,
`-roots`, `LMSTUDIO_URL`, `LMSTUDIO_MODEL`. Defaults (home-relative dirs) already resolve
inside `main.go`; the app only overrides when the user customizes them in Settings (§8).

**That is the full backend delta — no changes to watchers, RPC, hub, tmux, decoders.**

## 6. Frontend changes (Svelte) — small, surgical

### 6.1 New `src/lib/config.js`
Single source of truth for base URLs. In Tauri, call the Rust `get_config` command once at
startup; in web mode, fall back to relative/`location`-based URLs.

```js
// Resolves { apiBase, wsBase }. apiBase '' means same-origin (web mode).
let cfg = { apiBase: '', wsBase: '' };
export async function initConfig() {
  if (window.__TAURI__) {
    const { apiBase, wsBase } = await window.__TAURI__.core.invoke('get_config');
    cfg = { apiBase, wsBase };
  } else {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    cfg = { apiBase: '', wsBase: `${proto}//${location.host}` };
  }
}
export const apiUrl = (p) => cfg.apiBase + p;
export const wsUrl = (p) => cfg.wsBase + p;
```

### 6.2 Route all `fetch` and `WebSocket` through it
- `api/sessions.js`, `api/rpc.js`, `api/tmux.js`, `api/fs.js`, `imageViewUrl` →
  wrap paths in `apiUrl(...)`.
- `api/websocket.js` and `api/tmux.js` WS → use `wsUrl('/ws')` / `wsUrl('/ws/tmux/...')`
  instead of `` `${proto}//${location.host}/ws` ``.
- `main.js` → `await initConfig()` before mounting `<App/>` (or gate mount on a resolved
  store so components don't fetch pre-config).

This is a find-and-replace across ~6 files; behavior in web mode is unchanged because
`apiBase` is `''`.

### 6.3 Optional Tauri niceties (progressive)
- Use `@tauri-apps/plugin-notification` for "session finished / unread" toasts instead of
  in-page toasts when backgrounded.
- Use `@tauri-apps/plugin-dialog` for the native folder picker in `PathPicker`/
  `NewSessionModal` (replaces `/api/fs/browse` UI, optional).
- Persist window size/position with `tauri-plugin-window-state`.

## 7. Tauri host (Rust, `src-tauri`)

### 7.1 Sidecar lifecycle (`sidecar.rs`)
1. On `setup`, pick a free loopback port (bind `127.0.0.1:0`, read assigned port, release).
2. Resolve config (see §8) from a JSON settings file in
   `~/Library/Application Support/agent-reader/settings.json`.
3. Spawn the sidecar via Tauri shell `Command::new_sidecar("server")` with args
   `-desktop -addr 127.0.0.1:<port>` + configured dir flags + env.
4. Poll `GET /healthz` (200ms interval, ~10s timeout) until ready; store
   `{apiBase, wsBase}` in Tauri managed state.
5. Register an exit/`on_window_event(Destroyed)` + `RunEvent::Exit` handler that **kills
   the child** (and its process group, so `pi`/`tmux` children spawned by the server are
   reaped). Also handle panic/`Drop`.
6. If the sidecar dies unexpectedly, show a native dialog and offer restart.

### 7.2 Commands (`config.rs`)
- `get_config() -> {apiBase, wsBase}` — returns the resolved loopback URLs.
- `get_settings()` / `set_settings(...)` — read/write the settings JSON; on change that
  affects the server (dirs/roots/LM Studio), restart the sidecar.

### 7.3 `tauri.conf.json` essentials
- `bundle.externalBin: ["binaries/server"]` (Tauri appends the target triple).
- `bundle.targets: ["dmg", "app"]`, `bundle.macOS.minimumSystemVersion`, category
  `public.app-category.developer-tools`.
- `app.security.csp` — allow `connect-src` to `http://127.0.0.1:* ws://127.0.0.1:*`
  (and `ipc:`/`tauri:` per Tauri v2 defaults). Keep `default-src 'self'`.
- Plugins: `shell` (sidecar), `notification`, `dialog`, `window-state`, `fs` (scoped, if
  native pickers are used).
- Capabilities file granting the WebView `shell:allow-execute` for the sidecar only,
  `core:default`, and the plugin permissions above.

### 7.4 Signing & notarization (macOS)
- Developer ID Application cert; hardened runtime enabled.
- Entitlements: `com.apple.security.cs.allow-jit` /
  `allow-unsigned-executable-memory` are **not** needed for Go, but
  `com.apple.security.cs.disable-library-validation` may be required so the signed Go
  sidecar loads; verify. The sidecar binary must itself be signed (Tauri signs bundled
  externalBins during `tauri build` when `signingIdentity` is set).
- Notarize the `.dmg` via `notarytool` (Tauri does this when
  `APPLE_ID`/`APPLE_PASSWORD`/`APPLE_TEAM_ID` env are set), then staple.
- **No sandbox** (`com.apple.security.app-sandbox` = false): the app spawns `pi`/`tmux`
  and reads `~/.pi`, `~/.claude`, `~/.codex`, arbitrary project roots. App Store is out of
  scope for v1 precisely because of this.

## 8. Configuration & settings

Settings file: `~/Library/Application Support/agent-reader/settings.json`.

| Key | Default | Feeds |
|-----|---------|-------|
| `sessionsDir` | `~/.pi/agent/sessions` | `-sessions` |
| `claudeProjectsDir` | `~/.claude/projects` | `-claude-projects` |
| `codexSessionsDir` | `~/.codex/sessions` | `-codex-sessions` |
| `allowedRoots` | `[]` | `-roots` (fs API disabled if empty) |
| `lmStudioUrl` / `lmStudioModel` | unset | translate env |

A minimal **Settings** view (Svelte modal or native window) edits these via
`get_settings`/`set_settings`; saving restarts the sidecar. First-run: if `sessionsDir`
doesn't exist, show a friendly setup screen rather than the current `log.Fatalf`.

## 9. Build & dev workflow

**Prereqs:** Rust toolchain, Tauri CLI v2, Node, Go, Xcode CLT.

Add to `Makefile`:

```
tauri-dev:                 # hot-reload Svelte + spawn debug sidecar
	cd frontend && npm run build     # or vite dev via tauri devUrl
	cargo tauri dev

sidecar-build:             # compile Go for the host triple into src-tauri/binaries
	GOOS=darwin GOARCH=$(ARCH) go build -o src-tauri/binaries/server-$(TRIPLE) ./cmd/server

tauri-build: sidecar-build frontend-build
	cargo tauri build        # produces .app + .dmg (signed/notarized if env set)
```

- `beforeBuildCommand` in `tauri.conf.json` runs `frontend-build`; document that
  `sidecar-build` must run first (or wire it into `beforeBuildCommand`).
- Target both `aarch64-apple-darwin` and `x86_64-apple-darwin`; ship a universal binary
  or two DMGs. Recommend **universal** (`lipo` the Go binary, `--target universal-apple-darwin`).

## 10. Testing & verification

- **Backend unchanged** → existing `go test ./...` must stay green; add a test for the
  `-desktop` CORS/origin behavior and `/healthz`.
- **Frontend** → existing vitest (`*.test.mjs`) must pass; add a test that `config.js`
  returns relative URLs when `window.__TAURI__` is absent.
- **Integration smoke (manual, then scripted):**
  1. `cargo tauri dev` → app window opens, session list loads (proves sidecar + config
     handshake + WS).
  2. Start RPC on a session, send a prompt, see streaming tokens.
  3. Open tmux terminal modal, confirm attach WS works.
  4. Paste an image, translate a message.
  5. Quit app → confirm no orphan `server`/`pi`/`tmux` processes (`pgrep`).
- **Packaging:** `cargo tauri build` → install the `.dmg` on a clean machine (or a second
  user account), verify Gatekeeper accepts the notarized app and it runs without terminal.

## 11. Risks & future work

- **Orphaned child processes.** The Go server spawns `pi`/`tmux`. Killing only the sidecar
  may leak grandchildren. Mitigation: spawn the sidecar in its own process group and kill
  the group on exit; have the Go server reap its children on SIGTERM (verify `srv.Stop()`
  already does — it handles SIGINT/SIGTERM today).
- **Port/collision & multiple app instances.** Dynamic port solves collisions; a
  single-instance guard (`tauri-plugin-single-instance`) prevents two sidecars.
- **Missing external deps.** If `pi`/`tmux` aren't installed, RPC/tmux features fail. Show
  a clear in-app diagnostic (probe `which pi` via a command) rather than opaque errors.
- **Notarization friction** with a bundled Go binary — budget time to get signing right;
  it's the most likely blocker.
- **Future:** auto-update (Tauri updater), Windows/Linux, native menu-driven session
  actions, migrate translate to a bundled model, tray/menu-bar mode.

## 12. Phased implementation plan

1. **Backend prep** — `-desktop` flag (CORS + WS origin), `/healthz`, config-via-flags.
   Keep web mode identical. Tests green.
2. **Frontend config layer** — `config.js` + route all fetch/WS through it. Verify web
   mode unaffected (`make run`).
3. **Tauri skeleton** — `src-tauri` init, sidecar spawn with a *hardcoded* port + health
   probe, `get_config`. Get the session list rendering in the WebView.
4. **Dynamic port + lifecycle** — free-port pick, robust kill on exit, crash dialog,
   single-instance.
5. **Settings + first-run** — settings JSON, Settings UI, sidecar restart on change,
   friendly missing-dir setup.
6. **Native polish** — notifications, native folder picker, window-state, app menu, icons.
7. **Signing, notarization, universal build, DMG** — CI/local recipe, clean-machine test.

Each phase is independently verifiable and leaves both web and desktop modes working.
