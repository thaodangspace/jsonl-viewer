// Package server provides the HTTP + WebSocket server.
package server

import (
	"bufio"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-reader/internal/history"
	"agent-reader/internal/hub"
	"agent-reader/internal/jsonl"
	"agent-reader/internal/llm"
	"agent-reader/internal/readtracker"
	"agent-reader/internal/watcher"

	"github.com/gorilla/websocket"
)

//go:embed static/dist/*
var staticFS embed.FS

const (
	desktopOriginTauri = "tauri://localhost"
	desktopOriginHTTP  = "http://tauri.localhost"
)

var allowedDesktopOrigins = map[string]struct{}{
	desktopOriginTauri: {},
	desktopOriginHTTP:  {},
}

// Server ties together the HTTP server, WebSocket hub, and file watchers.
type Server struct {
	hub               *hub.Hub
	watcher           *watcher.Watcher       // pi-agent watcher
	claudeWatcher     *watcher.ClaudeWatcher // Claude Code watcher (may be nil)
	codexWatcher      *watcher.CodexWatcher  // Codex watcher (may be nil)
	readTracker       *readtracker.ReadTracker
	sessionsDir       string
	claudeProjectsDir string
	codexSessionsDir  string
	llmClient         *llm.LMStudioClient // local LLM client for translation
	historyService    *history.Service
	desktop           bool
}

// New creates a new Server.
func New(sessionsDir, claudeProjectsDir, codexSessionsDir string, desktop bool) (*Server, error) {
	w, err := watcher.New(sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	h := hub.New()

	dbPath := filepath.Join(sessionsDir, "read_tracker.db")
	rt, err := readtracker.New(dbPath, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("create read tracker: %w", err)
	}

	s := &Server{
		hub:               h,
		watcher:           w,
		readTracker:       rt,
		historyService:    history.NewService(),
		sessionsDir:       sessionsDir,
		claudeProjectsDir: claudeProjectsDir,
		codexSessionsDir:  codexSessionsDir,
		llmClient:         llm.NewLMStudioClient(),
		desktop:           desktop,
	}

	log.Printf("[server] read tracker initialized (24h TTL) at %s", dbPath)

	// Try to create Claude watcher (optional — skip if dir doesn't exist)
	if claudeProjectsDir != "" {
		if info, err := os.Stat(claudeProjectsDir); err == nil && info.IsDir() {
			cw, err := watcher.NewClaudeWatcher(claudeProjectsDir)
			if err != nil {
				log.Printf("[server] warning: could not create Claude watcher: %v", err)
			} else {
				s.claudeWatcher = cw
				log.Printf("[server] Claude Code watcher enabled: %s", claudeProjectsDir)
			}
		} else {
			log.Printf("[server] Claude projects dir not found, skipping: %s", claudeProjectsDir)
		}
	}

	// Try to create Codex watcher (optional — skip if dir doesn't exist)
	if codexSessionsDir != "" {
		if info, err := os.Stat(codexSessionsDir); err == nil && info.IsDir() {
			cw, err := watcher.NewCodexWatcher(codexSessionsDir)
			if err != nil {
				log.Printf("[server] warning: could not create Codex watcher: %v", err)
			} else {
				s.codexWatcher = cw
				log.Printf("[server] Codex watcher enabled: %s", codexSessionsDir)
			}
		} else {
			log.Printf("[server] Codex sessions dir not found, skipping: %s", codexSessionsDir)
		}
	}

	return s, nil
}

// Start launches the HTTP server on the given address.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", s.handleHealthz)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWS)

	// REST API
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/unread", s.handleUnreadIDs)
	mux.HandleFunc("GET /api/sessions/{id}/history", s.handleSessionHistory)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)

	// Historical images are served read-only.
	mux.HandleFunc("/api/images/view", s.handleImageView)

	// Translation
	mux.HandleFunc("/api/translate", s.handleTranslate)

	// Static files (Svelte SPA with fallback)
	staticSub, err := fs.Sub(staticFS, "static/dist")
	if err == nil {
		mux.Handle("/", spaHandler(staticSub))
	} else {
		mux.Handle("/", http.FileServer(http.Dir("internal/server/static/dist")))
	}

	// ServeMux normalizes paths containing a double slash. Handle the
	// missing-session history path before it reaches ServeMux so it remains a
	// normal not-found response rather than a redirect.
	return s.withDesktopCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/sessions//history" {
			s.handleSessionHistory(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

// Start launches the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	handler := s.handler()

	log.Printf("[server] listening on %s", addr)
	log.Printf("[server] WebSocket: ws://localhost%s/ws", addr[strings.Index(addr, ":"):])
	log.Printf("[server] Sessions dir: %s", s.sessionsDir)

	// History is now delivered via HTTP cursor pagination (GET /api/sessions/{id}/history).
	// onSubscribe is no longer registered; WebSocket is reserved for live events only.
	go s.hub.Run()
	go s.hub.SubscribeWatcher(s.watcher)
	s.watcher.Start()

	if s.claudeWatcher != nil {
		go s.hub.SubscribeClaudeWatcher(s.claudeWatcher)
		s.claudeWatcher.Start()
	}
	if s.codexWatcher != nil {
		go s.hub.SubscribeCodexWatcher(s.codexWatcher)
		s.codexWatcher.Start()
	}

	return http.ListenAndServe(addr, handler)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) withDesktopCORS(next http.Handler) http.Handler {
	if !s.desktop {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isDesktopCORSPath(r.URL.Path) && isAllowedDesktopOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) websocketUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.checkWSOrigin,
	}
}

