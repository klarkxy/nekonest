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
	runs      sync.Map // kiloRunKey -> *kiloRunState
}

type kiloRunKey struct {
	sessionID string
	runNumber uint64
}

type kiloRunState struct {
	startedAt int64
	stop      func()
	mu        sync.Mutex
	pending   *pendingKiloError
}

type pendingKiloError struct {
	messageID string
	content   string
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
	a.clearRuns()
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
	query := `
		SELECT id, title, directory, time_updated, agent
		FROM session
		WHERE time_archived IS NULL
		  AND time_updated >= ?`
	if kiloSessionHasParentID(db) {
		query += `
		  AND parent_id IS NULL`
	}
	query += `
		ORDER BY time_updated DESC
		LIMIT 100`
	rows, err := db.Query(query, cutoff)
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

// OwnsSession checks the authoritative Kilo session table. It deliberately
// does not use FetchHistory: a known session may legitimately have no text
// parts yet.
func (a *KiloAdapter) OwnsSession(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	if _, err := os.Stat(a.dbPath); err != nil {
		return false
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(500)", filepath.ToSlash(a.dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return false
	}
	defer db.Close()

	exists, err := kiloVisibleSessionExists(db, sessionID)
	return err == nil && exists
}

// kiloSessionHasParentID keeps discovery compatible with older Kilo schemas.
// Current Kilo/OpenCode versions use session.parent_id as the authoritative
// child-agent marker.
func kiloSessionHasParentID(db *sql.DB) bool {
	rows, err := db.Query(`PRAGMA table_info(session)`)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid      int
			name     string
			colType  string
			notNull  int
			defaultV interface{}
			primary  int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primary); err != nil {
			continue
		}
		if name == "parent_id" {
			return true
		}
	}
	return false
}

// kiloVisibleSessionExists applies the same durable visibility boundary used
// by discovery, while deliberately allowing an old but otherwise valid root
// session to remain addressable from an existing direct link.
func kiloVisibleSessionExists(db *sql.DB, sessionID string) (bool, error) {
	query := `
		SELECT 1
		FROM session
		WHERE id = ?
		  AND time_archived IS NULL`
	if kiloSessionHasParentID(db) {
		query += `
		  AND parent_id IS NULL`
	}
	query += `
		LIMIT 1`

	var exists int
	err := db.QueryRow(query, sessionID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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

// FetchHistory loads the last N visible turns and execution errors from kilo.db.
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

	exists, err := kiloVisibleSessionExists(db, sessionID)
	if err != nil {
		return nil, fmt.Errorf("check kilo session: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("kilo session not found: %s", sessionID)
	}

	rows, err := db.Query(`
		SELECT id, role, content, message_type, created_at
		FROM (
			SELECT
				p.id AS id,
				COALESCE(json_extract(m.data, '$.role'), '') AS role,
				COALESCE(json_extract(p.data, '$.text'), '') AS content,
				'text' AS message_type,
				p.time_created AS created_at
			FROM part p
			JOIN message m ON m.id = p.message_id
			WHERE p.session_id = ?
			  AND json_extract(p.data, '$.type') = 'text'
			  AND COALESCE(json_extract(p.data, '$.text'), '') != ''
			  AND COALESCE(json_extract(p.data, '$.ignored'), 0) = 0
			  AND json_extract(m.data, '$.role') IN ('user', 'assistant')

			UNION ALL

			SELECT
				m.id AS id,
				'system' AS role,
				m.data AS content,
				'error' AS message_type,
				m.time_updated AS created_at
			FROM message m
			WHERE m.session_id = ?
			  AND json_valid(m.data)
			  AND json_type(m.data, '$.error') IS NOT NULL
		)
		ORDER BY created_at DESC
		LIMIT ?
	`, sessionID, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("kilo history: %w", err)
	}
	defer rows.Close()

	var rev []*HistoryMessage
	for rows.Next() {
		var id, role, content, messageType string
		var tsMs int64
		if err := rows.Scan(&id, &role, &content, &messageType, &tsMs); err != nil {
			continue
		}
		if messageType == "error" {
			content = kiloStoredErrorText(content)
		}
		content = strings.TrimSpace(content)
		if content == "" ||
			(role != "user" && role != "assistant" && role != "system") {
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
			Type:      messageType,
			Timestamp: ts,
		})
	}
	// reverse DESC → ASC
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, rows.Err()
}

func kiloStoredErrorText(raw string) string {
	var record struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &record); err != nil || len(record.Error) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(record.Error, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return ""
		}
		return "Kilo execution failed: " + text
	}

	var detail struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(record.Error, &detail); err != nil {
		return ""
	}
	name := strings.TrimSpace(detail.Name)
	message := strings.TrimSpace(detail.Data.Message)
	if message == "" {
		message = strings.TrimSpace(detail.Message)
	}
	switch {
	case name != "" && message != "":
		return fmt.Sprintf("Kilo execution failed (%s): %s", name, message)
	case message != "":
		return "Kilo execution failed: " + message
	case name != "":
		return "Kilo execution failed: " + name
	default:
		return ""
	}
}

