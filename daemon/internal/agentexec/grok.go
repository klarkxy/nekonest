package agentexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/opslog"
)

const (
	grokMaxStreamBytes = 64 * 1024
	grokEmitStepBytes  = 512
)

// grokEmitLatency bounds relay cost for short incremental ACP/legacy chunks.
// The first useful content still emits immediately; pending content flushes on
// this window, the 512-byte step, terminal events, process exit, or stop.
var grokEmitLatency = 40 * time.Millisecond

// GrokCommander resumes Grok Build sessions in headless streaming-json mode.
type GrokCommander struct {
	mu         sync.Mutex
	dispatchMu sync.Mutex
	cliPath    string
	executors  map[string]*AgentExecutor
	streams    map[string]*grokStreamState

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
	emitTimer      *time.Timer
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
	var msg map[string]interface{}
	if json.Unmarshal([]byte(line), &msg) != nil {
		return false
	}
	if method, _ := msg["method"].(string); method == "session/update" {
		update := grokACPUpdate(msg)
		if update == nil {
			return false
		}
		switch sessionUpdate, _ := update["sessionUpdate"].(string); sessionUpdate {
		case "agent_message_chunk", "agent_thought_chunk",
			"tool_call", "tool_call_update", "tool_call_progress",
			"turn_completed":
			return true
		default:
			// User echoes, system primers, plans, retry state, and unknown
			// lifecycle metadata must not count as prompt acceptance.
			return false
		}
	}
	eventType, _ := msg["type"].(string)
	switch eventType {
	case "text", "thought", "tool", "tool_call", "tool_use", "end", "done", "complete":
		return true
	default:
		return false
	}
}

func grokACPUpdate(msg map[string]interface{}) map[string]interface{} {
	params, _ := msg["params"].(map[string]interface{})
	if params == nil {
		return nil
	}
	update, _ := params["update"].(map[string]interface{})
	return update
}

