package agentexec

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ClaudeCommander handles Claude Code CLI interactions.
type ClaudeCommander struct {
	mu        sync.Mutex
	cliPath   string                            // path to claude binary
	executors map[string]*AgentExecutor         // sessionID -> executor

	// OnAgentOutput is called for each line of output from the agent.
	// The callback receives (sessionID, parsedMessageType, content).
	OnAgentOutput func(sessionID string, msgType string, content string)
}

// NewClaudeCommander creates a new Claude Code commander.
func NewClaudeCommander() *ClaudeCommander {
	cliPath := "claude" // assume it's in PATH
	if p, err := exec.LookPath("claude"); err == nil {
		cliPath = p
	}
	return &ClaudeCommander{
		cliPath:   cliPath,
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

	// Check if we already have a running executor for this session
	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return executor.SendPrompt(prompt)
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
		delete(c.executors, sessionID)
		c.mu.Unlock()
	}

	if err := executor.StartWithDir(c.cliPath, args, nil, workDir); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}

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

// Approve approves a pending tool call.
// Only works while a print/resume process is still attached and reading stdin.
// Historical JSONL "waiting_approval" state without a live process cannot be approved remotely.
func (c *ClaudeCommander) Approve(sessionID, approvalID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if ok && executor.IsRunning() {
		return executor.SendPrompt("y")
	}

	return fmt.Errorf("approval_unavailable: no live Claude process for session %s (open/resume the session on the PC first)", sessionID)
}

// Deny denies a pending approval.
func (c *ClaudeCommander) Deny(sessionID, approvalID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if ok && executor.IsRunning() {
		return executor.SendPrompt("n")
	}

	return fmt.Errorf("approval_unavailable: no live Claude process for session %s (open/resume the session on the PC first)", sessionID)
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
func FindCLIBinary() string {
	locations := []string{
		"claude",
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "claude", "claude.exe"),
		filepath.Join(os.Getenv("APPDATA"), "npm", "claude.cmd"),
		`C:\Program Files\Claude\claude.exe`,
	}

	for _, loc := range locations {
		if p, err := exec.LookPath(loc); err == nil {
			return p
		}
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}
	return "claude"
}

// ParseSessionIDFromPath extracts session ID from a Claude Code JSONL file path.
func ParseSessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".jsonl")
}
