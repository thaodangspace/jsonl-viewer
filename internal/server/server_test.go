package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"agent-reader/internal/history"
)

func TestSPAHandlerServesClientRoutesAndStaticAssets(t *testing.T) {
	fileSystem := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>app shell</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('asset')")},
	}
	h := spaHandler(fileSystem)

	routeReq := httptest.NewRequest(http.MethodGet, "/sessions/example", nil)
	routeRec := httptest.NewRecorder()
	h.ServeHTTP(routeRec, routeReq)
	if routeRec.Code != http.StatusOK {
		t.Fatalf("session route status = %d, want %d", routeRec.Code, http.StatusOK)
	}
	if got := routeRec.Body.String(); got != "<html>app shell</html>" {
		t.Fatalf("session route body = %q, want SPA shell", got)
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	assetRec := httptest.NewRecorder()
	h.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", assetRec.Code, http.StatusOK)
	}
	if got := assetRec.Body.String(); got != "console.log('asset')" {
		t.Fatalf("asset body = %q, want asset contents", got)
	}
}

func TestDesktopCORSAllowsTauriOriginForAPI(t *testing.T) {
	s := &Server{sessionsDir: t.TempDir(), desktop: true}
	h := s.handler()

	preflight := httptest.NewRequest(http.MethodOptions, "/api/sessions", nil)
	preflight.Header.Set("Origin", desktopOriginTauri)
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	preflight.Header.Set("Access-Control-Request-Headers", "Content-Type")
	preflightRec := httptest.NewRecorder()
	h.ServeHTTP(preflightRec, preflight)
	if preflightRec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", preflightRec.Code, http.StatusNoContent)
	}
	if got := preflightRec.Header().Get("Access-Control-Allow-Origin"); got != desktopOriginTauri {
		t.Fatalf("preflight allow-origin = %q, want %q", got, desktopOriginTauri)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	get.Header.Set("Origin", desktopOriginTauri)
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d; body=%s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	if got := getRec.Header().Get("Access-Control-Allow-Origin"); got != desktopOriginTauri {
		t.Fatalf("GET allow-origin = %q, want %q", got, desktopOriginTauri)
	}
}

func TestDesktopCORSDisabledByDefault(t *testing.T) {
	s := &Server{sessionsDir: t.TempDir()}
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Origin", desktopOriginTauri)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow-origin = %q, want empty", got)
	}
}

func TestCheckWSOriginDesktopOriginsOnlyWhenDesktop(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Host = "localhost:8081"
	req.Header.Set("Origin", desktopOriginTauri)
	if (&Server{}).checkWSOrigin(req) {
		t.Fatal("non-desktop server accepted Tauri origin")
	}
	if !(&Server{desktop: true}).checkWSOrigin(req) {
		t.Fatal("desktop server rejected Tauri origin")
	}

	sameOrigin := httptest.NewRequest(http.MethodGet, "/ws", nil)
	sameOrigin.Host = "localhost:8081"
	sameOrigin.Header.Set("Origin", "http://localhost:8081")
	if !(&Server{}).checkWSOrigin(sameOrigin) {
		t.Fatal("same-origin websocket was rejected")
	}

	devOrigin := httptest.NewRequest(http.MethodGet, "/ws", nil)
	devOrigin.Host = "localhost:8081"
	devOrigin.Header.Set("Origin", "http://127.0.0.1:1420")
	if !(&Server{desktop: true}).checkWSOrigin(devOrigin) {
		t.Fatal("desktop server rejected Tauri dev loopback origin")
	}
	if (&Server{}).checkWSOrigin(devOrigin) {
		t.Fatal("non-desktop server accepted Tauri dev loopback origin")
	}
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	(&Server{}).handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
}

func TestHealthzDesktopCORS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://127.0.0.1:1420")
	rec := httptest.NewRecorder()
	(&Server{desktop: true}).handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:1420" {
		t.Fatalf("allow-origin = %q, want %q", got, "http://127.0.0.1:1420")
	}
}

func TestGetFirstUserMessageCodexSkipsEnvironmentContext(t *testing.T) {
	path := writeTempSession(t,
		`{"timestamp":"2026-05-19T02:39:55.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/tmp/project</cwd>\n</environment_context>"}]}}`,
		`{"timestamp":"2026-05-19T02:39:56.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"real user prompt"}]}}`,
	)

	got := getFirstUserMessage(path, "codex")
	if got != "real user prompt" {
		t.Fatalf("expected real user prompt, got %q", got)
	}
}

