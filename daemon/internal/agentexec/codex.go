package agentexec

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// CodexCommander handles Codex CLI interactions.
type CodexCommander struct {
	mu        sync.Mutex
	cliPath   string
	executors map[string]*AgentExecutor

	// lastAssistant dedupes identical assistant frames from multi-event JSON.
	lastAssistant   map[string]string
	lastAssistantAt map[string]int64

	// OnAgentOutput is called for each line of output from the agent.
	OnAgentOutput func(sessionID string, msgType string, content string)
}

// NewCodexCommander creates a new Codex commander.
func NewCodexCommander() *CodexCommander {
	return &CodexCommander{
		cliPath:         findCodexCLI(),
		executors:       make(map[string]*AgentExecutor),
		lastAssistant:   make(map[string]string),
		lastAssistantAt: make(map[string]int64),
	}
}

func findCodexCLI() string {
	appData := os.Getenv("APPDATA")
	// Prefer .exe over .cmd to avoid cmd.exe metacharacter re-parse of prompts.
	candidates := []string{
		filepath.Join(appData, "npm", "codex.exe"),
		filepath.Join(appData, "npm", "codex.cmd"),
	}
	for _, loc := range candidates {
		if st, err := os.Stat(loc); err == nil && !st.IsDir() {
			return loc
		}
	}
	if p, err := exec.LookPath("codex.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("codex"); err == nil {
		if runtime.GOOS == "windows" {
			ext := strings.ToLower(filepath.Ext(p))
			if ext == "" {
				if st, err := os.Stat(p + ".exe"); err == nil && !st.IsDir() {
					return p + ".exe"
				}
				if st, err := os.Stat(p + ".cmd"); err == nil && !st.IsDir() {
					return p + ".cmd"
				}
			}
		}
		return p
	}
	if p, err := exec.LookPath("codex.cmd"); err == nil {
		return p
	}
	return "codex"
}

// IsAvailable checks if Codex CLI is installed.
func (c *CodexCommander) IsAvailable() bool {
	if c.cliPath == "" {
		return false
	}
	if st, err := os.Stat(c.cliPath); err == nil && !st.IsDir() {
		return true
	}
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

// SendPrompt sends a prompt to a Codex session.
func (c *CodexCommander) SendPrompt(sessionID, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// exec resume puts the prompt on argv and closes stdin — never write stdin
	// again (that caused garbled/duplicate turns). If a run is still active, reject.
	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return fmt.Errorf("codex session %s is still running; wait for it to finish", sessionID)
	}

	// Non-interactive resume: `codex resume` requires a TTY.
	// Use: codex exec --json resume <sessionID> <prompt>
	args := []string{
		"exec",
		"--json",
		"resume",
		sessionID,
		prompt,
	}

	executor := NewAgentExecutor("codex", sessionID)

	// Wire up output callback
	executor.OnOutput = func(line string) {
		c.parseAndForwardOutput(sessionID, line)
	}

	executor.OnExit = func(exitCode int) {
		log.Printf("[codex] session %s process exited with code %d", sessionID, exitCode)
		c.mu.Lock()
		if cur, ok := c.executors[sessionID]; ok && cur == executor {
			delete(c.executors, sessionID)
		}
		c.mu.Unlock()
	}

	if err := executor.Start(c.cliPath, args, nil); err != nil {
		return fmt.Errorf("start codex: %w", err)
	}
	// Prompt is a positional arg on exec resume; close stdin.
	_ = executor.CloseStdin()

	c.executors[sessionID] = executor
	log.Printf("[codex] started process for session %s", sessionID)
	return nil
}

