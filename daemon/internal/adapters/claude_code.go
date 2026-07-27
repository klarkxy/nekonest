package adapters

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nekonest/daemon/internal/agentexec"
)

// ClaudeCodeAdapter discovers and monitors Claude Code sessions.
type ClaudeCodeAdapter struct {
	projectsDir string
	watcherMu   sync.Mutex
	watchers    map[string]*fsnotify.Watcher
	lastPaths   map[string]string // sessionID -> jsonl path
	commander   *agentexec.ClaudeCommander
}

// NewClaudeCodeAdapter creates a new Claude Code adapter.
func NewClaudeCodeAdapter() *ClaudeCodeAdapter {
	home, _ := os.UserHomeDir()
	return &ClaudeCodeAdapter{
		projectsDir: filepath.Join(home, ".claude", "projects"),
		watchers:    make(map[string]*fsnotify.Watcher),
		lastPaths:   make(map[string]string),
		commander:   agentexec.NewClaudeCommander(),
	}
}

func (a *ClaudeCodeAdapter) Name() string { return "claude_code" }

// IsAvailable checks if the Claude CLI is installed.
func (a *ClaudeCodeAdapter) IsAvailable() bool {
	return a.commander.IsAvailable()
}

// Close releases all file watchers and stops running agent processes.
func (a *ClaudeCodeAdapter) Close() error {
	if a.commander != nil {
		a.commander.StopAll()
	}
	a.watcherMu.Lock()
	defer a.watcherMu.Unlock()

	for id, w := range a.watchers {
		if err := w.Close(); err != nil {
			log.Printf("[claude] error closing watcher for %s: %v", id, err)
		}
		delete(a.watchers, id)
	}
	log.Printf("[claude] closed all watchers")
	return nil
}

// Discover finds all Claude Code sessions by scanning the projects directory.
func (a *ClaudeCodeAdapter) Discover() ([]*SessionInfo, error) {
	var sessions []*SessionInfo

	// Claude Code stores sessions in:
	// ~/.claude/projects/<encoded-path>/*.jsonl
	if _, err := os.Stat(a.projectsDir); os.IsNotExist(err) {
		return nil, nil // Claude Code not installed or no sessions
	}

	err := filepath.Walk(a.projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}

		session, parseErr := a.parseSessionFile(path, info)
		if parseErr != nil {
			log.Printf("[claude] skip %s: %v", path, parseErr)
			return nil
		}

		// Include recent + historical (7 days) so phone can resume older chats
		if time.Since(session.LastActivity) > 7*24*time.Hour {
			return nil
		}

		a.watcherMu.Lock()
		a.lastPaths[session.ID] = session.SessionPath
		a.watcherMu.Unlock()
		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk error: %w", err)
	}

	return sessions, nil
}

// FetchHistory imports the last N user/assistant turns from the session JSONL.
func (a *ClaudeCodeAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	limit = clampHistoryLimit(limit)
	path := a.resolveSessionPath(sessionID)
	if path == "" {
		return nil, fmt.Errorf("claude session not found: %s", sessionID)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []*HistoryMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		role := ""
		if r, ok := msg["type"].(string); ok && (r == "user" || r == "assistant") {
			role = r
		}
		if nested, ok := msg["message"].(map[string]interface{}); ok {
			if r, ok := nested["role"].(string); ok && (r == "user" || r == "assistant") {
				role = r
			}
		}
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(extractContent(msg))
		if content == "" {
			continue
		}
		// Skip meta local-command caveats
		if strings.Contains(content, "<local-command-caveat>") {
			continue
		}
		ts := time.Now()
		if t, ok := parseMessageTime(msg["timestamp"]); ok {
			ts = t
		}
		id, _ := msg["uuid"].(string)
		if id == "" {
			id = fmt.Sprintf("claude_%s_%d", sessionID, len(out))
		}
		out = append(out, &HistoryMessage{
			ID:        id,
			Role:      role,
			Content:   truncateRunes(content, 4000),
			Type:      "text",
			Timestamp: ts.Unix(),
		})
	}
	return takeLastHistory(out, limit), sc.Err()
}