func (a *KiloAdapter) latestExecutionError(
	sessionID string,
	startedAt int64,
) (messageID, content string) {
	db, err := a.openRO()
	if err != nil {
		return "", ""
	}
	defer db.Close()

	var raw string
	err = db.QueryRow(`
		SELECT id, data
		FROM message
		WHERE session_id = ?
		  AND time_created >= ?
		  AND json_valid(data)
		  AND json_type(data, '$.error') IS NOT NULL
		ORDER BY time_updated DESC
		LIMIT 1
	`, sessionID, startedAt).Scan(&messageID, &raw)
	if err != nil {
		return "", ""
	}
	return messageID, kiloStoredErrorText(raw)
}

// ensure implements ClosableAdapter
var _ ClosableAdapter = (*KiloAdapter)(nil)
var _ Adapter = (*KiloAdapter)(nil)
var _ OutputAdapter = (*KiloAdapter)(nil)
var _ Adapter = (*ClaudeCodeAdapter)(nil)
var _ Adapter = (*CodexAdapter)(nil)

// SetOutputSink normalizes both Kilo CLI JSON and the progressive DB stream.
func (a *KiloAdapter) SetOutputSink(sink OutputSink) {
	if a.commander == nil {
		return
	}
	if sink == nil {
		a.commander.OnAgentOutput = nil
		a.commander.OnStreamStart = nil
		a.commander.OnStreamEnd = nil
		a.clearRuns()
		return
	}

	a.commander.OnAgentOutput = func(
		sessionID string,
		runNumber uint64,
		msgType string,
		content string,
		msgID string,
	) {
		key := kiloRunKey{sessionID: sessionID, runNumber: runNumber}
		value, ok := a.runs.Load(key)
		if !ok {
			return
		}
		state, ok := value.(*kiloRunState)
		if !ok {
			return
		}
		if msgType == "error" {
			state.setPending(&pendingKiloError{
				messageID: msgID,
				content:   content,
			})
			return
		}
		sink(OutputEvent{
			SessionID: sessionID,
			AgentType: AgentKilo,
			Type:      msgType,
			Content:   content,
			MessageID: msgID,
		})
	}
	a.commander.OnStreamStart = func(
		sessionID string,
		runNumber uint64,
		startedAt int64,
	) {
		key := kiloRunKey{sessionID: sessionID, runNumber: runNumber}
		state := &kiloRunState{startedAt: startedAt}
		state.stop = a.StartDBStream(sessionID, func(sid, partID, msgType, content string) {
			sink(OutputEvent{
				SessionID: sid,
				AgentType: AgentKilo,
				Type:      msgType,
				Content:   content,
				MessageID: partID,
			})
		})
		a.runs.Store(key, state)
		a.stopOtherRunStreams(sessionID, runNumber)
	}
	a.commander.OnStreamEnd = func(
		sessionID string,
		runNumber uint64,
		exitCode int,
	) {
		key := kiloRunKey{sessionID: sessionID, runNumber: runNumber}
		value, ok := a.runs.LoadAndDelete(key)
		if !ok {
			return
		}
		state, ok := value.(*kiloRunState)
		if !ok {
			return
		}
		state.stopStream()
		pending := state.takePending()
		if exitCode == 0 && pending == nil {
			return
		}

		messageID, content := a.latestExecutionError(sessionID, state.startedAt)
		if content == "" {
			if pending != nil {
				messageID = pending.messageID
				content = strings.TrimSpace(pending.content)
				if content != "" {
					content = "Kilo execution failed: " + content
				}
			}
		}
		if content == "" {
			content = fmt.Sprintf("Kilo process exited with code %d", exitCode)
		}
		if messageID == "" {
			messageID = fmt.Sprintf(
				"kilo_error_%d_%d",
				state.startedAt,
				runNumber,
			)
		}
		sink(OutputEvent{
			SessionID: sessionID,
			AgentType: AgentKilo,
			Type:      "error",
			Content:   content,
			MessageID: messageID,
		})
	}
}

func (s *kiloRunState) setPending(pending *pendingKiloError) {
	s.mu.Lock()
	s.pending = pending
	s.mu.Unlock()
}

func (s *kiloRunState) takePending() *pendingKiloError {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = nil
	return pending
}

func (s *kiloRunState) stopStream() {
	if s.stop != nil {
		s.stop()
	}
}

func (a *KiloAdapter) stopOtherRunStreams(sessionID string, runNumber uint64) {
	a.runs.Range(func(key, value any) bool {
		runKey, keyOK := key.(kiloRunKey)
		state, stateOK := value.(*kiloRunState)
		if keyOK &&
			stateOK &&
			runKey.sessionID == sessionID &&
			runKey.runNumber != runNumber {
			state.stopStream()
		}
		return true
	})
}

func (a *KiloAdapter) clearRuns() {
	a.runs.Range(func(key, value any) bool {
		if state, ok := value.(*kiloRunState); ok {
			state.stopStream()
		}
		a.runs.Delete(key)
		return true
	})
}