// parseAndForwardOutput parses Codex JSON output and calls OnAgentOutput.
// Codex rollout format: {"role": "assistant", "content": "..."}
// or {"type": "approval_request", "tool_name": "...", ...}
func (c *CodexCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil {
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Not JSON, forward as raw text
		c.OnAgentOutput(sessionID, "text", line)
		return
	}

	role, _ := msg["role"].(string)
	eventType, _ := msg["type"].(string)

	switch {
	case role == "assistant":
		if content := extractCodexText(msg); content != "" {
			c.emitAssistant(sessionID, content)
		}

	case role == "tool":
		if content := extractCodexText(msg); content != "" {
			c.OnAgentOutput(sessionID, "tool_result", content)
		}

	case eventType == "approval_request":
		toolName, _ := msg["tool_name"].(string)
		desc, _ := msg["description"].(string)
		c.OnAgentOutput(sessionID, "tool_call", fmt.Sprintf("⚠️ %s: %s", toolName, desc))

	case eventType == "approval_response":
		approved, _ := msg["approved"].(bool)
		if approved {
			c.OnAgentOutput(sessionID, "system", "✅ Approved")
		} else {
			c.OnAgentOutput(sessionID, "system", "❌ Denied")
		}

	case eventType == "error":
		if m, _ := msg["message"].(string); m != "" {
			c.OnAgentOutput(sessionID, "error", m)
		}

	case eventType == "turn.failed":
		if errObj, ok := msg["error"].(map[string]interface{}); ok {
			if m, _ := errObj["message"].(string); m != "" {
				c.OnAgentOutput(sessionID, "error", m)
			}
		}

	case eventType == "item.completed":
		// Prefer final agent_message only (skip tool items / duplicates).
		if item, ok := msg["item"].(map[string]interface{}); ok {
			it, _ := item["type"].(string)
			if it == "agent_message" || it == "message" {
				if t := extractCodexText(item); t != "" {
					c.emitAssistant(sessionID, t)
				}
			}
		}

	case eventType == "agent_message" || eventType == "message":
		if text := extractCodexText(msg); text != "" {
			c.emitAssistant(sessionID, text)
		}

	default:
		// ignore noisy lifecycle / duplicates
	}
}

func (c *CodexCommander) emitAssistant(sessionID, content string) {
	content = strings.TrimSpace(content)
	if content == "" || c.OnAgentOutput == nil {
		return
	}
	now := time.Now().Unix()
	c.mu.Lock()
	if c.lastAssistant[sessionID] == content && now-c.lastAssistantAt[sessionID] < 30 {
		c.mu.Unlock()
		return
	}
	c.lastAssistant[sessionID] = content
	c.lastAssistantAt[sessionID] = now
	c.mu.Unlock()
	c.OnAgentOutput(sessionID, "assistant", content)
}

// extractCodexText extracts human-readable text from a Codex JSON message.
func extractCodexText(msg map[string]interface{}) string {
	if t, ok := msg["text"].(string); ok && t != "" {
		return t
	}
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
		return strings.Join(parts, "\n")
	}
	return ""
}

// Approve is unavailable in non-interactive exec/resume (stdin closed after start).
func (c *CodexCommander) Approve(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.StdinOpen() {
		return executor.SendPrompt("y")
	}
	return fmt.Errorf("approval_unavailable: Codex runs non-interactively from phone; approve on the PC terminal (session %s)", sessionID)
}

// Deny is unavailable in non-interactive exec/resume (stdin closed after start).
func (c *CodexCommander) Deny(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.StdinOpen() {
		return executor.SendPrompt("n")
	}
	return fmt.Errorf("approval_unavailable: Codex runs non-interactively from phone; deny on the PC terminal (session %s)", sessionID)
}

// Interrupt sends SIGINT to interrupt a running Codex session.
func (c *CodexCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return executor.Interrupt()
}

// StopSession stops a running Codex executor.
func (c *CodexCommander) StopSession(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if !ok {
		return nil
	}
	return executor.Stop()
}

// StopAll stops every tracked executor (daemon shutdown).
func (c *CodexCommander) StopAll() {
	c.mu.Lock()
	list := make([]*AgentExecutor, 0, len(c.executors))
	for _, e := range c.executors {
		list = append(list, e)
	}
	c.executors = make(map[string]*AgentExecutor)
	c.mu.Unlock()
	for _, e := range list {
		_ = e.Stop()
	}
}