func (s *Server) checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if s.desktop && isAllowedDesktopOrigin(origin) {
		return true
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func isAllowedDesktopOrigin(origin string) bool {
	if _, ok := allowedDesktopOrigins[origin]; ok {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func isDesktopCORSPath(path string) bool {
	return strings.HasPrefix(path, "/api/") || path == "/healthz"
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.watcher.Stop()
	if s.claudeWatcher != nil {
		s.claudeWatcher.Stop()
	}
	if s.codexWatcher != nil {
		s.codexWatcher.Stop()
	}
	if s.readTracker != nil {
		s.readTracker.Stop()
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := s.websocketUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[server] upgrade error: %v", err)
		return
	}

	client := hub.NewClient(s.hub, conn)
	go client.Serve()
}

// ===== REST API =====

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/api/sessions" {
		return
	}

	// Parse page parameter (default: 1)
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
		if page < 1 {
			page = 1
		}
	}

	sessions := s.listSessions()

	// Parse parameters
	groupBy := r.URL.Query().Get("group_by")
	sortBy := r.URL.Query().Get("sort")

	var total int

	if groupBy == "project" {
		// Group sessions by project
		groups := make(map[string][]SessionInfo)
		for _, sess := range sessions {
			key := sess.Project
			if key == "" {
				key = sess.CWD
			}
			if key == "" {
				key = "unknown"
			}
			groups[key] = append(groups[key], sess)
		}

		// Always sort sessions within each project group by timestamp descending
		for key := range groups {
			sort.Slice(groups[key], func(i, j int) bool {
				return groups[key][i].Timestamp.After(groups[key][j].Timestamp)
			})
		}

		// Create a list of project keys
		type projectMeta struct {
			key             string
			newestTimestamp time.Time
		}
		var projects []projectMeta
		for key, list := range groups {
			newest := time.Time{}
			if len(list) > 0 {
				newest = list[0].Timestamp
			}
			projects = append(projects, projectMeta{
				key:             key,
				newestTimestamp: newest,
			})
		}

		// Sort the projects
		if sortBy == "alphabetical" {
			sort.Slice(projects, func(i, j int) bool {
				return strings.ToLower(projects[i].key) < strings.ToLower(projects[j].key)
			})
		} else {
			// default to last_updated
			sort.Slice(projects, func(i, j int) bool {
				return projects[i].newestTimestamp.After(projects[j].newestTimestamp)
			})
		}

		total = len(projects)

		// Paginate projects: 50 projects per page
		const projectPageSize = 50
		offset := (page - 1) * projectPageSize
		var paginatedProjects []projectMeta
		if offset < total {
			end := offset + projectPageSize
			if end > total {
				end = total
			}
			paginatedProjects = projects[offset:end]
		}

		// Collect all sessions for the paginated projects
		var resultSessions []SessionInfo
		for _, p := range paginatedProjects {
			resultSessions = append(resultSessions, groups[p.key]...)
		}
		sessions = resultSessions

	} else {
		// Flat pagination
		// Sort based on "sort" parameter
		if sortBy == "alphabetical" {
			sort.Slice(sessions, func(i, j int) bool {
				pI := sessions[i].Project
				if pI == "" {
					pI = sessions[i].CWD
				}
				pI = strings.ToLower(pI)

				pJ := sessions[j].Project
				if pJ == "" {
					pJ = sessions[j].CWD
				}
				pJ = strings.ToLower(pJ)

				if pI != pJ {
					return pI < pJ
				}
				return sessions[i].Timestamp.After(sessions[j].Timestamp)
			})
		} else {
			// Default to last_updated
			sort.Slice(sessions, func(i, j int) bool {
				return sessions[i].Timestamp.After(sessions[j].Timestamp)
			})
		}

		total = len(sessions)

		// Paginate: 100 sessions per page
		const pageSize = 100
		offset := (page - 1) * pageSize
		if offset >= total {
			sessions = []SessionInfo{}
		} else {
			end := offset + pageSize
			if end > total {
				end = total
			}
			sessions = sessions[offset:end]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"total":    total,
	})
}

