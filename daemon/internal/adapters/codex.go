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

// CodexAdapter discovers and monitors Codex sessions.
type CodexAdapter struct {
	sessionsDir string
	watcherMu   sync.Mutex
	watchers    map[string]*fsnotify.Watcher
	commander   *agentexec.CodexCommander
}

// NewCodexAdapter creates a new Codex adapter.
func NewCodexAdapter() *CodexAdapter {
	home, _ := os.UserHomeDir()
	return &CodexAdapter{
		sessionsDir: filepath.Join(home, ".codex", "sessions"),
		watchers:    make(map[string]*fsnotify.Watcher),
		commander:   agentexec.NewCodexCommander(),
	}
}

func (a *CodexAdapter) Name() string { return "codex" }

// IsAvailable checks if the Codex CLI is installed.
func (a *CodexAdapter) IsAvailable() bool {
	return a.commander.IsAvailable()
}

// Close releases all file watchers.
func (a *CodexAdapter) Close() error {
	a.watcherMu.Lock()
	defer a.watcherMu.Unlock()

	for id, w := range a.watchers {
		if err := w.Close(); err != nil {
			log.Printf("[codex] error closing watcher for %s: %v", id, err)
		}
		delete(a.watchers, id)
	}
	log.Printf("[codex] closed all watchers")
	return nil
}

