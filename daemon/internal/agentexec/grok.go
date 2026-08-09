package agentexec

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nekonest/daemon/internal/attach"
)

const (
	grokMaxStreamBytes = 64 * 1024
	grokEmitStepBytes  = 512
)

// GrokCommander resumes Grok Build sessions in headless streaming-json mode.
type GrokCommander struct {
	mu        sync.Mutex
	cliPath   string
	executors map[string]*AgentExecutor
	streams   map[string]*grokStreamState

	OnAgentOutput func(sessionID, msgType, content, msgID string)
	OnTurnEnd     func(sessionID string, exitCode int, interrupted bool)
}

type grokStreamState struct {
	text           strings.Builder
	thought        strings.Builder
	textID         string
	thoughtID      string
	textEmitted    int
	thoughtEmitted int
}

func NewGrokCommander() *GrokCommander {
	return &GrokCommander{
		cliPath:   findGrokCLI(),
		executors: make(map[string]*AgentExecutor),
		streams:   make(map[string]*grokStreamState),
	}
}

func findGrokCLI() string {
	if p, err := exec.LookPath("grok.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("grok"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		filepath.Join(home, ".grok", "bin", "grok.exe"),
		filepath.Join(home, ".grok", "bin", "grok"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return "grok"
}

func (c *GrokCommander) CLIPath() string { return c.cliPath }

func (c *GrokCommander) IsAvailable() bool {
	if c.cliPath == "" {
		return false
	}
	if st, err := os.Stat(c.cliPath); err == nil && !st.IsDir() {
		return true
	}
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

func grokResumeArgs(sessionID, prompt, workDir string) []string {
	args := []string{
		"--resume", sessionID,
		"-p", prompt,
		"--output-format", "streaming-json",
		"--permission-mode", "auto",
	}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	return args
}

func grokStartArgs(sessionID, prompt, workDir string) []string {
	args := []string{
		"--session-id", sessionID,
		"-p", prompt,
		"--output-format", "streaming-json",
		"--permission-mode", "auto",
	}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	return args
}

// ProbeThreadStart verifies Grok's deterministic native session-id path
// without starting a conversation.
func (c *GrokCommander) ProbeThreadStart(ctx context.Context) error {
	return probeCLIHelp(ctx, c.cliPath, "--session-id", "--output-format", "streaming-json")
}

// StartThread uses Grok's documented --session-id new-conversation flag. The
// caller must confirm the returned native ID against ~/.grok/sessions.
func (c *GrokCommander) StartThread(ctx context.Context, workDir, prompt string) (string, bool, bool, error) {
	if err := c.ProbeThreadStart(ctx); err != nil {
		return "", false, false, err
	}
	sessionID := uuid.NewString()
	ack := make(chan struct{}, 1)
	var ackOnce sync.Once
	if err := c.startPromptInDir(sessionID, grokStartArgs(sessionID, prompt, workDir), workDir, nil, func(source, line string) {
		if source == "stdout" && grokPromptAcknowledged(line) {
			ackOnce.Do(func() { ack <- struct{}{} })
		}
	}); err != nil {
		return sessionID, false, false, err
	}
	select {
	case <-ack:
		return sessionID, true, true, nil
	case <-ctx.Done():
		return sessionID, true, false, fmt.Errorf("Grok initial prompt was not confirmed: %w", ctx.Err())
	}
}

func grokPromptAcknowledged(line string) bool {
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &message) != nil {
		return false
	}
	switch message.Type {
	case "text", "thought", "tool", "tool_call", "tool_use", "end", "done", "complete":
		return true
	default:
		return false
	}
}

func (c *GrokCommander) SendPromptInDir(
	sessionID string,
	prompt string,
	workDir string,
	_ []attach.LocalFile,
	onComplete func(),
) error {
	return c.startPromptInDir(sessionID, grokResumeArgs(sessionID, prompt, workDir), workDir, onComplete, nil)
}

func (c *GrokCommander) startPromptInDir(
	sessionID string,
	args []string,
	workDir string,
	onComplete func(),
	onOutput func(source, line string),
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return fmt.Errorf("%w: grok session %s is still running; wait for it to finish", ErrSessionBusy, sessionID)
	}
	if !c.IsAvailable() {
		return fmt.Errorf("grok CLI not found")
	}

	now := time.Now().UnixNano()
	c.streams[sessionID] = &grokStreamState{
		textID:    fmt.Sprintf("grok_%s_text_%d", sessionID, now),
		thoughtID: fmt.Sprintf("grok_%s_thought_%d", sessionID, now),
	}
	executor := NewAgentExecutor("grok_build", sessionID)
	diagnostics := &stderrDiagnostics{}
	executor.OnOutputSource = func(source, line string) {
		if onOutput != nil {
			onOutput(source, line)
		}
		if diagnostics.suppress("grok", sessionID, source) {
			return
		}
		c.handleProcessLine(sessionID, source, line)
	}
	executor.OnExit = func(exitCode int) {
		defer completePrompt(onComplete)
		c.flushStream(sessionID)
		log.Printf("[grok] session %s process exited with code %d", sessionID, exitCode)
		if message := diagnostics.exitFailure(
			"Grok Build",
			exitCode,
			executor.WasIntentionallyStopped(),
		); message != "" &&
			c.OnAgentOutput != nil {
			c.OnAgentOutput(sessionID, "error", message, "")
		}
		c.mu.Lock()
		if cur, ok := c.executors[sessionID]; ok && cur == executor {
			delete(c.executors, sessionID)
			delete(c.streams, sessionID)
		}
		c.mu.Unlock()
		if c.OnTurnEnd != nil {
			c.OnTurnEnd(sessionID, exitCode, executor.WasIntentionallyStopped())
		}
	}

	if err := executor.StartWithDir(c.cliPath, args, nil, workDir); err != nil {
		delete(c.streams, sessionID)
		return fmt.Errorf("start grok: %w", err)
	}
	_ = executor.CloseStdin()
	c.executors[sessionID] = executor
	return nil
}

func (c *GrokCommander) handleProcessLine(sessionID, source, line string) {
	if source != "stdout" {
		return
	}
	c.parseAndForwardOutput(sessionID, line)
}

func (c *GrokCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil {
		return
	}
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		c.emitChunk(sessionID, "assistant", line)
		return
	}
	eventType, _ := msg["type"].(string)
	data, _ := msg["data"].(string)
	switch eventType {
	case "text":
		c.emitChunk(sessionID, "assistant", data)
	case "thought":
		c.emitChunk(sessionID, "thinking", data)
	case "error":
		message, _ := msg["message"].(string)
		if message != "" {
			c.OnAgentOutput(sessionID, "error", message, "")
		}
	case "end", "done", "complete":
		c.flushStream(sessionID)
	default:
		// end and lifecycle events carry metadata only.
	}
}