// handleSessionHistory serves paginated history via GET /api/sessions/{id}/history.
// Query params: ?limit=N (default 20, max 50), &cursor=... (for previous pages).
func (s *Server) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusNotFound)
		return
	}

	// Find the session file and agent
	sessions := s.listSessions()
	var sessionFile, sessionAgent string
	for i := range sessions {
		if sessions[i].ID == id {
			sessionFile = sessions[i].File
			sessionAgent = sessions[i].Agent
			break
		}
	}
	if sessionFile == "" {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Parse limit
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	cursorStr := r.URL.Query().Get("cursor")

	var page *history.Page
	var err error

	if cursorStr != "" {
		page, err = s.historyService.LoadPrevious(id, sessionAgent, sessionFile, cursorStr, limit)
	} else {
		page, err = s.historyService.LoadLatest(id, sessionAgent, sessionFile, limit)
	}

	if err != nil {
		switch {
		case errors.Is(err, history.ErrSessionNotFound):
			http.Error(w, "session not found", http.StatusNotFound)
		case errors.Is(err, history.ErrInvalidCursor):
			http.Error(w, "invalid cursor", http.StatusBadRequest)
		case errors.Is(err, history.ErrHistoryChanged):
			http.Error(w, "history changed", http.StatusConflict)
		default:
			log.Printf("[server] history error for session %s: %v", id, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	// Dispatch mark-read sub-route
	if strings.HasSuffix(id, "/mark-read") {
		markReadID := strings.TrimSuffix(id, "/mark-read")
		if markReadID == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Optionally accept line_count in request body
		var req struct {
			LineCount int `json:"line_count"`
		}
		json.NewDecoder(r.Body).Decode(&req) // ignore error, line_count defaults to 0
		if s.readTracker != nil {
			s.readTracker.MarkRead(markReadID, req.LineCount)
		}
		log.Printf("[server] session marked as read: %s (seen_lines=%d)", markReadID, req.LineCount)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessions := s.listSessions()
	var found *SessionInfo
	for i := range sessions {
		if sessions[i].ID == id {
			found = &sessions[i]
			break
		}
	}

	if found == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(found)
}

// handleUnreadIDs returns all unread session IDs.
// GET /api/sessions/unread
func (s *Server) handleUnreadIDs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var ids []string
	if s.readTracker != nil {
		sessions := s.listSessions()
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, sess := range sessions {
			if sess.Timestamp.After(cutoff) && s.readTracker.IsUnread(sess.ID, sess.LineCount) {
				ids = append(ids, sess.ID)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"unread_ids": ids,
	})
}

// resolvePiImagesDir resolves the read-only historical image directory.
func resolvePiImagesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "images"), nil
}

// handleImageView serves images from ~/.pi/images/ or clipboard temp paths.
// The path is passed as a base64url-encoded query parameter: /api/images/view?p=<base64url>
func (s *Server) handleImageView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	encoded := r.URL.Query().Get("p")
	if encoded == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	// Decode base64url (frontend strips padding, so use RawURLEncoding)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		// Fall back to padded URLEncoding for backwards compatibility
		decoded, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	imagePath := string(decoded)

	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Security: verify the path is under ~/.pi/images/ OR is a clipboard temp file
	imagesDir, err := resolvePiImagesDir()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	isPiImage := strings.HasPrefix(absPath, imagesDir+string(filepath.Separator))
	isClipboardTemp := strings.HasPrefix(absPath, "/var/folders/") && strings.Contains(filepath.Base(absPath), "pi-clipboard-")

	if !isPiImage && !isClipboardTemp {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Validate file extension is an image
	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".tiff":
		// okay
	default:
		http.Error(w, "not an image", http.StatusBadRequest)
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "image not found", http.StatusNotFound)
		} else {
			http.Error(w, "read error", http.StatusInternalServerError)
		}
		return
	}

	// Set cache headers (images are immutable once uploaded)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", imageMimeFromExt(ext))
	w.Write(data)
}

