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

	// Codex stores sessions in:
	// ~/.codex/sessions/<session-id>/rollout-*.jsonl
	if _, err := os.Stat(a.sessionsDir); os.IsNotExist(err) {
		return nil, nil // Codex not installed or no sessions
	}

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

		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk error: %w", err)
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

		role, _ := msg["role"].(string)
		eventType, _ := msg["type"].(string)

		if role == "assistant" {
			content := extractCodexContent(msg)
			if len(content) > 0 {
				lastMessage = truncate(content, 120)
			}
		}

		// Detect approval state
		if eventType == "approval_request" {
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
				ID:          fmt.Sprintf("codex_approval_%s_%d", filepath.Base(filepath.Dir(path)), msgCount),
				ToolName:    toolName,
				Description: desc,
			}
		}
		if eventType == "approval_response" {
			isWaiting = false
			pendingApproval = nil
		}

		if ts, ok := msg["timestamp"].(float64); ok {
			lastTime = time.Unix(int64(ts), 0)
		}
	}

	if msgCount == 0 {
		return nil, fmt.Errorf("empty rollout file")
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

	// Session ID is the parent directory name
	sessionID := filepath.Base(filepath.Dir(path))

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
				if text, ok := blockMap["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// ensure CodexAdapter implements ClosableAdapter
var _ ClosableAdapter = (*CodexAdapter)(nil)
// GetCommander returns the underlying CodexCommander for direct access.
func (a *CodexAdapter) GetCommander() *agentexec.CodexCommander {
	return a.commander
}