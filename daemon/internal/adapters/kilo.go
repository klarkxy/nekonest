package adapters

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/agentexec"
	_ "modernc.org/sqlite"
)

// KiloAdapter discovers and controls Kilo CLI sessions.
type KiloAdapter struct {
	dbPath    string
	watcherMu sync.Mutex
	// lastDirs caches session_id -> project directory for SendPrompt.
	lastDirs  map[string]string
	commander *agentexec.KiloCommander
}

// NewKiloAdapter creates a new Kilo adapter.
func NewKiloAdapter() *KiloAdapter {
	return &KiloAdapter{
		dbPath:    resolveKiloDBPath(),
		lastDirs:  make(map[string]string),
		commander: agentexec.NewKiloCommander(),
	}
}

func (a *KiloAdapter) Name() string { return "kilo" }

// IsAvailable reports whether the Kilo CLI is present.
func (a *KiloAdapter) IsAvailable() bool {
	return a.commander.IsAvailable()
}

// GetCommander returns the underlying commander.
func (a *KiloAdapter) GetCommander() *agentexec.KiloCommander {
	return a.commander
}

// Close stops running kilo processes.
func (a *KiloAdapter) Close() error {
	if a.commander != nil {
		a.commander.StopAll()
	}
	return nil
}

// resolveKiloDBPath finds ~/.local/share/kilo/kilo.db (and Windows equivalents).
func resolveKiloDBPath() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "share", "kilo", "kilo.db"),
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		candidates = append([]string{filepath.Join(xdg, "kilo", "kilo.db")}, candidates...)
	}
	// Windows LOCALAPPDATA fallback (some installs)
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		candidates = append(candidates,
			filepath.Join(la, "kilo", "kilo.db"),
			filepath.Join(la, "Kilo", "kilo.db"),
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return candidates[0]
}