// imageMimeFromExt maps file extensions to MIME types.
func imageMimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

// ===== Translation =====

// handleTranslate translates text to Vietnamese using local LLM (LM Studio).
// POST /api/translate { "text": "...", "target_lang": "vi" }
func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Text       string `json:"text"`
		TargetLang string `json:"target_lang"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "missing text", http.StatusBadRequest)
		return
	}

	if req.TargetLang == "" {
		req.TargetLang = "vi" // default to Vietnamese
	}

	log.Printf("[server] translate request: text_len=%d target=%s", len(req.Text), req.TargetLang)

	translated, err := s.llmClient.Translate(req.Text, req.TargetLang)
	if err != nil {
		log.Printf("[server] translate error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	log.Printf("[server] translate success: result_len=%d", len(translated))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"translated": translated,
	})
}

// ===== Helpers =====

// findSessionFile finds the JSONL file for a given session ID.
func (s *Server) findSessionFile(sessionID string) string {
	sessions := s.listSessions()
	for i := range sessions {
		if sessions[i].ID == sessionID {
			return sessions[i].File
		}
	}
	return ""
}

// SessionInfo is returned by the /api/sessions endpoint.
type SessionInfo struct {
	ID               string    `json:"id"`
	Project          string    `json:"project"`
	CWD              string    `json:"cwd"`
	Model            string    `json:"model"`
	ContextWindow    int64     `json:"context_window"`
	Agent            string    `json:"agent"`
	Timestamp        time.Time `json:"timestamp"`
	FirstUserMessage string    `json:"first_user_message"`
	LastMessageTime  string    `json:"last_message_time"`
	File             string    `json:"file"`
	LineCount        int       `json:"line_count"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	TotalCost        float64   `json:"total_cost"`
	IsActive         bool      `json:"is_active"`
	Status           string    `json:"status"` // "running", "completed", "error"
	IsUnread         bool      `json:"is_unread"`
}