func (c *GrokCommander) emitChunk(sessionID, msgType, chunk string) {
	if chunk == "" || c.OnAgentOutput == nil {
		return
	}
	c.mu.Lock()
	state := c.streams[sessionID]
	if state == nil {
		now := time.Now().UnixNano()
		state = &grokStreamState{
			textID:    fmt.Sprintf("grok_%s_text_%d", sessionID, now),
			thoughtID: fmt.Sprintf("grok_%s_thought_%d", sessionID, now),
		}
		c.streams[sessionID] = state
	}
	var content, msgID string
	var shouldEmit bool
	if msgType == "thinking" {
		appendGrokStreamChunk(&state.thought, chunk)
		content = state.thought.String()
		msgID = state.thoughtID
		if state.thoughtEmitted == 0 ||
			state.thought.Len()-state.thoughtEmitted >= grokEmitStepBytes ||
			(state.thought.Len() == grokMaxStreamBytes &&
				state.thoughtEmitted < state.thought.Len()) {
			state.thoughtEmitted = state.thought.Len()
			shouldEmit = true
		}
	} else {
		appendGrokStreamChunk(&state.text, chunk)
		content = state.text.String()
		msgID = state.textID
		if state.textEmitted == 0 ||
			state.text.Len()-state.textEmitted >= grokEmitStepBytes ||
			(state.text.Len() == grokMaxStreamBytes &&
				state.textEmitted < state.text.Len()) {
			state.textEmitted = state.text.Len()
			shouldEmit = true
		}
	}
	c.mu.Unlock()
	if shouldEmit {
		c.OnAgentOutput(sessionID, msgType, content, msgID)
	}
}

func appendGrokStreamChunk(builder *strings.Builder, chunk string) {
	remaining := grokMaxStreamBytes - builder.Len()
	if remaining <= 0 || chunk == "" {
		return
	}
	if len(chunk) > remaining {
		chunk = chunk[:remaining]
		for chunk != "" && !utf8.ValidString(chunk) {
			chunk = chunk[:len(chunk)-1]
		}
	}
	builder.WriteString(chunk)
}

func (c *GrokCommander) flushStream(sessionID string) {
	if c.OnAgentOutput == nil {
		return
	}
	type pendingEvent struct {
		msgType string
		content string
		msgID   string
	}
	var pending []pendingEvent
	c.mu.Lock()
	state := c.streams[sessionID]
	if state != nil {
		if state.text.Len() > state.textEmitted {
			state.textEmitted = state.text.Len()
			pending = append(pending, pendingEvent{
				msgType: "assistant",
				content: state.text.String(),
				msgID:   state.textID,
			})
		}
		if state.thought.Len() > state.thoughtEmitted {
			state.thoughtEmitted = state.thought.Len()
			pending = append(pending, pendingEvent{
				msgType: "thinking",
				content: state.thought.String(),
				msgID:   state.thoughtID,
			})
		}
	}
	c.mu.Unlock()
	for _, event := range pending {
		c.OnAgentOutput(sessionID, event.msgType, event.content, event.msgID)
	}
}

func (c *GrokCommander) Approve(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: Grok headless mode cannot accept approval %s for session %s", approvalID, sessionID)
}

func (c *GrokCommander) Deny(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: Grok headless mode cannot deny approval %s for session %s", approvalID, sessionID)
}

func (c *GrokCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return executor.Interrupt()
}

func (c *GrokCommander) StopAll() {
	c.mu.Lock()
	list := make([]*AgentExecutor, 0, len(c.executors))
	for _, executor := range c.executors {
		list = append(list, executor)
	}
	c.executors = make(map[string]*AgentExecutor)
	c.streams = make(map[string]*grokStreamState)
	c.mu.Unlock()
	for _, executor := range list {
		_ = executor.Stop()
	}
}