func (a *ClaudeCodeAdapter) resolveSessionPath(sessionID string) string {
	a.watcherMu.Lock()
	p := a.lastPaths[sessionID]
	a.watcherMu.Unlock()
	if p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fallback walk
	var found string
	_ = filepath.Walk(a.projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.TrimSuffix(info.Name(), ".jsonl") == sessionID {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func (a *ClaudeCodeAdapter) parseSessionFile(path string, info os.FileInfo) (*SessionInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var lastAssistantMsg string
	var lastTime time.Time
	var msgCount int
	var isWaiting bool
	var pendingApproval *ApprovalInfo
	var projectDir string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		msgCount++

		if projectDir == "" {
			if cwd, ok := msg["cwd"].(string); ok && cwd != "" {
				projectDir = cwd
			}
		}

		// Claude JSONL often nests role/content under message.{role,content}
		role, _ := msg["role"].(string)
		msgType, _ := msg["type"].(string)
		if nested, ok := msg["message"].(map[string]interface{}); ok {
			if r, ok := nested["role"].(string); ok && r != "" {
				role = r
			}
			// Prefer nested for content/tool detection
			if role == "" {
				if r, _ := nested["role"].(string); r != "" {
					role = r
				}
			}
		}
		contentSrc := msg
		if nested, ok := msg["message"].(map[string]interface{}); ok {
			if _, has := nested["content"]; has {
				contentSrc = nested
			}
		}

		if role == "assistant" || msgType == "assistant" {
			content := extractContent(contentSrc)
			if content == "" {
				content = extractContent(msg)
			}
			if len(content) > 0 {
				lastAssistantMsg = truncate(content, 120)
			}
		}

		// Detect tool_use for approval detection
		if msgType == "tool_use" || hasToolUse(contentSrc) || hasToolUse(msg) {
			toolName := extractToolName(contentSrc)
			if toolName == "" {
				toolName = extractToolName(msg)
			}
			if toolName != "" && !isReadOnlyTool(toolName) {
				isWaiting = true
				desc := extractToolDescription(contentSrc)
				if desc == "" || desc == "Tool call" {
					desc = extractToolDescription(msg)
				}
				pendingApproval = &ApprovalInfo{
					ID:          fmt.Sprintf("approval_%s_%d", filepath.Base(path), msgCount),
					ToolName:    toolName,
					Description: desc,
				}
			}
		}

		// Tool result clears waiting state (top-level or nested message.content blocks).
		if msgType == "tool_result" || role == "tool" || hasToolResult(contentSrc) || hasToolResult(msg) {
			isWaiting = false
			pendingApproval = nil
		}

		// Update timestamp (unix seconds/ms or RFC3339 string)
		if t, ok := parseMessageTime(msg["timestamp"]); ok && t.After(lastTime) {
			lastTime = t
		} else if t, ok := parseMessageTime(msg["created"]); ok && t.After(lastTime) {
			lastTime = t
		}
	}

	if msgCount == 0 {
		return nil, fmt.Errorf("empty session file")
	}

	// If file was modified recently, consider it running
	modTime := info.ModTime()
	if modTime.After(lastTime) {
		lastTime = modTime
	}

	status := StatusIdle
	if isWaiting {
		status = StatusWaitingApproval
	} else if time.Since(lastTime) < 60*time.Second {
		status = StatusRunning
	}

	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if projectDir == "" {
		projectDir = decodeClaudeProjectDir(filepath.Base(filepath.Dir(path)))
	}

	return &SessionInfo{
		ID:              sessionID,
		AgentType:       AgentClaudeCode,
		Status:          status,
		Summary:         lastAssistantMsg,
		LastActivity:    lastTime,
		SessionPath:     path,
		ProjectDir:      projectDir,
		PendingApproval: pendingApproval,
	}, nil
}

// decodeClaudeProjectDir best-effort reverses Claude's project folder encoding
// (drive/path separators become '-').
func decodeClaudeProjectDir(encoded string) string {
	if encoded == "" || encoded == "." {
		return ""
	}
	// Common Windows form: C--Users-admin-... or D--nekonest
	s := encoded
	if len(s) >= 3 && s[1] == '-' && s[2] == '-' {
		// C--foo -> C:\foo  (remaining dashes stay as path seps approximately)
		drive := string(s[0])
		rest := s[3:]
		rest = strings.ReplaceAll(rest, "-", string(filepath.Separator))
		return drive + ":" + string(filepath.Separator) + rest
	}
	return strings.ReplaceAll(s, "-", string(filepath.Separator))
}

// Watch monitors a session file for changes.
func (a *ClaudeCodeAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
	sessions, err := a.Discover()
	if err != nil {
		return nil, err
	}

	var targetPath string
	for _, s := range sessions {
		if s.ID == sessionID {
			targetPath = s.SessionPath
			break
		}
	}
	if targetPath == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(filepath.Dir(targetPath)); err != nil {
		watcher.Close()
		return nil, err
	}

	a.watcherMu.Lock()
	a.watchers[sessionID] = watcher
	a.watcherMu.Unlock()

	ch := make(chan *SessionInfo, 10)
	go func() {
		defer watcher.Close()
		defer close(ch)

		// Debounce timer to avoid excessive re-parsing
		var debounce <-chan time.Time

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name == targetPath && event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					debounce = time.After(300 * time.Millisecond)
				}
			case <-func() <-chan time.Time {
				if debounce == nil {
					return make(chan time.Time) // never fires
				}
				return debounce
			}():
				info, err := os.Stat(targetPath)
				if err != nil {
					continue
				}
				session, err := a.parseSessionFile(targetPath, info)
				if err != nil {
					continue
				}
				ch <- session

			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return ch, nil
}

// SendPrompt resumes Claude Code with a new prompt.
func (a *ClaudeCodeAdapter) SendPrompt(sessionID string, prompt string) error {
	if !a.commander.IsAvailable() {
		return fmt.Errorf("claude CLI not found in PATH")
	}
	return a.commander.SendPrompt(sessionID, prompt)
}

// Approve approves a pending tool call.
func (a *ClaudeCodeAdapter) Approve(sessionID string, approvalID string) error {
	return a.commander.Approve(sessionID, approvalID)
}

// Deny denies a pending tool call.
func (a *ClaudeCodeAdapter) Deny(sessionID string, approvalID string) error {
	return a.commander.Deny(sessionID, approvalID)
}

// Interrupt interrupts a running session.
func (a *ClaudeCodeAdapter) Interrupt(sessionID string) error {
	return a.commander.Interrupt(sessionID)
}

// --- Helper functions ---

func extractContent(msg map[string]interface{}) string {
	if content, ok := msg["content"]; ok {
		if s := contentToText(content); s != "" {
			return s
		}
	}
	// Claude JSONL often nests under message.{role,content}
	if nested, ok := msg["message"].(map[string]interface{}); ok {
		if content, ok := nested["content"]; ok {
			return contentToText(content)
		}
	}
	return ""
}

func contentToText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, _ := blockMap["type"].(string); blockType == "text" || blockType == "" {
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func hasToolUse(msg map[string]interface{}) bool {
	return contentHasBlockType(msg, "tool_use")
}

func hasToolResult(msg map[string]interface{}) bool {
	if contentHasBlockType(msg, "tool_result") {
		return true
	}
	// Nested message.content
	if nested, ok := msg["message"].(map[string]interface{}); ok {
		return contentHasBlockType(nested, "tool_result")
	}
	return false
}

func contentHasBlockType(msg map[string]interface{}, want string) bool {
	content, ok := msg["content"]
	if !ok {
		return false
	}
	blocks, ok := content.([]interface{})
	if !ok {
		return false
	}
	for _, block := range blocks {
		if blockMap, ok := block.(map[string]interface{}); ok {
			if blockType, _ := blockMap["type"].(string); blockType == want {
				return true
			}
		}
	}
	return false
}

func extractToolName(msg map[string]interface{}) string {
	content, ok := msg["content"]
	if !ok {
		return ""
	}
	blocks, ok := content.([]interface{})
	if !ok {
		return ""
	}
	for _, block := range blocks {
		if blockMap, ok := block.(map[string]interface{}); ok {
			if blockType, _ := blockMap["type"].(string); blockType == "tool_use" {
				if name, ok := blockMap["name"].(string); ok {
					return name
				}
			}
		}
	}
	return ""
}

func extractToolDescription(msg map[string]interface{}) string {
	toolName := extractToolName(msg)
	if toolName == "" {
		return "Tool call"
	}

	content, _ := msg["content"].([]interface{})
	for _, block := range content {
		if blockMap, ok := block.(map[string]interface{}); ok {
			if blockType, _ := blockMap["type"].(string); blockType == "tool_use" {
				if input, ok := blockMap["input"].(map[string]interface{}); ok {
					switch toolName {
					case "Bash", "bash":
						if cmd, ok := input["command"].(string); ok {
							return fmt.Sprintf("Run: %s", truncate(cmd, 80))
						}
					case "Write", "write":
						if path, ok := input["file_path"].(string); ok {
							return fmt.Sprintf("Write: %s", filepath.Base(path))
						}
					case "Read", "read":
						if path, ok := input["file_path"].(string); ok {
							return fmt.Sprintf("Read: %s", filepath.Base(path))
						}
					case "Edit", "edit":
						if path, ok := input["file_path"].(string); ok {
							return fmt.Sprintf("Edit: %s", filepath.Base(path))
						}
					default:
						return fmt.Sprintf("Call %s", toolName)
					}
				}
			}
		}
	}
	return toolName
}

// isReadOnlyTool returns true for tools that don't need approval.
func isReadOnlyTool(name string) bool {
	readOnly := map[string]bool{
		"Read": true, "read": true,
		"Glob": true, "glob": true,
		"Grep": true, "grep": true,
		"ListDir": true, "list_dir": true,
	}
	return readOnly[name]
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func parseMessageTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case float64:
		// Heuristic: ms vs seconds
		if t > 1e12 {
			return time.UnixMilli(int64(t)), true
		}
		if t > 0 {
			return time.Unix(int64(t), 0), true
		}
	case string:
		if t == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed, true
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// ensure ClaudeCodeAdapter implements ClosableAdapter
var _ ClosableAdapter = (*ClaudeCodeAdapter)(nil)

// GetCommander returns the underlying ClaudeCommander for direct access.
func (a *ClaudeCodeAdapter) GetCommander() *agentexec.ClaudeCommander {
	return a.commander
}