func TestReadCodexSessionInfoToleratesLongLineBeforeMeta(t *testing.T) {
	path := writeTempSession(t,
		longCodexUserMessage("ignored", 40*1024),
		`{"timestamp":"2026-05-19T02:39:55.659Z","type":"session_meta","payload":{"id":"codex-session-1","cwd":"/tmp/codex-project","thread_source":"user","model":"gpt-5"}}`,
	)

	meta, ok := readCodexSessionInfo(path)
	if !ok {
		t.Fatal("expected session metadata")
	}
	if meta.ID != "codex-session-1" {
		t.Fatalf("unexpected session id: %q", meta.ID)
	}
}

func TestAggregateCodexSessionDataToleratesLongLineBeforeModel(t *testing.T) {
	path := writeTempSession(t,
		`{"timestamp":"2026-05-19T02:39:55.659Z","type":"session_meta","payload":{"id":"codex-session-2","cwd":"/tmp/codex-project","thread_source":"user"}}`,
		longCodexUserMessage("ignored", 40*1024),
		`{"timestamp":"2026-05-19T02:39:56.659Z","type":"turn_context","payload":{"model":"gpt-5"}}`,
	)

	lineCount, cwd, model, _, _, _, _, _ := aggregateSessionData(path, "codex")
	if lineCount != 3 {
		t.Fatalf("expected 3 lines counted, got %d", lineCount)
	}
	if cwd != "/tmp/codex-project" {
		t.Fatalf("unexpected cwd: %q", cwd)
	}
	if model != "gpt-5" {
		t.Fatalf("unexpected model: %q", model)
	}
}

func TestListSessionsCodexDerivesProjectFromCWD(t *testing.T) {
	root := t.TempDir()
	piDir := filepath.Join(root, "pi")
	codexDir := filepath.Join(root, "codex")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		t.Fatalf("create pi dir: %v", err)
	}
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("create codex dir: %v", err)
	}
	path := filepath.Join(codexDir, "session.jsonl")
	content := `{"timestamp":"2026-05-19T02:39:55.659Z","type":"session_meta","payload":{"id":"codex-session-3","cwd":"/tmp/final-codex-project","thread_source":"user","model":"gpt-5"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write codex session: %v", err)
	}

	s := &Server{sessionsDir: piDir, codexSessionsDir: codexDir}
	sessions := s.listSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Project != "final-codex-project" {
		t.Fatalf("unexpected project: %q", sessions[0].Project)
	}
}

func writeTempSession(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp session: %v", err)
	}
	return path
}

func longCodexUserMessage(prefix string, n int) string {
	return `{"timestamp":"2026-05-19T02:39:56.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + prefix + strings.Repeat("x", n) + `"}]}}`
}

// --- History endpoint tests ---

// makeHistoryServer creates a test Server with a pi-agent session file.
func makeHistoryServer(t *testing.T, turns int) (*Server, string) {
	t.Helper()

	root := t.TempDir()
	sessionsDir := filepath.Join(root, "pi")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("create sessions dir: %v", err)
	}

	path := filepath.Join(sessionsDir, "test-proj", "2026-07-15T00-00_session-id.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}

	var lines []string
	for i := 1; i <= turns; i++ {
		pad := strconv.Itoa(i)
		if i < 10 {
			pad = "0" + pad
		}
		lines = append(lines, `{"type":"message","id":"m`+pad+`","timestamp":"2026-07-15T00:`+pad+`:00Z","message":{"role":"user","content":[{"type":"text","text":"prompt `+pad+`"}]}}`)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	s := &Server{sessionsDir: sessionsDir, historyService: history.NewService()}
	return s, path
}

func TestHandleHistory_LatestPage(t *testing.T) {
	s, _ := makeHistoryServer(t, 25)
	h := s.handler()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?limit=20", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page struct {
		Events     json.RawMessage `json:"events"`
		NextCursor string          `json:"next_cursor"`
		HasMore    bool            `json:"has_more"`
		Snapshot   struct {
			LineCount int `json:"line_count"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !page.HasMore {
		t.Fatal("expected has_more = true for 25-turn session with limit 20")
	}
	if page.NextCursor == "" {
		t.Fatal("expected non-empty next_cursor")
	}

	var events []json.RawMessage
	json.Unmarshal(page.Events, &events)
	if len(events) < 20 {
		t.Fatalf("events count = %d, want >= 20", len(events))
	}
}

