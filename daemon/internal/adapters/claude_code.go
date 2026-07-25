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
	commander   *agentexec.ClaudeCommander
}

// NewClaudeCodeAdapter creates a new Claude Code adapter.
func NewClaudeCodeAdapter() *ClaudeCodeAdapter {
	home, _ := os.UserHomeDir()
	return &ClaudeCodeAdapter{
		projectsDir: filepath.Join(home, ".claude", "projects"),
		watchers:    make(map[string]*fsnotify.Watcher),
		commander:   agentexec.NewClaudeCommander(),
	}
}

func (a *ClaudeCodeAdapter) Name() string { return "claude_code" }

// IsAvailable checks if the Claude CLI is installed.
func (a *ClaudeCodeAdapter) IsAvailable() bool {
	return a.commander.IsAvailable()
}

// Close releases all file watchers.
func (a *ClaudeCodeAdapter) Close() error {
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

		// Only include sessions from the last 24 hours
		if time.Since(session.LastActivity) > 24*time.Hour {
			return nil
		}

		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk error: %w", err)
	}

	return sessions, nil
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

		// Extract content for summary
		role, _ := msg["role"].(string)
		msgType, _ := msg["type"].(string)

		if role == "assistant" {
			content := extractContent(msg)
			if len(content) > 0 {
				lastAssistantMsg = truncate(content, 120)
			}
		}

		// Detect tool_use for approval detection
		if msgType == "tool_use" || role == "assistant" && hasToolUse(msg) {
			toolName := extractToolName(msg)
			if toolName != "" && !isReadOnlyTool(toolName) {
				isWaiting = true
				pendingApproval = &ApprovalInfo{
					ID:          fmt.Sprintf("approval_%s_%d", filepath.Base(path), msgCount),
					ToolName:    toolName,
					Description: extractToolDescription(msg),
				}
			}
		}

		// Tool result clears waiting state
		if msgType == "tool_result" || role == "tool" {
			isWaiting = false
			pendingApproval = nil
		}

		// Update timestamp
		if ts, ok := msg["timestamp"].(float64); ok {
			lastTime = time.Unix(int64(ts), 0)
		} else if ts, ok := msg["created"].(float64); ok {
			lastTime = time.Unix(int64(ts), 0)
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

	return &SessionInfo{
		ID:              sessionID,
		AgentType:       AgentClaudeCode,
		Status:          status,
		Summary:         lastAssistantMsg,
		LastActivity:    lastTime,
		SessionPath:     path,
		PendingApproval: pendingApproval,
	}, nil
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
	content, ok := msg["content"]
	if !ok {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, _ := blockMap["type"].(string); blockType == "text" {
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
			if blockType, _ := blockMap["type"].(string); blockType == "tool_use" {
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

// ensure ClaudeCodeAdapter implements ClosableAdapter
var _ ClosableAdapter = (*ClaudeCodeAdapter)(nil)
// GetCommander returns the underlying ClaudeCommander for direct access.
func (a *ClaudeCodeAdapter) GetCommander() *agentexec.ClaudeCommander {
	return a.commander
}