// Discover lists recent non-archived Kilo sessions from the local DB.
func (a *KiloAdapter) Discover() ([]*SessionInfo, error) {
	if _, err := os.Stat(a.dbPath); os.IsNotExist(err) {
		return nil, nil
	}

	// Read-only open; tolerate concurrent Kilo writers.
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(500)", filepath.ToSlash(a.dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open kilo db: %w", err)
	}
	defer db.Close()

	// time_updated is unix milliseconds. 7-day window + higher limit for phone history.
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	rows, err := db.Query(`
		SELECT id, title, directory, time_updated, agent
		FROM session
		WHERE time_archived IS NULL
		  AND time_updated >= ?
		ORDER BY time_updated DESC
		LIMIT 100
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*SessionInfo
	dirs := make(map[string]string)

	for rows.Next() {
		var (
			id, title, directory string
			updated              int64
			agent                sql.NullString
		)
		if err := rows.Scan(&id, &title, &directory, &updated, &agent); err != nil {
			continue
		}
		if id == "" {
			continue
		}

		last := time.UnixMilli(updated)
		summary := strings.TrimSpace(title)
		if summary == "" {
			summary = id
		}
		// Prefer last non-empty assistant text part when available.
		if text := a.latestTextSummary(db, id); text != "" {
			summary = text
		}

		status := StatusIdle
		if time.Since(last) < 60*time.Second {
			status = StatusRunning
		}
		if a.hasRunningTool(db, id) {
			status = StatusRunning
		}

		dirs[id] = directory
		a.commander.RememberSessionDir(id, directory)

		sessions = append(sessions, &SessionInfo{
			ID:           id,
			AgentType:    AgentKilo,
			Status:       status,
			Summary:      truncate(summary, 120),
			LastActivity: last,
			SessionPath:  directory,
			ProjectDir:   directory,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	a.watcherMu.Lock()
	a.lastDirs = dirs
	a.watcherMu.Unlock()

	return sessions, nil
}

func (a *KiloAdapter) latestTextSummary(db *sql.DB, sessionID string) string {
	row := db.QueryRow(`
		SELECT data FROM part
		WHERE session_id = ?
		  AND json_extract(data, '$.type') = 'text'
		  AND COALESCE(json_extract(data, '$.text'), '') != ''
		  AND COALESCE(json_extract(data, '$.ignored'), 0) = 0
		ORDER BY time_updated DESC
		LIMIT 1
	`, sessionID)
	var raw string
	if err := row.Scan(&raw); err != nil {
		return ""
	}
	var part struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &part); err != nil {
		return ""
	}
	return strings.TrimSpace(part.Text)
}

func (a *KiloAdapter) hasRunningTool(db *sql.DB, sessionID string) bool {
	row := db.QueryRow(`
		SELECT 1 FROM part
		WHERE session_id = ?
		  AND json_extract(data, '$.type') = 'tool'
		  AND json_extract(data, '$.state.status') = 'running'
		LIMIT 1
	`, sessionID)
	var one int
	return row.Scan(&one) == nil
}

// Watch is a lightweight poll-based channel for a single session.
func (a *KiloAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
	ch := make(chan *SessionInfo, 4)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		var lastUpdated time.Time
		for range ticker.C {
			sessions, err := a.Discover()
			if err != nil {
				continue
			}
			for _, s := range sessions {
				if s.ID == sessionID {
					if s.LastActivity.After(lastUpdated) {
						lastUpdated = s.LastActivity
						select {
						case ch <- s:
						default:
						}
					}
					break
				}
			}
		}
	}()
	return ch, nil
}

// SendPrompt resumes a Kilo session.
func (a *KiloAdapter) SendPrompt(sessionID string, prompt string) error {
	if !a.commander.IsAvailable() {
		return fmt.Errorf("kilo CLI not found in PATH or VS Code extension")
	}
	dir := a.sessionDir(sessionID)
	if dir != "" {
		return a.commander.SendPromptInDir(sessionID, prompt, dir)
	}
	return a.commander.SendPrompt(sessionID, prompt)
}

func (a *KiloAdapter) sessionDir(sessionID string) string {
	a.watcherMu.Lock()
	dir := a.lastDirs[sessionID]
	a.watcherMu.Unlock()
	if dir != "" {
		return dir
	}
	// Fallback: one-shot discover
	if sessions, err := a.Discover(); err == nil {
		for _, s := range sessions {
			if s.ID == sessionID {
				return s.SessionPath
			}
		}
	}
	return a.commander.SessionDir(sessionID)
}

func (a *KiloAdapter) Approve(sessionID string, approvalID string) error {
	return a.commander.Approve(sessionID, approvalID)
}

func (a *KiloAdapter) Deny(sessionID string, approvalID string) error {
	return a.commander.Deny(sessionID, approvalID)
}

func (a *KiloAdapter) Interrupt(sessionID string) error {
	return a.commander.Interrupt(sessionID)
}

// FetchHistory loads last N text turns from kilo.db (part + message).
func (a *KiloAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	limit = clampHistoryLimit(limit)
	if _, err := os.Stat(a.dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kilo db not found")
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(500)", filepath.ToSlash(a.dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT
			p.id,
			COALESCE(json_extract(m.data, '$.role'), ''),
			COALESCE(json_extract(p.data, '$.text'), ''),
			p.time_created
		FROM part p
		JOIN message m ON m.id = p.message_id
		WHERE p.session_id = ?
		  AND json_extract(p.data, '$.type') = 'text'
		  AND COALESCE(json_extract(p.data, '$.text'), '') != ''
		  AND COALESCE(json_extract(p.data, '$.ignored'), 0) = 0
		  AND json_extract(m.data, '$.role') IN ('user', 'assistant')
		ORDER BY p.time_created DESC
		LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("kilo history: %w", err)
	}
	defer rows.Close()

	var rev []*HistoryMessage
	for rows.Next() {
		var id, role, content string
		var tsMs int64
		if err := rows.Scan(&id, &role, &content, &tsMs); err != nil {
			continue
		}
		content = strings.TrimSpace(content)
		if content == "" || (role != "user" && role != "assistant") {
			continue
		}
		ts := tsMs / 1000
		if ts <= 0 {
			ts = time.Now().Unix()
		}
		rev = append(rev, &HistoryMessage{
			ID:        id,
			Role:      role,
			Content:   truncateRunes(content, 4000),
			Type:      "text",
			Timestamp: ts,
		})
	}
	// reverse DESC → ASC
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, rows.Err()
}

// ensure implements ClosableAdapter
var _ ClosableAdapter = (*KiloAdapter)(nil)
var _ Adapter = (*KiloAdapter)(nil)
var _ Adapter = (*ClaudeCodeAdapter)(nil)
var _ Adapter = (*CodexAdapter)(nil)