func TestHandleHistory_PreviousPage(t *testing.T) {
	s, _ := makeHistoryServer(t, 25)
	h := s.handler()

	// Fetch latest page first
	req1 := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?limit=20", nil)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("latest page status = %d", rec1.Code)
	}
	var page1 struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	json.Unmarshal(rec1.Body.Bytes(), &page1)

	if !page1.HasMore || page1.NextCursor == "" {
		t.Fatal("expected has_more and cursor from latest page")
	}

	// Fetch previous page
	req2 := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?limit=20&cursor="+page1.NextCursor, nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("previous page status = %d; body=%s", rec2.Code, rec2.Body.String())
	}

	var page2 struct {
		Events     json.RawMessage `json:"events"`
		NextCursor string          `json:"next_cursor"`
		HasMore    bool            `json:"has_more"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &page2)

	if page2.HasMore {
		t.Fatal("expected has_more = false for remaining 5 turns")
	}
	if page2.NextCursor != "" {
		t.Fatal("expected empty next_cursor")
	}

	var events []json.RawMessage
	json.Unmarshal(page2.Events, &events)
	if len(events) < 5 {
		t.Fatalf("previous page events = %d, want >= 5", len(events))
	}
}

func TestHandleHistory_SessionNotFound(t *testing.T) {
	s := &Server{sessionsDir: t.TempDir(), historyService: history.NewService()}
	h := s.handler()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent/history", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleHistory_InvalidCursor(t *testing.T) {
	s, _ := makeHistoryServer(t, 25)
	h := s.handler()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?cursor=not-valid-base64!!!", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleHistory_WrongMethod(t *testing.T) {
	s, _ := makeHistoryServer(t, 5)
	h := s.handler()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/session-id/history", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleHistory_MissingSessionID(t *testing.T) {
	s := &Server{sessionsDir: t.TempDir(), historyService: history.NewService()}
	h := s.handler()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions//history", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Empty ID should result in 404 (no session found)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleHistory_LimitCap(t *testing.T) {
	s, _ := makeHistoryServer(t, 60)
	h := s.handler()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?limit=100", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var page struct {
		HasMore bool `json:"has_more"`
	}
	json.Unmarshal(rec.Body.Bytes(), &page)
	if !page.HasMore {
		t.Fatal("expected has_more = true since limit 100 capped to 50 for 60 turns")
	}
}

func TestHandleHistory_TruncatedFile(t *testing.T) {
	s, path := makeHistoryServer(t, 20)
	h := s.handler()

	// Get cursor
	req1 := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?limit=10", nil)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first page status = %d", rec1.Code)
	}
	var page1 struct {
		NextCursor string `json:"next_cursor"`
	}
	json.Unmarshal(rec1.Body.Bytes(), &page1)
	if page1.NextCursor == "" {
		t.Fatal("expected cursor for 20-turn session with limit 10")
	}

	// Truncate the file
	if err := os.Truncate(path, 100); err != nil {
		t.Fatalf("truncate session: %v", err)
	}

	// Try to use cursor → should get 409
	req2 := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history?cursor="+page1.NextCursor, nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (409 Conflict)", rec2.Code, http.StatusConflict)
	}
}

func TestHandleHistory_DefaultLimit(t *testing.T) {
	s, _ := makeHistoryServer(t, 25)
	h := s.handler()

	// No limit param → defaults to 20
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var page struct {
		HasMore bool `json:"has_more"`
	}
	json.Unmarshal(rec.Body.Bytes(), &page)
	if !page.HasMore {
		t.Fatal("expected has_more = true for 25 turns with default limit 20")
	}
}

func TestHandleHistory_RouteBeforeCatchall(t *testing.T) {
	s, _ := makeHistoryServer(t, 5)
	h := s.handler()

	// GET /api/sessions/session-id/history should reach handleSessionHistory, not handleSessionByID
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-id/history", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("history endpoint status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page struct {
		Events json.RawMessage `json:"events"`
	}
	json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Events == nil {
		t.Fatal("history response should contain events, not a SessionInfo object")
	}
}

// Sort import added - need to ensure sort is imported.
var _ = sort.Ints // no-op to prevent unused import error when test file is compiled without other sort usage
