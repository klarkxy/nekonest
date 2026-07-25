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

// KiloCommander handles Kilo CLI interactions.
type KiloCommander struct {
	mu        sync.Mutex
	cliPath   string
	executors map[string]*AgentExecutor

	// sessionDirs remembers project directory per session for resume.
	sessionDirs map[string]string

	// OnAgentOutput is called for each parsed line of agent output.
	OnAgentOutput func(sessionID string, msgType string, content string)
}

// NewKiloCommander creates a new Kilo commander.
func NewKiloCommander() *KiloCommander {
	return &KiloCommander{
		cliPath:     resolveKiloCLI(),
		executors:   make(map[string]*AgentExecutor),
		sessionDirs: make(map[string]string),
	}
}

// resolveKiloCLI finds the kilo binary on PATH or common install locations.
func resolveKiloCLI() string {
	if p, err := exec.LookPath("kilo"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "kilo"),
		filepath.Join(home, ".local", "bin", "kilo.exe"),
	}
	// VS Code / VS Code Insiders extension bundles
	for _, root := range []string{
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
	} {
		matches, _ := filepath.Glob(filepath.Join(root, "kilocode.kilo-code-*", "bin", "kilo"))
		candidates = append(candidates, matches...)
		matches, _ = filepath.Glob(filepath.Join(root, "kilocode.kilo-code-*", "bin", "kilo.exe"))
		candidates = append(candidates, matches...)
	}
	if runtime.GOOS == "windows" {
		// Prefer .exe if both exist
		for i := len(candidates) - 1; i >= 0; i-- {
			if strings.HasSuffix(strings.ToLower(candidates[i]), ".exe") {
				if st, err := os.Stat(candidates[i]); err == nil && !st.IsDir() {
					return candidates[i]
				}
			}
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return "kilo"
}

// CLIPath returns the resolved CLI path.
func (c *KiloCommander) CLIPath() string { return c.cliPath }

// IsAvailable checks if Kilo CLI is installed and runnable.
func (c *KiloCommander) IsAvailable() bool {
	if c.cliPath == "" {
		return false
	}
	if _, err := os.Stat(c.cliPath); err == nil {
		return true
	}
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

// RememberSessionDir stores the project directory for a session id.
func (c *KiloCommander) RememberSessionDir(sessionID, dir string) {
	if sessionID == "" || dir == "" {
		return
	}
	c.mu.Lock()
	c.sessionDirs[sessionID] = dir
	c.mu.Unlock()
}

// SessionDir returns a remembered project directory.
func (c *KiloCommander) SessionDir(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionDirs[sessionID]
}

// SendPrompt resumes a Kilo session with a new prompt.
// Uses: kilo run --session <id> --format json --dir <dir> <prompt>
func (c *KiloCommander) SendPrompt(sessionID, prompt string) error {
	return c.SendPromptInDir(sessionID, prompt, c.SessionDir(sessionID))
}

// SendPromptInDir resumes a session with an explicit working directory.
func (c *KiloCommander) SendPromptInDir(sessionID, prompt, workDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.IsAvailable() {
		return fmt.Errorf("kilo CLI not found")
	}
	if strings.HasPrefix(sessionID, "nekonest_") || strings.HasPrefix(sessionID, "new_") {
		return fmt.Errorf("invalid session id %q — use a session discovered from the PC", sessionID)
	}

	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return executor.SendPrompt(prompt)
	}

	if workDir != "" {
		c.sessionDirs[sessionID] = workDir
	}

	args := []string{
		"run",
		"--session", sessionID,
		"--format", "json",
	}
	if workDir != "" {
		args = append(args, "--dir", workDir)
	}
	// Positional message last
	args = append(args, prompt)

	executor := NewAgentExecutor("kilo", sessionID)
	executor.OnOutput = func(line string) {
		c.parseAndForwardOutput(sessionID, line)
	}
	executor.OnExit = func(exitCode int) {
		log.Printf("[kilo] session %s process exited with code %d", sessionID, exitCode)
		c.mu.Lock()
		delete(c.executors, sessionID)
		c.mu.Unlock()
	}

	// Process cwd can stay default; --dir tells kilo the project root.
	if err := executor.Start(c.cliPath, args, nil); err != nil {
		return fmt.Errorf("start kilo: %w", err)
	}

	c.executors[sessionID] = executor
	log.Printf("[kilo] started process for session %s dir=%s", sessionID, workDir)
	return nil
}

func (c *KiloCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil || strings.TrimSpace(line) == "" {
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		c.OnAgentOutput(sessionID, "text", line)
		return
	}

	// Common Kilo JSON event shapes (best-effort).
	if t, _ := msg["type"].(string); t != "" {
		switch t {
		case "text", "message", "assistant":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, "assistant", text)
				return
			}
		case "reasoning", "thinking":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, "thinking", text)
				return
			}
		case "tool", "tool_call":
			name, _ := msg["tool"].(string)
			if name == "" {
				name, _ = msg["name"].(string)
			}
			c.OnAgentOutput(sessionID, "tool_call", fmt.Sprintf("🔧 %s", name))
			return
		case "error":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, "error", text)
				return
			}
		case "permission", "permission_request":
			c.OnAgentOutput(sessionID, "tool_call", "⚠️ permission request")
			return
		}
	}

	if text := extractKiloText(msg); text != "" {
		c.OnAgentOutput(sessionID, "text", text)
	}
}

func extractKiloText(msg map[string]interface{}) string {
	for _, key := range []string{"text", "content", "message", "error"} {
		if s, ok := msg[key].(string); ok && s != "" {
			return s
		}
	}
	if part, ok := msg["part"].(map[string]interface{}); ok {
		if s, ok := part["text"].(string); ok && s != "" {
			return s
		}
	}
	if data, ok := msg["data"].(map[string]interface{}); ok {
		if s, ok := data["text"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// Approve approves a pending permission if a live process is attached.
func (c *KiloCommander) Approve(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.IsRunning() {
		return executor.SendPrompt("y")
	}
	return fmt.Errorf("approval_unavailable: no live Kilo process for session %s", sessionID)
}

// Deny denies a pending permission if a live process is attached.
func (c *KiloCommander) Deny(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.IsRunning() {
		return executor.SendPrompt("n")
	}
	return fmt.Errorf("approval_unavailable: no live Kilo process for session %s", sessionID)
}

// Interrupt stops a running Kilo session process.
func (c *KiloCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return executor.Interrupt()
}
