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

// KiloCommander handles Kilo CLI interactions.
type KiloCommander struct {
	mu            sync.Mutex
	cliPath       string
	executors     map[string]*AgentExecutor
	nextRunNumber uint64

	// sessionDirs remembers project directory per session for resume.
	sessionDirs map[string]string

	// OnAgentOutput is called for each parsed line of agent output.
	// msgID may be empty (daemon will mint one) or a stable part id for streaming patches.
	OnAgentOutput func(
		sessionID string,
		runNumber uint64,
		msgType string,
		content string,
		msgID string,
	)

	// OnStreamStart is optional; called immediately before a Kilo process starts.
	OnStreamStart func(sessionID string, runNumber uint64, startedAt int64)
	// OnStreamEnd is optional; called when the process exits.
	OnStreamEnd func(sessionID string, runNumber uint64, exitCode int)
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

	if _, ok := c.executors[sessionID]; ok {
		return fmt.Errorf(
			"kilo session %s is still running or finishing; wait for it to finish",
			sessionID,
		)
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

	c.nextRunNumber++
	runNumber := c.nextRunNumber
	executor := NewAgentExecutor("kilo", sessionID)
	executor.OnOutputSource = func(source, line string) {
		c.handleProcessLine(sessionID, runNumber, source, line)
	}
	executor.OnExit = func(exitCode int) {
		log.Printf("[kilo] session %s process exited with code %d", sessionID, exitCode)
		c.mu.Lock()
		cur, ok := c.executors[sessionID]
		live := ok && cur == executor
		c.mu.Unlock()
		if !live {
			return
		}

		// Keep the map entry until all run-specific cleanup and error delivery
		// completes, so a new prompt cannot overlap this finishing window.
		if c.OnStreamEnd != nil {
			c.OnStreamEnd(sessionID, runNumber, exitCode)
		}
		c.mu.Lock()
		if cur, ok := c.executors[sessionID]; ok && cur == executor {
			delete(c.executors, sessionID)
		}
		c.mu.Unlock()
	}

	// Process cwd can stay default; --dir tells kilo the project root.
	startedAt := time.Now().UnixMilli()
	if c.OnStreamStart != nil {
		c.OnStreamStart(sessionID, runNumber, startedAt)
	}
	if err := executor.Start(c.cliPath, args, nil); err != nil {
		if c.OnStreamEnd != nil {
			c.OnStreamEnd(sessionID, runNumber, 0)
		}
		return fmt.Errorf("start kilo: %w", err)
	}
	// Prompt is on argv; leave stdin open and kilo waits forever for more input.
	_ = executor.CloseStdin()

	c.executors[sessionID] = executor
	log.Printf("[kilo] started process for session %s dir=%s", sessionID, workDir)
	return nil
}

func (c *KiloCommander) handleProcessLine(
	sessionID string,
	runNumber uint64,
	source string,
	line string,
) {
	if source == "stderr" {
		log.Printf("[kilo] session %s stderr: %s", sessionID, boundedDiagnostic(line))
		return
	}
	c.parseAndForwardOutput(sessionID, runNumber, line)
}

func (c *KiloCommander) parseAndForwardOutput(
	sessionID string,
	runNumber uint64,
	line string,
) {
	if c.OnAgentOutput == nil || strings.TrimSpace(line) == "" {
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		c.OnAgentOutput(sessionID, runNumber, "text", line, "")
		return
	}

	partID := ""
	if part, ok := msg["part"].(map[string]interface{}); ok {
		if id, _ := part["id"].(string); id != "" {
			partID = id
		}
	}
	if partID == "" {
		if id, _ := msg["id"].(string); id != "" {
			partID = id
		}
	}

	// Common Kilo JSON event shapes (best-effort).
	if t, _ := msg["type"].(string); t != "" {
		switch t {
		case "text", "message", "assistant":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, runNumber, "assistant", text, partID)
				return
			}
		case "reasoning", "thinking":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, runNumber, "thinking", text, partID)
				return
			}
		case "tool", "tool_call", "tool_use":
			name, _ := msg["tool"].(string)
			if name == "" {
				name, _ = msg["name"].(string)
			}
			if name == "" {
				if part, ok := msg["part"].(map[string]interface{}); ok {
					name, _ = part["tool"].(string)
					if name == "" {
						name, _ = part["name"].(string)
					}
				}
			}
			if name == "" {
				name = "tool"
			}
			c.OnAgentOutput(
				sessionID,
				runNumber,
				"tool_call",
				fmt.Sprintf("🔧 %s", name),
				partID,
			)
			return
		case "step_start", "step_finish":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, runNumber, "system", text, partID)
			}
			return
		case "error":
			if text := extractKiloText(msg); text != "" {
				c.OnAgentOutput(sessionID, runNumber, "error", text, partID)
				return
			}
			if text := extractKiloError(msg); text != "" {
				c.OnAgentOutput(sessionID, runNumber, "error", text, partID)
				return
			}
		case "permission", "permission_request":
			c.OnAgentOutput(
				sessionID,
				runNumber,
				"tool_call",
				"⚠️ permission request",
				partID,
			)
			return
		}
	}

	if text := extractKiloText(msg); text != "" {
		c.OnAgentOutput(sessionID, runNumber, "text", text, partID)
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

func extractKiloError(msg map[string]interface{}) string {
	raw, ok := msg["error"]
	if !ok || raw == nil {
		return ""
	}
	if text, ok := raw.(string); ok {
		return strings.TrimSpace(text)
	}
	errObj, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}

	name, _ := errObj["name"].(string)
	message, _ := errObj["message"].(string)
	if data, ok := errObj["data"].(map[string]interface{}); ok {
		if nested, _ := data["message"].(string); nested != "" {
			message = nested
		}
	}
	name = strings.TrimSpace(name)
	message = strings.TrimSpace(message)
	switch {
	case name != "" && message != "":
		return name + ": " + message
	case message != "":
		return message
	default:
		return name
	}
}

// Approve is unavailable when kilo run closes stdin after argv prompt.
func (c *KiloCommander) Approve(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.StdinOpen() {
		return executor.SendPrompt("y")
	}
	return fmt.Errorf("approval_unavailable: Kilo run is non-interactive from phone; approve on the PC (session %s)", sessionID)
}

// Deny is unavailable when kilo run closes stdin after argv prompt.
func (c *KiloCommander) Deny(sessionID, approvalID string) error {
	_ = approvalID
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if ok && executor.StdinOpen() {
		return executor.SendPrompt("n")
	}
	return fmt.Errorf("approval_unavailable: Kilo run is non-interactive from phone; deny on the PC (session %s)", sessionID)
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

// StopAll stops every tracked executor (daemon shutdown).
func (c *KiloCommander) StopAll() {
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
