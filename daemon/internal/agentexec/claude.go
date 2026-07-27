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
)

// ClaudeCommander handles Claude Code CLI interactions.
type ClaudeCommander struct {
	mu        sync.Mutex
	cliPath   string                    // path to claude binary
	executors map[string]*AgentExecutor // sessionID -> executor

	// OnAgentOutput is called for each line of output from the agent.
	// The callback receives (sessionID, parsedMessageType, content).
	OnAgentOutput func(sessionID string, msgType string, content string)
}

// NewClaudeCommander creates a new Claude Code commander.
func NewClaudeCommander() *ClaudeCommander {
	return &ClaudeCommander{
		cliPath:   FindCLIBinary(),
		executors: make(map[string]*AgentExecutor),
	}
}

// IsAvailable checks if Claude Code CLI is installed.
func (c *ClaudeCommander) IsAvailable() bool {
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

// SendPrompt resumes a Claude Code session with a new prompt.
// Uses: claude --resume <session-id> -p "<prompt>" --output-format stream-json
// sessionID must be a real Claude session id (from Discover / JSONL basename).
func (c *ClaudeCommander) SendPrompt(sessionID, prompt string) error {
	return c.SendPromptInDir(sessionID, prompt, "")
}

// SendPromptInDir is like SendPrompt but sets the process working directory.
func (c *ClaudeCommander) SendPromptInDir(sessionID, prompt, workDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Print/resume closes stdin after -p; a second prompt needs a new process.
	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return fmt.Errorf("claude session %s is still running; wait for it to finish", sessionID)
	}

	// Reject obviously fake IDs from older create_session experiments
	if strings.HasPrefix(sessionID, "nekonest_") || strings.HasPrefix(sessionID, "new_") {
		return fmt.Errorf("invalid session id %q — use a session discovered from the PC", sessionID)
	}

	// Resume existing Claude session (print mode, JSON stream)
	args := []string{
		"--resume", sessionID,
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
	}

	executor := NewAgentExecutor("claude_code", sessionID)
	executor.OnOutput = func(line string) {
		c.parseAndForwardOutput(sessionID, line)
	}
	executor.OnExit = func(exitCode int) {
		log.Printf("[claude] session %s process exited with code %d", sessionID, exitCode)
		c.mu.Lock()
		if cur, ok := c.executors[sessionID]; ok && cur == executor {
			delete(c.executors, sessionID)
		}
		c.mu.Unlock()
	}

	if err := executor.StartWithDir(c.cliPath, args, nil, workDir); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	// Prompt already passed via -p; close stdin to avoid 3s "no stdin" wait.
	_ = executor.CloseStdin()

	c.executors[sessionID] = executor
	log.Printf("[claude] started process for session %s", sessionID)
	return nil
}

// parseAndForwardOutput parses a JSON line from Claude Code output and calls OnAgentOutput.
// Claude Code --output-format json emits one JSON object per line:
// {"type": "assistant", "message": {"content": [...]}}
// {"type": "result", "result": "...", "cost_usd": 0.01}
// {"type": "system", "subtype": "init", ...}
func (c *ClaudeCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil {
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Not JSON, forward as raw text
		c.OnAgentOutput(sessionID, "text", line)
		return
	}

	msgType, _ := msg["type"].(string)

	switch msgType {
	case "assistant":
		// Extract text from message.content array
		content := extractClaudeText(msg)
		if content != "" {
			c.OnAgentOutput(sessionID, "assistant", content)
		}

	case "result":
		// Final result
		result, _ := msg["result"].(string)
		if result != "" {
			c.OnAgentOutput(sessionID, "assistant", result)
		}

	case "system":
		subtype, _ := msg["subtype"].(string)
		c.OnAgentOutput(sessionID, "system", fmt.Sprintf("[%s]", subtype))

	default:
		// Forward other types as-is
		if text := extractClaudeText(msg); text != "" {
			c.OnAgentOutput(sessionID, msgType, text)
		}
	}
}