// listSessions scans pi-agent, Claude Code, and Codex session directories.
func (s *Server) listSessions() []SessionInfo {
	var sessions []SessionInfo

	// Scan pi-agent sessions
	filepath.WalkDir(s.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		info := SessionInfo{File: path, Agent: "pi"}

		dir := filepath.Dir(path)
		info.Project = filepath.Base(dir)

		base := filepath.Base(path)
		for i := len(base) - 1; i >= 0; i-- {
			if base[i] == '_' {
				info.ID = base[i+1 : len(base)-len(".jsonl")]
				break
			}
		}

		info.LineCount, info.CWD, info.Model, info.InputTokens, info.OutputTokens, info.TotalTokens, info.TotalCost, info.ContextWindow = aggregateSessionData(path, "pi")
		info.FirstUserMessage = getFirstUserMessage(path, "pi")
		info.LastMessageTime, info.IsActive, info.Status = getLastMessageTimeAndStatus(path)

		if fi, err := d.Info(); err == nil {
			info.Timestamp = fi.ModTime()
		}

		sessions = append(sessions, info)
		return nil
	})

	// Scan Claude Code sessions
	if s.claudeProjectsDir != "" {
		filepath.WalkDir(s.claudeProjectsDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			// Skip subagents
			if strings.Contains(path, "/subagents/") {
				return nil
			}

			info := SessionInfo{File: path, Agent: "claude"}

			base := filepath.Base(path)
			info.ID = strings.TrimSuffix(base, ".jsonl")

			info.LineCount, info.CWD, info.Model, info.InputTokens, info.OutputTokens, info.TotalTokens, info.TotalCost, info.ContextWindow = aggregateSessionData(path, "claude")
			info.FirstUserMessage = getFirstUserMessage(path, "claude")
			info.LastMessageTime, info.IsActive, info.Status = getLastMessageTimeAndStatus(path)

			if fi, err := d.Info(); err == nil {
				info.Timestamp = fi.ModTime()
			}

			// For Claude, project name comes from cwd
			if info.CWD != "" {
				info.Project = filepath.Base(info.CWD)
			} else {
				info.Project = filepath.Base(filepath.Dir(path))
			}

			sessions = append(sessions, info)
			return nil
		})
	}

	// Scan Codex sessions
	if s.codexSessionsDir != "" {
		filepath.WalkDir(s.codexSessionsDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}

			meta, ok := readCodexSessionInfo(path)
			if !ok {
				return nil
			}

			info := SessionInfo{
				ID:    meta.ID,
				File:  path,
				Agent: "codex",
				CWD:   meta.CWD,
				Model: meta.Model,
			}

			info.LineCount, info.CWD, info.Model, info.InputTokens, info.OutputTokens, info.TotalTokens, info.TotalCost, info.ContextWindow = aggregateSessionData(path, "codex")
			if info.CWD == "" {
				info.CWD = meta.CWD
			}
			if info.Model == "" {
				info.Model = meta.Model
			}
			if info.CWD != "" {
				info.Project = filepath.Base(info.CWD)
			} else {
				info.Project = filepath.Base(filepath.Dir(path))
			}
			info.FirstUserMessage = getFirstUserMessage(path, "codex")
			info.LastMessageTime, info.IsActive, info.Status = getLastMessageTimeAndStatus(path)

			if fi, err := d.Info(); err == nil {
				info.Timestamp = fi.ModTime()
			}

			sessions = append(sessions, info)
			return nil
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})

	// Mark IsUnread: sessions within 24h that have new content since the user last viewed them
	if s.readTracker != nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for i := range sessions {
			if sessions[i].Timestamp.After(cutoff) && s.readTracker.IsUnread(sessions[i].ID, sessions[i].LineCount) {
				sessions[i].IsUnread = true
			}
		}
	}

	return sessions
}

// countLinesAndCWD reads the JSONL file to get CWD, model, and counts total lines.
func countLinesAndCWD(path string) (int, string, string) {
	lineCount, cwd, model, _, _, _, _, _ := aggregateSessionData(path, "pi")
	return lineCount, cwd, model
}

func readCodexSessionInfo(path string) (jsonl.CodexSessionMeta, bool) {
	var found jsonl.CodexSessionMeta
	ok := false
	scanCodexLines(path, func(line []byte) bool {
		meta, parsed := jsonl.ParseCodexSessionMeta(line)
		if !parsed {
			return true
		}
		if jsonl.IsCodexUserSession(meta) {
			found = meta
			ok = true
		}
		return false
	})

	return found, ok
}

