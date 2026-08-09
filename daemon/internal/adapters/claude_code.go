package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

var errClaudeSubagentTranscript = errors.New("claude subagent transcript")

// ClaudeCodeAdapter discovers and monitors Claude Code sessions.
type ClaudeCodeAdapter struct {
	projectsDir    string
	watcherMu      sync.Mutex
	watchers       map[string]*fsnotify.Watcher
	lastPaths      map[string]string // sessionID -> jsonl path
	discoveryCache *fileDiscoveryCache[*SessionInfo]
	attentionCache *fileDiscoveryCache[*SessionInfo]
	commander      *agentexec.ClaudeCommander
	turns          turnTracker
}

// NewClaudeCodeAdapter creates a new Claude Code adapter.
func NewClaudeCodeAdapter() *ClaudeCodeAdapter {
	home, _ := os.UserHomeDir()
	a := &ClaudeCodeAdapter{
		projectsDir:    filepath.Join(home, ".claude", "projects"),
		watchers:       make(map[string]*fsnotify.Watcher),
		lastPaths:      make(map[string]string),
		discoveryCache: newFileDiscoveryCache[*SessionInfo](),
		attentionCache: newFileDiscoveryCache[*SessionInfo](),
		commander:      agentexec.NewClaudeCommander(),
		turns:          newTurnTracker(AgentClaudeCode),
	}
	a.commander.OnTurnEnd = func(sessionID string, exitCode int, interrupted bool) {
		a.turns.finish(sessionID, exitCode, interrupted)
	}
	return a
}

func (a *ClaudeCodeAdapter) Name() string { return "claude_code" }

// IsAvailable checks if the Claude CLI is installed.
func (a *ClaudeCodeAdapter) IsAvailable() bool {
	return a.commander.IsAvailable()
}

func (a *ClaudeCodeAdapter) ProbeThreadStart(ctx context.Context) ThreadStartCapability {
	if a.commander == nil {
		return ThreadStartCapability{Reason: "Claude commander is unavailable"}
	}
	if err := a.commander.ProbeThreadStart(ctx); err != nil {
		return ThreadStartCapability{Reason: err.Error()}
	}
	return ThreadStartCapability{Available: true, ControlPath: "cli", AttachmentMode: AttachUnsupported}
}

func (a *ClaudeCodeAdapter) StartNativeThread(ctx context.Context, request ThreadStartRequest) (ThreadStartResult, error) {
	if a.commander == nil {
		return ThreadStartResult{}, fmt.Errorf("Claude commander is unavailable")
	}
	sessionID, created, promptAccepted, err := a.commander.StartThread(ctx, request.ProjectDir, request.Prompt)
	return ThreadStartResult{SessionID: sessionID, Created: created, PromptAccepted: promptAccepted}, err
}