// Discover finds all Codex sessions.
func (a *CodexAdapter) Discover() ([]*SessionInfo, error) {
	var sessions []*SessionInfo

	// Current Codex layout:
	// ~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl
	// Older layout (still accepted):
	// ~/.codex/sessions/<session-id>/rollout-*.jsonl
	if _, err := os.Stat(a.sessionsDir); os.IsNotExist(err) {
		return nil, nil // Codex not installed or no sessions
	}

	byID := make(map[string]*SessionInfo)
	err := filepath.Walk(a.sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasPrefix(info.Name(), "rollout-") || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}

		session, parseErr := a.parseRolloutFile(path, info)
		if parseErr != nil {
			log.Printf("[codex] skip %s: %v", path, parseErr)
			return nil
		}

		// Only include recent sessions
		if time.Since(session.LastActivity) > 24*time.Hour {
			return nil
		}

		// One logical session may have multiple rollout files; keep the newest.
		if prev, ok := byID[session.ID]; !ok || session.LastActivity.After(prev.LastActivity) {
			byID[session.ID] = session
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk error: %w", err)
	}

	for _, s := range byID {
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func (a *CodexAdapter) parseRolloutFile(path string, info os.FileInfo) (*SessionInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var lastMessage string
	var lastTime time.Time
	var msgCount int
	var isWaiting bool
	var pendingApproval *ApprovalInfo
	sessionID := codexSessionIDFromFilename(filepath.Base(path))

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
		if t := parseCodexTimestamp(msg["timestamp"]); !t.IsZero() {
			lastTime = t
		}

		eventType, _ := msg["type"].(string)
		payload, _ := msg["payload"].(map[string]interface{})

		// Modern Codex: session_meta carries the real thread/session ids.
		if eventType == "session_meta" && payload != nil {
			if id, _ := payload["id"].(string); id != "" {
				sessionID = id
			} else if id, _ := payload["session_id"].(string); id != "" {
				sessionID = id
			}
			if t := parseCodexTimestamp(payload["timestamp"]); !t.IsZero() {
				lastTime = t
			}
		}

		// Modern: event_msg / response_item nested under payload.
		if payload != nil {
			pType, _ := payload["type"].(string)
			switch {
			case pType == "agent_message":
				if m, _ := payload["message"].(string); m != "" {
					lastMessage = truncate(m, 120)
				}
			case pType == "task_complete":
				if m, _ := payload["last_agent_message"].(string); m != "" {
					lastMessage = truncate(m, 120)
				}
			case pType == "message":
				role, _ := payload["role"].(string)
				if role == "assistant" {
					if content := extractCodexContent(payload); content != "" {
						lastMessage = truncate(content, 120)
					}
				}
			case pType == "approval_request" || eventType == "approval_request":
				isWaiting = true
				toolName, _ := payload["tool_name"].(string)
				desc, _ := payload["description"].(string)
				if toolName == "" {
					toolName = "unknown_tool"
				}
				if desc == "" {
					desc = "Codex tool call"
				}
				pendingApproval = &ApprovalInfo{
					ID:          fmt.Sprintf("codex_approval_%s_%d", sessionID, msgCount),
					ToolName:    toolName,
					Description: desc,
				}
			case pType == "approval_response" || eventType == "approval_response":
				isWaiting = false
				pendingApproval = nil
			}
		}

		// Legacy flat rollout format.
		role, _ := msg["role"].(string)
		if role == "assistant" {
			if content := extractCodexContent(msg); content != "" {
				lastMessage = truncate(content, 120)
			}
		}
		if eventType == "approval_request" && pendingApproval == nil {
			isWaiting = true
			toolName, _ := msg["tool_name"].(string)
			desc, _ := msg["description"].(string)
			if toolName == "" {
				toolName = "unknown_tool"
			}
			if desc == "" {
				desc = "Codex tool call"
			}
			pendingApproval = &ApprovalInfo{
				ID:          fmt.Sprintf("codex_approval_%s_%d", sessionID, msgCount),
				ToolName:    toolName,
				Description: desc,
			}
		}
		if eventType == "approval_response" {
			isWaiting = false
			pendingApproval = nil
		}
	}

	if msgCount == 0 {
		return nil, fmt.Errorf("empty rollout file")
	}
	if sessionID == "" {
		// Last-resort fallback for very old layouts: parent directory name.
		sessionID = filepath.Base(filepath.Dir(path))
	}
	if sessionID == "" || sessionID == "." || sessionID == string(filepath.Separator) {
		return nil, fmt.Errorf("could not determine session id for %s", path)
	}

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

	return &SessionInfo{
		ID:              sessionID,
		AgentType:       AgentCodex,
		Status:          status,
		Summary:         lastMessage,
		LastActivity:    lastTime,
		SessionPath:     path,
		PendingApproval: pendingApproval,
	}, nil
}

// codexSessionIDFromFilename extracts the UUID suffix from
// rollout-2026-07-25T18-02-04-019f98b9-787f-7573-9d8e-9d308b04aaf6.jsonl
func codexSessionIDFromFilename(name string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	base = strings.TrimPrefix(base, "rollout-")
	// UUID is the last 5 hyphen-separated groups at the end.
	parts := strings.Split(base, "-")
	if len(parts) >= 5 {
		cand := strings.Join(parts[len(parts)-5:], "-")
		if looksLikeUUID(cand) {
			return cand
		}
	}
	return ""
}

func looksLikeUUID(s string) bool {
	if len(s) < 32 || len(s) > 40 {
		return false
	}
	hyphens := 0
	for _, r := range s {
		switch {
		case r == '-':
			hyphens++
		case (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'):
		default:
			return false
		}
	}
	return hyphens == 4
}

func parseCodexTimestamp(v interface{}) time.Time {
	switch t := v.(type) {
	case float64:
		// Seconds or milliseconds
		if t > 1e12 {
			return time.UnixMilli(int64(t))
		}
		return time.Unix(int64(t), 0)
	case string:
		if t == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// Watch monitors a Codex session for changes.
func (a *CodexAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
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
					return make(chan time.Time)
				}
				return debounce
			}():
				info, err := os.Stat(targetPath)
				if err != nil {
					continue
				}
				session, err := a.parseRolloutFile(targetPath, info)
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

func (a *CodexAdapter) SendPrompt(sessionID string, prompt string) error {
	if !a.commander.IsAvailable() {
		return fmt.Errorf("codex CLI not found in PATH")
	}
	return a.commander.SendPrompt(sessionID, prompt)
}

func (a *CodexAdapter) Approve(sessionID string, approvalID string) error {
	return a.commander.Approve(sessionID, approvalID)
}

func (a *CodexAdapter) Deny(sessionID string, approvalID string) error {
	return a.commander.Deny(sessionID, approvalID)
}

func (a *CodexAdapter) Interrupt(sessionID string) error {
	return a.commander.Interrupt(sessionID)
}

// --- Codex-specific helpers ---

func extractCodexContent(msg map[string]interface{}) string {
	if content, ok := msg["content"]; ok {
		switch v := content.(type) {
		case string:
			return v
		case []interface{}:
			var parts []string
			for _, block := range v {
				blockMap, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if text, ok := blockMap["text"].(string); ok && text != "" {
					parts = append(parts, text)
					continue
				}
				// Modern Codex uses input_text / output_text blocks.
				if text, ok := blockMap["text"].(string); ok {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, " ")
		}
	}
	if m, ok := msg["message"].(string); ok {
		return m
	}
	return ""
}

// ensure CodexAdapter implements ClosableAdapter
var _ ClosableAdapter = (*CodexAdapter)(nil)
// GetCommander returns the underlying CodexCommander for direct access.
func (a *CodexAdapter) GetCommander() *agentexec.CodexCommander {
	return a.commander
}