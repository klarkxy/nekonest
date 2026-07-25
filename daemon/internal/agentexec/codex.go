package agentexec

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
)

// CodexCommander handles Codex CLI interactions.
type CodexCommander struct {
	mu        sync.Mutex
	cliPath   string
	executors map[string]*AgentExecutor

	// OnAgentOutput is called for each line of output from the agent.
	OnAgentOutput func(sessionID string, msgType string, content string)
}

// NewCodexCommander creates a new Codex commander.
func NewCodexCommander() *CodexCommander {
	cliPath := "codex"
	if p, err := exec.LookPath("codex"); err == nil {
		cliPath = p
	}
	return &CodexCommander{
		cliPath:   cliPath,
		executors: make(map[string]*AgentExecutor),
	}
}

// IsAvailable checks if Codex CLI is installed.
func (c *CodexCommander) IsAvailable() bool {
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

// SendPrompt sends a prompt to a Codex session.
func (c *CodexCommander) SendPrompt(sessionID, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check for existing executor
	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return executor.SendPrompt(prompt)
	}

	// Start new codex process
	args := []string{
		"resume",
		sessionID,
	}

	executor := NewAgentExecutor("codex", sessionID)

	// Wire up output callback
	executor.OnOutput = func(line string) {
		c.parseAndForwardOutput(sessionID, line)
	}

	executor.OnExit = func(exitCode int) {
		log.Printf("[codex] session %s process exited with code %d", sessionID, exitCode)
		c.mu.Lock()
		delete(c.executors, sessionID)
		c.mu.Unlock()
	}

	if err := executor.Start(c.cliPath, args, nil); err != nil {
		return fmt.Errorf("start codex: %w", err)
	}

	// Send the prompt after process starts
	if err := executor.SendPrompt(prompt); err != nil {
		executor.Stop()
		return fmt.Errorf("send prompt: %w", err)
	}

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
		content := extractCodexText(msg)
		if content != "" {
			c.OnAgentOutput(sessionID, "assistant", content)
		}

	case role == "tool":
		content := extractCodexText(msg)
		if content != "" {
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

	default:
		if text := extractCodexText(msg); text != "" {
			c.OnAgentOutput(sessionID, eventType, text)
		}
	}
}

// extractCodexText extracts human-readable text from a Codex JSON message.
func extractCodexText(msg map[string]interface{}) string {
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

// Approve approves a pending tool call in a Codex session.
// Only works while a live Codex process is attached; file fallback is not honored by the CLI.
func (c *CodexCommander) Approve(sessionID, approvalID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if ok && executor.IsRunning() {
		return executor.SendPrompt("y")
	}

	return fmt.Errorf("approval_unavailable: no live Codex process for session %s (open/resume the session on the PC first)", sessionID)
}

// Deny denies a pending tool call.
func (c *CodexCommander) Deny(sessionID, approvalID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()

	if ok && executor.IsRunning() {
		return executor.SendPrompt("n")
	}

	return fmt.Errorf("approval_unavailable: no live Codex process for session %s (open/resume the session on the PC first)", sessionID)
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