func scanCodexLines(path string, visit func([]byte) bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytesTrimRightNewline(line)
			if !visit(line) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func bytesTrimRightNewline(line []byte) []byte {
	for len(line) > 0 {
		last := line[len(line)-1]
		if last != '\n' && last != '\r' {
			break
		}
		line = line[:len(line)-1]
	}
	return line
}

// getContextWindow returns the context window size for a given model ID.
// Returns 0 if the model is unknown.
func getContextWindow(model string) int64 {
	if model == "" {
		return 0
	}
	// Strip provider prefix (e.g., "anthropic.", "us.anthropic.", "bedrock.")
	cleanModel := model
	for _, prefix := range []string{"us.anthropic.", "eu.anthropic.", "au.anthropic.", "global.anthropic.", "anthropic.", "bedrock.", "openai.", "google.", "meta.", "mistral.", "deepseek.", "qwen.", "zai.", "minimax.", "nvidia.", "moonshot.", "moonshotai.", "writer.", "amazon."} {
		if strings.HasPrefix(cleanModel, prefix) {
			cleanModel = strings.TrimPrefix(cleanModel, prefix)
			break
		}
	}
	// Strip Bedrock version suffix (e.g., "-v1:0")
	cleanModel = strings.TrimSuffix(cleanModel, "-v1:0")

	switch cleanModel {
	// Claude models
	case "claude-opus-4-7", "claude-opus-4.7":
		return 200000
	case "claude-opus-4-6", "claude-opus-4.6":
		return 200000
	case "claude-opus-4-5", "claude-opus-4.5", "claude-opus-4-20251101":
		return 200000
	case "claude-opus-4-1", "claude-opus-4.1", "claude-opus-4-20250805":
		return 200000
	case "claude-opus-4", "claude-opus-4-20250514":
		return 200000
	case "claude-sonnet-4-6", "claude-sonnet-4.6":
		return 200000
	case "claude-sonnet-4-5", "claude-sonnet-4.5", "claude-sonnet-4-20250929":
		return 200000
	case "claude-sonnet-4", "claude-sonnet-4-20250514":
		return 200000
	case "claude-haiku-4-5", "claude-haiku-4.5", "claude-haiku-4-20251001":
		return 200000
	case "claude-3-7-sonnet", "claude-3-7-sonnet-20250219":
		return 200000
	case "claude-3-5-sonnet", "claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620":
		return 200000
	case "claude-3-5-haiku", "claude-3-5-haiku-20241022":
		return 200000
	case "claude-3-haiku", "claude-3-haiku-20240307":
		return 200000
	// GPT models
	case "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "gpt-4o", "gpt-4o-mini", "gpt-4-turbo":
		return 128000
	case "gpt-4":
		return 8192
	case "gpt-3.5-turbo":
		return 16385
	case "o1", "o1-mini", "o1-preview":
		return 128000
	case "o3", "o3-mini", "o4-mini":
		return 200000
	// Gemini models
	case "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash":
		return 1048576
	case "gemini-1.5-pro", "gemini-1.5-flash":
		return 2097152
	// Claude extended thinking variants
	case "claude-sonnet-4-20250514-extended-thinking", "claude-sonnet-4-20250514-thinking":
		return 200000
	case "claude-opus-4-20250514-extended-thinking", "claude-opus-4-20250514-thinking":
		return 200000
	default:
		return 0
	}
}

// aggregateSessionData reads the JSONL file and aggregates all session metadata.
// The agent parameter distinguishes between "pi" and "claude" formats.
func aggregateSessionData(path string, agent string) (lineCount int, cwd string, model string, inputTokens, outputTokens, totalTokens int64, totalCost float64, contextWindow int64) {
	if agent == "codex" {
		return aggregateCodexSessionData(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, "", "", 0, 0, 0, 0, 0
	}
	defer f.Close()

	count := 0
	buf := make([]byte, 32*1024)
	scanner := NewLineScanner(f, buf)

	for scanner.Scan() {
		count++
		line := scanner.Bytes()

		if agent == "pi" {
			// pi-agent: cwd from first-line session event
			if count == 1 {
				var first struct {
					Type string `json:"type"`
					CWD  string `json:"cwd"`
				}
				json.Unmarshal(line, &first)
				if first.Type == "session" {
					cwd = first.CWD
				}
			}
			// Look for model_change events
			if model == "" {
				var mc struct {
					Type    string `json:"type"`
					ModelID string `json:"modelId"`
				}
				if json.Unmarshal(line, &mc) == nil && mc.Type == "model_change" && mc.ModelID != "" {
					model = mc.ModelID
				}
			}
			// Look for assistant messages with model field
			if model == "" {
				var me struct {
					Type    string `json:"type"`
					Message struct {
						Role  string `json:"role"`
						Model string `json:"model"`
					} `json:"message"`
				}
				if json.Unmarshal(line, &me) == nil && me.Type == "message" && me.Message.Role == "assistant" && me.Message.Model != "" {
					model = me.Message.Model
				}
			}
			// Aggregate usage from assistant messages (pi format)
			var usageCheck struct {
				Type    string `json:"type"`
				Message struct {
					Role  string `json:"role"`
					Usage *struct {
						Input       int64 `json:"input"`
						Output      int64 `json:"output"`
						TotalTokens int64 `json:"totalTokens"`
						Cost        struct {
							Total float64 `json:"total"`
						} `json:"cost"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &usageCheck) == nil && usageCheck.Type == "message" && usageCheck.Message.Role == "assistant" && usageCheck.Message.Usage != nil {
				u := usageCheck.Message.Usage
				inputTokens += u.Input
				outputTokens += u.Output
				totalTokens += u.TotalTokens
				totalCost += u.Cost.Total
			}
		} else {
			// Claude Code: cwd from any event's top-level field
			if cwd == "" {
				var cwCheck struct {
					CWD string `json:"cwd"`
				}
				json.Unmarshal(line, &cwCheck)
				if cwCheck.CWD != "" {
					cwd = cwCheck.CWD
				}
			}
			// Claude Code: assistant messages with model and snake_case usage
			var claudeCheck struct {
				Type    string `json:"type"`
				Message struct {
					Role  string `json:"role"`
					Model string `json:"model"`
					Usage *struct {
						InputTokens  int64 `json:"input_tokens"`
						OutputTokens int64 `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &claudeCheck) == nil && claudeCheck.Type == "assistant" && claudeCheck.Message.Role == "assistant" {
				if model == "" && claudeCheck.Message.Model != "" {
					model = claudeCheck.Message.Model
				}
				if claudeCheck.Message.Usage != nil {
					inputTokens += claudeCheck.Message.Usage.InputTokens
					outputTokens += claudeCheck.Message.Usage.OutputTokens
					totalTokens += claudeCheck.Message.Usage.InputTokens + claudeCheck.Message.Usage.OutputTokens
				}
			}
		}
	}

	contextWindow = getContextWindow(model)
	return count, cwd, model, inputTokens, outputTokens, totalTokens, totalCost, contextWindow
}

func aggregateCodexSessionData(path string) (lineCount int, cwd string, model string, inputTokens, outputTokens, totalTokens int64, totalCost float64, contextWindow int64) {
	scanCodexLines(path, func(line []byte) bool {
		lineCount++
		if meta, ok := jsonl.ParseCodexSessionMeta(line); ok {
			if cwd == "" {
				cwd = meta.CWD
			}
			if model == "" {
				model = meta.Model
			}
			return true
		}
		if model == "" {
			var ctx struct {
				Type    string `json:"type"`
				Model   string `json:"model"`
				Payload struct {
					Type  string `json:"type"`
					Model string `json:"model"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &ctx) == nil {
				if ctx.Type == "turn_context" {
					if ctx.Model != "" {
						model = ctx.Model
					} else if ctx.Payload.Model != "" {
						model = ctx.Payload.Model
					}
				} else if ctx.Type == "response_item" && ctx.Payload.Type == "turn_context" && ctx.Payload.Model != "" {
					model = ctx.Payload.Model
				}
			}
		}
		return true
	})

	contextWindow = getContextWindow(model)
	return lineCount, cwd, model, 0, 0, 0, 0, contextWindow
}

// getFirstUserMessage reads the JSONL file and returns the text content of the first user message (truncated to 200 chars).
func getFirstUserMessage(path string, agent string) string {
	if agent == "codex" {
		var first string
		scanCodexLines(path, func(line []byte) bool {
			var env jsonl.CodexEnvelope
			if json.Unmarshal(line, &env) != nil || env.Type != "response_item" {
				return true
			}
			var msg jsonl.CodexMessage
			if json.Unmarshal(env.Payload, &msg) != nil || msg.Type != "message" || msg.Role != "user" {
				return true
			}
			for _, block := range msg.Content {
				if (block.Type == "input_text" || block.Type == "text") && block.Text != "" {
					if strings.HasPrefix(strings.TrimSpace(block.Text), "<environment_context>") {
						continue
					}
					first = truncateMessage(block.Text)
					return false
				}
			}
			return true
		})
		return first
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	scanner := NewLineScanner(f, buf)

	for scanner.Scan() {
		line := scanner.Bytes()

		if agent == "pi" {
			var evt struct {
				Type    string `json:"type"`
				Message *struct {
					Role    string `json:"role"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &evt) == nil && evt.Type == "message" && evt.Message != nil && evt.Message.Role == "user" {
				for _, block := range evt.Message.Content {
					if block.Type == "text" && block.Text != "" {
						return truncateMessage(block.Text)
					}
				}
			}
		} else {
			// Claude: content can be string or array
			var evtStr struct {
				Type    string `json:"type"`
				Message *struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &evtStr) == nil && evtStr.Type == "user" && evtStr.Message != nil && evtStr.Message.Role == "user" && evtStr.Message.Content != "" {
				return truncateMessage(evtStr.Message.Content)
			}
			var evtArr struct {
				Type    string `json:"type"`
				Message *struct {
					Role    string `json:"role"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &evtArr) == nil && evtArr.Type == "user" && evtArr.Message != nil && evtArr.Message.Role == "user" {
				for _, block := range evtArr.Message.Content {
					if block.Type == "text" && block.Text != "" {
						return truncateMessage(block.Text)
					}
				}
			}
		}
	}
	return ""
}

func truncateMessage(s string) string {
	const maxLen = 200
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// getLastMessageTimeAndStatus reads the last line of the JSONL file and returns
// a formatted timestamp and whether the session is still active.
// A session is active if the last message was within 3 minutes AND the last
// event is not a terminal event (turn_end, session_end, or message with a
// terminal stopReason like end_turn/stop).
// Status is "running", "completed", or "error".
func getLastMessageTimeAndStatus(path string) (string, bool, string) {
	allBuf, err := os.ReadFile(path)
	if err != nil || len(allBuf) == 0 {
		return "", false, "completed"
	}

	// Find the last non-empty line (skip trailing newlines)
	end := len(allBuf) - 1
	for end >= 0 && allBuf[end] == '\n' {
		end--
	}
	if end < 0 {
		return "", false, "completed"
	}

	start := end
	for start > 0 && allBuf[start-1] != '\n' {
		start--
	}

	lastLine := allBuf[start : end+1]
	if len(lastLine) == 0 {
		return "", false, "completed"
	}

	var lineData struct {
		Type       string `json:"type"`
		Timestamp  string `json:"timestamp"`
		StopReason string `json:"stopReason"`
		Message    *struct {
			StopReason string `json:"stopReason"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(lastLine, &lineData); err != nil || lineData.Timestamp == "" {
		return "", false, "completed"
	}

	t, err := time.Parse(time.RFC3339Nano, lineData.Timestamp)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", lineData.Timestamp)
		if err != nil {
			return "", false, "completed"
		}
	}

	// Check if the last event is a terminal event
	isTerminal := false
	eventType := lineData.Type
	if eventType == "turn_end" || eventType == "session_end" {
		isTerminal = true
	}
	// Check stopReason at top level or nested in message
	stopReason := lineData.StopReason
	if lineData.Message != nil && lineData.Message.StopReason != "" {
		stopReason = lineData.Message.StopReason
	}
	if stopReason == "end_turn" || stopReason == "stop" || stopReason == "completed" {
		isTerminal = true
	}

	// Determine status
	status := "completed"
	isRecent := time.Since(t) < 3*time.Minute

	// Check for error events
	if lineData.Error != "" || eventType == "error" || stopReason == "error" || stopReason == "cancelled" {
		status = "error"
	}

	// Active if recent AND not terminal
	isActive := isRecent && !isTerminal
	if isActive {
		status = "running"
	}

	return formatRelativeTime(t), isActive, status
}

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	}
	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	}
	if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}

	return t.Format("Jan 2")
}

// LineScanner is a simple line-by-line scanner.
type LineScanner struct {
	buf  []byte
	line []byte
	err  error
	pos  int
	n    int
	r    *os.File
}

func NewLineScanner(r *os.File, buf []byte) *LineScanner {
	return &LineScanner{r: r, buf: buf}
}

func (s *LineScanner) Scan() bool {
	if s.err != nil {
		return false
	}
	for {
		if s.pos >= s.n {
			s.pos = 0
			s.n, s.err = s.r.Read(s.buf)
			if s.n == 0 {
				return false
			}
		}
		for i := s.pos; i < s.n; i++ {
			if s.buf[i] == '\n' {
				s.line = s.buf[s.pos:i]
				s.pos = i + 1
				return true
			}
		}
		s.line = s.buf[s.pos:s.n]
		s.pos = s.n
		return false
	}
}

func (s *LineScanner) Bytes() []byte { return s.line }

// spaHandler serves the Svelte SPA with index.html fallback for client-side routing
func spaHandler(fileSystem fs.FS) http.Handler {
	index, err := fs.ReadFile(fileSystem, "index.html")
	if err != nil {
		log.Fatalf("failed to read index.html: %v", err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the requested file
		path := filepath.Join(".", r.URL.Path)
		f, err := fileSystem.Open(path)
		if err == nil {
			f.Close()
			http.FileServer(http.FS(fileSystem)).ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA routing
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}