// Close releases all file watchers and stops running agent processes.
func (a *ClaudeCodeAdapter) Close() error {
	a.turns.detachAll()
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
	now := time.Now()
	cutoff := recentSessionCutoff(now)
	keep := make(map[string]struct{})

	// Claude Code stores sessions in:
	// ~/.claude/projects/<encoded-path>/*.jsonl
	if _, err := os.Stat(a.projectsDir); os.IsNotExist(err) {
		return nil, nil // Claude Code not installed or no sessions
	}

	err := filepath.Walk(a.projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			if strings.EqualFold(info.Name(), "subagents") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		oldFile := info.ModTime().Before(cutoff)
		var session *SessionInfo
		var parseErr error
		if oldFile {
			cached, cachedErr, ok := a.discoveryCache.peek(path, info)
			if ok {
				if cachedErr != nil || cached == nil {
					return nil
				}
				session, parseErr = a.parseSessionFile(path, info)
			} else {
				session, parseErr = a.probeOldSessionAttention(path, info)
			}
		} else {
			keep[normalizedDiscoveryPath(path)] = struct{}{}
			session, parseErr = a.parseSessionFile(path, info)
		}

		if parseErr != nil {
			if errors.Is(parseErr, errClaudeSubagentTranscript) {
				return nil
			}
			log.Printf("[claude] skip %s: %v", path, parseErr)
			return nil
		}

		if !sessionIsVisible(session, now) {
			return nil
		}
		if oldFile {
			keep[normalizedDiscoveryPath(path)] = struct{}{}
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
	a.discoveryCache.prune(keep)
	a.attentionCache.pruneMissing()

	return sessions, nil
}

func (a *ClaudeCodeAdapter) probeOldSessionAttention(path string, info os.FileInfo) (*SessionInfo, error) {
	cached, err := a.attentionCache.load(path, info, func() (*SessionInfo, error) {
		probe, probeErr := readJSONLAttentionProbe(path, info)
		if probeErr != nil {
			return nil, probeErr
		}
		return parseClaudeAttentionProbe(path, info, probe)
	})
	if err != nil || cached == nil {
		return nil, err
	}
	session := *cached
	applyClaudeDynamicStatus(&session, a.commander)
	return &session, nil
}

func parseClaudeAttentionProbe(path string, info os.FileInfo, probe jsonlAttentionProbe) (*SessionInfo, error) {
	projectDir := ""
	for index, line := range probe.head {
		var msg map[string]interface{}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if index == 0 && isClaudeSubagentRecord(msg) {
			return nil, errClaudeSubagentTranscript
		}
		if cwd, _ := msg["cwd"].(string); cwd != "" {
			projectDir = cwd
			break
		}
	}
	if projectDir == "" {
		projectDir = decodeClaudeProjectDir(filepath.Base(filepath.Dir(path)))
	}

	status := StatusIdle
	lastAssistantMessage := ""
	lastTime := info.ModTime()
	lines := probe.tail
	if probe.whole {
		lines = probe.head
	}
	for _, line := range lines {
		var msg map[string]interface{}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		role, _ := msg["role"].(string)
		msgType, _ := msg["type"].(string)
		contentSource := msg
		if nested, ok := msg["message"].(map[string]interface{}); ok {
			if nestedRole, _ := nested["role"].(string); nestedRole != "" {
				role = nestedRole
			}
			if _, hasContent := nested["content"]; hasContent {
				contentSource = nested
			}
		}
		if role == "assistant" || msgType == "assistant" {
			if content := extractContent(contentSource); content != "" {
				lastAssistantMessage = truncate(content, 120)
			}
		}
		if timestamp, ok := parseMessageTime(msg["timestamp"]); ok && timestamp.After(lastTime) {
			lastTime = timestamp
		} else if created, ok := parseMessageTime(msg["created"]); ok && created.After(lastTime) {
			lastTime = created
		}
	}

	return &SessionInfo{
		ID:        strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		AgentType: AgentClaudeCode, Status: status, Summary: lastAssistantMessage,
		LastActivity: lastTime, SessionPath: path, ProjectDir: projectDir,
	}, nil
}

// OwnsSession checks the Claude transcript store for an exact session ID.
func (a *ClaudeCodeAdapter) OwnsSession(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	return a.resolveSessionPath(sessionID) != ""
}

// FetchHistory imports the last N user/assistant turns from the session JSONL.
func (a *ClaudeCodeAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	limit = clampHistoryLimit(limit)
	if !a.OwnsSession(sessionID) {
		return nil, fmt.Errorf("claude session not found: %s", sessionID)
	}
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
		a.watcherMu.Lock()
		delete(a.lastPaths, sessionID)
		a.watcherMu.Unlock()
	}
	// Fallback walk validates the transcript marker rather than relying on the
	// filename alone. Old visible root sessions remain addressable by direct
	// link even after they age out of the discovery list.
	var found string
	_ = filepath.Walk(a.projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			if strings.EqualFold(info.Name(), "subagents") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".jsonl") ||
			strings.TrimSuffix(info.Name(), ".jsonl") != sessionID {
			return nil
		}
		session, parseErr := a.parseSessionFile(path, info)
		if parseErr == nil && session != nil && session.ID == sessionID {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		a.watcherMu.Lock()
		a.lastPaths[sessionID] = found
		a.watcherMu.Unlock()
	}
	return found
}

func (a *ClaudeCodeAdapter) parseSessionFile(path string, info os.FileInfo) (*SessionInfo, error) {
	cached, err := a.discoveryCache.load(path, info, func() (*SessionInfo, error) {
		return a.parseSessionFileUncached(path, info)
	})
	if err != nil || cached == nil {
		return nil, err
	}
	session := *cached
	applyClaudeDynamicStatus(&session, a.commander)
	return &session, nil
}

func applyClaudeDynamicStatus(session *SessionInfo, commander *agentexec.ClaudeCommander) {
	if session == nil {
		return
	}
	if commander != nil && commander.IsSessionRunning(session.ID) {
		session.Status = StatusRunning
	} else if time.Since(session.LastActivity) < time.Minute {
		session.Status = StatusRunning
	} else {
		session.Status = StatusIdle
	}
}

func (a *ClaudeCodeAdapter) parseSessionFileUncached(path string, info os.FileInfo) (*SessionInfo, error) {
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

		// Current Claude Code stores child-agent transcripts under a subagents
		// directory. This first-record check is a defensive fallback for a
		// future layout that keeps the same explicit sidechain metadata.
		if msgCount == 0 && isClaudeSubagentRecord(msg) {
			return nil, errClaudeSubagentTranscript
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
	if time.Since(lastTime) < 60*time.Second {
		status = StatusRunning
	}

	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if projectDir == "" {
		projectDir = decodeClaudeProjectDir(filepath.Base(filepath.Dir(path)))
	}

	return &SessionInfo{
		ID:           sessionID,
		AgentType:    AgentClaudeCode,
		Status:       status,
		Summary:      lastAssistantMsg,
		LastActivity: lastTime,
		SessionPath:  path,
		ProjectDir:   projectDir,
	}, nil
}

func isClaudeSubagentRecord(msg map[string]interface{}) bool {
	isSidechain, _ := msg["isSidechain"].(bool)
	agentID, _ := msg["agentId"].(string)
	return isSidechain && strings.TrimSpace(agentID) != ""
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
func (a *ClaudeCodeAdapter) SendPrompt(sessionID string, request PromptRequest) error {
	if !a.commander.IsAvailable() {
		return fmt.Errorf("claude CLI not found in PATH")
	}
	if !a.turns.begin(sessionID, request) {
		return agentexec.ErrSessionBusy
	}
	err := a.commander.SendPrompt(
		sessionID,
		request.Prompt,
		request.Attachments,
		request.OnComplete,
	)
	if err != nil {
		a.turns.abort(sessionID, request.Generation)
		return err
	}
	if !request.DeferAcceptance {
		a.turns.accepted(sessionID, request.Generation)
	}
	return nil
}

func (a *ClaudeCodeAdapter) AcknowledgePrompt(sessionID string, generation uint64) {
	a.turns.accepted(sessionID, generation)
}
func (a *ClaudeCodeAdapter) AbandonPrompt(sessionID string, generation uint64) {
	a.turns.abort(sessionID, generation)
}

func (a *ClaudeCodeAdapter) SetControlSink(sink func(ControlEvent)) { a.turns.setSink(sink) }

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
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if nested, ok := v["content"]; ok {
			return contentToText(nested)
		}
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
var _ OutputAdapter = (*ClaudeCodeAdapter)(nil)
var _ NativeThreadStarter = (*ClaudeCodeAdapter)(nil)

// GetCommander returns the underlying ClaudeCommander for direct access.
func (a *ClaudeCodeAdapter) GetCommander() *agentexec.ClaudeCommander {
	return a.commander
}

// SetOutputSink normalizes Claude commander output for the daemon registry.
func (a *ClaudeCodeAdapter) SetOutputSink(sink OutputSink) {
	if a.commander == nil {
		return
	}
	if sink == nil {
		a.commander.OnAgentOutput = nil
		return
	}
	a.commander.OnAgentOutput = func(sessionID, msgType, content string) {
		sink(OutputEvent{
			SessionID: sessionID,
			AgentType: AgentClaudeCode,
			Type:      msgType,
			Content:   content,
		})
	}
}