func grokACPText(update map[string]interface{}) string {
	if update == nil {
		return ""
	}
	switch content := update["content"].(type) {
	case string:
		return content
	case map[string]interface{}:
		text, _ := content["text"].(string)
		return text
	default:
		return ""
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

	c.replaceStreamLocked(sessionID)
	executor := NewAgentExecutor("grok_build", sessionID)
	diagnostics := &stderrDiagnostics{}
	executor.OnOutputSource = func(source, line string) {
		if onOutput != nil {
			onOutput(source, line)
		}
		if diagnostics.suppress("grok_build", sessionID, source, line) {
			return
		}
		c.handleProcessLine(sessionID, source, line)
	}
	executor.OnExit = func(exitCode int) {
		defer completePrompt(onComplete)
		c.flushStream(sessionID)
		opslog.Info("daemon.agentexec", "process_exited", "agent process exited", "agent_type", "grok_build", "session_id", sessionID, "status", exitCode)
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
			c.clearStreamLocked(sessionID)
		}
		c.mu.Unlock()
		if c.OnTurnEnd != nil {
			c.OnTurnEnd(sessionID, exitCode, executor.WasIntentionallyStopped())
		}
	}

	if err := executor.StartWithDir(c.cliPath, args, nil, workDir); err != nil {
		c.clearStreamLocked(sessionID)
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

func (c *GrokCommander) replaceStreamLocked(sessionID string) {
	c.clearStreamLocked(sessionID)
	now := time.Now().UnixNano()
	c.streams[sessionID] = &grokStreamState{
		textID:    fmt.Sprintf("grok_%s_text_%d", sessionID, now),
		thoughtID: fmt.Sprintf("grok_%s_thought_%d", sessionID, now),
	}
}

func (c *GrokCommander) clearStreamLocked(sessionID string) {
	if state := c.streams[sessionID]; state != nil {
		c.stopEmitTimerLocked(state)
	}
	delete(c.streams, sessionID)
}

func (c *GrokCommander) stopEmitTimerLocked(state *grokStreamState) {
	if state == nil || state.emitTimer == nil {
		return
	}
	state.emitTimer.Stop()
	state.emitTimer = nil
}

func (c *GrokCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil {
		return
	}
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		c.emitChunk(sessionID, "assistant", line, "")
		return
	}
	if method, _ := msg["method"].(string); method == "session/update" {
		c.handleACPSessionUpdate(sessionID, msg)
		return
	}
	eventType, _ := msg["type"].(string)
	data, _ := msg["data"].(string)
	switch eventType {
	case "text":
		c.emitChunk(sessionID, "assistant", data, "")
	case "thought":
		c.emitChunk(sessionID, "thinking", data, "")
	case "error":
		message, _ := msg["message"].(string)
		if message != "" {
			c.OnAgentOutput(sessionID, "error", message, "")
		}
	case "end", "done", "complete":
		c.flushStream(sessionID)
	default:
		// Lifecycle and unknown records carry metadata only.
	}
}

func (c *GrokCommander) handleACPSessionUpdate(sessionID string, msg map[string]interface{}) {
	update := grokACPUpdate(msg)
	if update == nil {
		return
	}
	sessionUpdate, _ := update["sessionUpdate"].(string)
	switch sessionUpdate {
	case "agent_message_chunk":
		text := grokACPText(update)
		if text == "" {
			return
		}
		messageID, _ := update["messageId"].(string)
		c.emitChunk(sessionID, "assistant", text, messageID)
	case "agent_thought_chunk":
		text := grokACPText(update)
		if text == "" {
			return
		}
		messageID, _ := update["messageId"].(string)
		c.emitChunk(sessionID, "thinking", text, messageID)
	case "turn_completed":
		c.flushStream(sessionID)
	default:
		// Ignore user/system chunks, tool bodies, plans, retry metadata, and
		// unknown lifecycle events. They must never become assistant text.
	}
}

func (c *GrokCommander) emitChunk(sessionID, msgType, chunk, nativeID string) {
	if chunk == "" || c.OnAgentOutput == nil {
		return
	}
	c.dispatchMu.Lock()
	defer c.dispatchMu.Unlock()
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
		if nativeID != "" && state.thought.Len() == 0 {
			state.thoughtID = nativeID
		}
		appendGrokStreamChunk(&state.thought, chunk)
		content = state.thought.String()
		msgID = state.thoughtID
		shouldEmit = noteGrokPending(&state.thoughtEmitted, state.thought.Len())
	} else {
		if nativeID != "" && state.text.Len() == 0 {
			state.textID = nativeID
		}
		appendGrokStreamChunk(&state.text, chunk)
		content = state.text.String()
		msgID = state.textID
		shouldEmit = noteGrokPending(&state.textEmitted, state.text.Len())
	}
	pendingOther := state.text.Len() > state.textEmitted ||
		state.thought.Len() > state.thoughtEmitted
	if shouldEmit && !pendingOther {
		c.stopEmitTimerLocked(state)
	} else if pendingOther || !shouldEmit {
		// Keep a shared latency flush while either stream still has pending
		// content. An immediate emit on one stream must not cancel the other.
		c.scheduleEmitLocked(sessionID, state)
	}
	c.mu.Unlock()
	if shouldEmit {
		c.OnAgentOutput(sessionID, msgType, content, msgID)
	}
}

func noteGrokPending(emitted *int, length int) bool {
	if *emitted == 0 ||
		length-*emitted >= grokEmitStepBytes ||
		(length == grokMaxStreamBytes && *emitted < length) {
		*emitted = length
		return true
	}
	return false
}

func (c *GrokCommander) scheduleEmitLocked(sessionID string, state *grokStreamState) {
	if state.emitTimer != nil {
		return
	}
	state.emitTimer = time.AfterFunc(grokEmitLatency, func() {
		c.flushScheduled(sessionID, state)
	})
}

func (c *GrokCommander) flushScheduled(sessionID string, state *grokStreamState) {
	if c.OnAgentOutput == nil {
		return
	}
	c.dispatchMu.Lock()
	defer c.dispatchMu.Unlock()
	type pendingEvent struct {
		msgType string
		content string
		msgID   string
	}
	var pending []pendingEvent
	c.mu.Lock()
	if c.streams[sessionID] != state {
		c.mu.Unlock()
		return
	}
	state.emitTimer = nil
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
	c.mu.Unlock()
	for _, event := range pending {
		c.OnAgentOutput(sessionID, event.msgType, event.content, event.msgID)
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
	c.dispatchMu.Lock()
	defer c.dispatchMu.Unlock()
	type pendingEvent struct {
		msgType string
		content string
		msgID   string
	}
	var pending []pendingEvent
	c.mu.Lock()
	state := c.streams[sessionID]
	if state != nil {
		c.stopEmitTimerLocked(state)
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
	for sessionID := range c.streams {
		c.clearStreamLocked(sessionID)
	}
	c.streams = make(map[string]*grokStreamState)
	c.mu.Unlock()
	for _, executor := range list {
		_ = executor.Stop()
	}
}