// extractClaudeText extracts human-readable text from a Claude Code JSON message.
func extractClaudeText(msg map[string]interface{}) string {
	// Try message.content first (assistant messages)
	if message, ok := msg["message"].(map[string]interface{}); ok {
		if content, ok := message["content"].([]interface{}); ok {
			return extractTextFromContentBlocks(content)
		}
		if content, ok := message["content"].(string); ok {
			return content
		}
	}

	// Try top-level content
	if content, ok := msg["content"].([]interface{}); ok {
		return extractTextFromContentBlocks(content)
	}
	if content, ok := msg["content"].(string); ok {
		return content
	}

	// Try result field
	if result, ok := msg["result"].(string); ok {
		return result
	}

	return ""
}

// extractTextFromContentBlocks extracts text from Claude's content block arrays.
func extractTextFromContentBlocks(blocks []interface{}) string {
	var parts []string
	for _, block := range blocks {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := blockMap["type"].(string)
		switch blockType {
		case "text":
			if text, ok := blockMap["text"].(string); ok {
				parts = append(parts, text)
			}
		case "thinking":
			if thinking, ok := blockMap["thinking"].(string); ok {
				parts = append(parts, fmt.Sprintf("[thinking] %s", thinking))
			}
		case "tool_use":
			toolName, _ := blockMap["name"].(string)
			parts = append(parts, fmt.Sprintf("[tool: %s]", toolName))
		}
	}
	return strings.Join(parts, "\n")
}

// Approve is unavailable in print/resume mode (stdin closed after -p).
func (c *ClaudeCommander) Approve(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.StdinOpen() {
		return executor.SendPrompt("y")
	}
	return fmt.Errorf("approval_unavailable: Claude print mode is non-interactive; approve on the PC terminal (session %s)", sessionID)
}

// Deny is unavailable in print/resume mode (stdin closed after -p).
func (c *ClaudeCommander) Deny(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.StdinOpen() {
		return executor.SendPrompt("n")
	}
	return fmt.Errorf("approval_unavailable: Claude print mode is non-interactive; deny on the PC terminal (session %s)", sessionID)
}

// Interrupt sends SIGINT to a running Claude Code process.
func (c *ClaudeCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return executor.Interrupt()
}

// StopAll stops every tracked executor (daemon shutdown).
func (c *ClaudeCommander) StopAll() {
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

// StopSession stops a running Claude Code executor.
func (c *ClaudeCommander) StopSession(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if !ok {
		return nil
	}
	return executor.Stop()
}

// FindCLIBinary locates the Claude Code CLI binary.
// On Windows prefer the real .exe under npm global (not the npm shim without extension).
func FindCLIBinary() string {
	appData := os.Getenv("APPDATA")
	localApp := os.Getenv("LOCALAPPDATA")
	candidates := []string{
		filepath.Join(appData, "npm", "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe"),
		filepath.Join(localApp, "Programs", "claude", "claude.exe"),
		`C:\Program Files\Claude\claude.exe`,
	}
	for _, loc := range candidates {
		if st, err := os.Stat(loc); err == nil && !st.IsDir() {
			return loc
		}
	}
	// PATH: prefer *.exe
	if p, err := exec.LookPath("claude.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("claude"); err == nil {
		// Skip non-executable npm shims on Windows (no extension)
		if runtime.GOOS == "windows" {
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".exe" || ext == ".cmd" || ext == ".bat" {
				return p
			}
			// bare "claude" file is often a unix shell shim — try sibling .cmd / nested exe
			cmdShim := p + ".cmd"
			if st, err := os.Stat(cmdShim); err == nil && !st.IsDir() {
				// Prefer nested exe next to npm root
				nested := filepath.Join(filepath.Dir(p), "node_modules", "@anthropic-ai", "claude-code", "bin", "claude.exe")
				if st, err := os.Stat(nested); err == nil && !st.IsDir() {
					return nested
				}
				return cmdShim
			}
		}
		return p
	}
	return "claude"
}

// ParseSessionIDFromPath extracts session ID from a Claude Code JSONL file path.
func ParseSessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".jsonl")
}
