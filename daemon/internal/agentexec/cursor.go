package agentexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/opslog"
)

const (
	cursorMaxStreamBytes   = 64 * 1024
	cursorEmitStepBytes    = 512
	cursorStartPlaceholder = "cursor-start"
)

// CursorCommander resumes Cursor Agent CLI sessions in print/stream-json mode.
// The desktop editor binary (cursor.exe) is never treated as this CLI.
type CursorCommander struct {
	mu        sync.Mutex
	cliPath   string
	executors map[string]*AgentExecutor
	streams   map[string]*cursorStreamState

	OnAgentOutput func(sessionID, msgType, content, msgID string)
	OnTurnEnd     func(sessionID string, exitCode int, interrupted bool)
}

type cursorStreamState struct {
	text           strings.Builder
	thought        strings.Builder
	textID         string
	thoughtID      string
	textEmitted    int
	thoughtEmitted int
}

func NewCursorCommander() *CursorCommander {
	return &CursorCommander{
		cliPath:   findCursorAgentCLI(),
		executors: make(map[string]*AgentExecutor),
		streams:   make(map[string]*cursorStreamState),
	}
}

func findCursorAgentCLI() string {
	if env := strings.TrimSpace(os.Getenv("NEKONEST_CURSOR_CLI")); looksLikeCursorAgentCLI(env) {
		return env
	}
	if found := cliLookPath("cursor-agent.exe", "cursor-agent.cmd", "cursor-agent"); found != "" && looksLikeCursorAgentCLI(found) {
		return found
	}
	home, _ := os.UserHomeDir()
	localApp := os.Getenv("LOCALAPPDATA")
	if found := firstExistingFile(
		filepath.Join(home, ".local", "bin", "cursor-agent.exe"),
		filepath.Join(home, ".local", "bin", "cursor-agent.cmd"),
		filepath.Join(home, ".local", "bin", "cursor-agent"),
		filepath.Join(home, ".cursor", "bin", "cursor-agent.exe"),
		filepath.Join(home, ".cursor", "bin", "cursor-agent.cmd"),
		filepath.Join(home, ".cursor", "bin", "cursor-agent"),
		filepath.Join(home, ".cursor", "bin", "agent.exe"),
		filepath.Join(home, ".cursor", "bin", "agent"),
		filepath.Join(localApp, "cursor-agent", "cursor-agent.exe"),
		filepath.Join(localApp, "cursor-agent", "cursor-agent.cmd"),
		filepath.Join(localApp, "cursor-agent", "agent.exe"),
	); found != "" && looksLikeCursorAgentCLI(found) {
		return found
	}
	if runtime.GOOS == "windows" {
		return "cursor-agent.exe"
	}
	return "cursor-agent"
}

func (c *CursorCommander) CLIPath() string { return c.cliPath }

func (c *CursorCommander) SetCLIPath(path string) { c.cliPath = strings.TrimSpace(path) }

func (c *CursorCommander) IsAvailable() bool {
	if c == nil || !looksLikeCursorAgentCLI(c.cliPath) {
		return false
	}
	if isNodeScript(c.cliPath) {
		return nodeScriptRunnable(c.cliPath)
	}
	if fileExists(c.cliPath) {
		return true
	}
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

func cursorResumeArgs(sessionID, prompt, workDir string, attachments []attach.LocalFile) []string {
	args := []string{
		"--resume", sessionID, "-p", prompt,
		"--output-format", "stream-json", "--stream-partial-output",
		"--force", "--trust",
	}
	if workDir != "" {
		args = append(args, "--workspace", workDir)
	}
	for _, dir := range attachmentDirs(attachments) {
		args = append(args, "--add-dir", dir)
	}
	return args
}

func cursorStartArgs(prompt, workDir string, attachments []attach.LocalFile) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json", "--stream-partial-output",
		"--force", "--trust",
	}
	if workDir != "" {
		args = append(args, "--workspace", workDir)
	}
	for _, dir := range attachmentDirs(attachments) {
		args = append(args, "--add-dir", dir)
	}
	return args
}

func (c *CursorCommander) ProbeThreadStart(ctx context.Context) error {
	if !c.IsAvailable() {
		return fmt.Errorf("cursor agent CLI not found")
	}
	return probeCLIHelp(ctx, c.cliPath, "--resume", "--print", "--output-format", "--force", "--add-dir")
}

func (c *CursorCommander) StartThread(ctx context.Context, workDir, prompt string, attachments []attach.LocalFile, onComplete func()) (string, bool, bool, error) {
	if err := c.ProbeThreadStart(ctx); err != nil {
		return "", false, false, err
	}
	ack := make(chan struct{}, 1)
	exited := make(chan struct{})
	var ackOnce, exitOnce sync.Once
	var nativeID string
	var idMu sync.Mutex
	var lastDiag string
	var diagMu sync.Mutex
	placeholder := cursorStartPlaceholder
	wrappedComplete := func() {
		exitOnce.Do(func() { close(exited) })
		if onComplete != nil {
			onComplete()
		}
	}
	if err := c.startPromptInDir(placeholder, cursorStartArgs(prompt, workDir, attachments), workDir, wrappedComplete, func(source, line string) {
		if source == "stderr" {
			if text := strings.TrimSpace(line); text != "" {
				diagMu.Lock()
				lastDiag = text
				diagMu.Unlock()
			}
			return
		}
		if source != "stdout" {
			return
		}
		if id := cursorSessionIDFromLine(line); id != "" {
			idMu.Lock()
			if nativeID == "" {
				nativeID = id
			}
			idMu.Unlock()
		}
		if cursorPromptAcknowledged(line) {
			ackOnce.Do(func() { ack <- struct{}{} })
		}
	}); err != nil {
		return "", false, false, err
	}
	return waitPromptAck(ctx, ack, exited, &nativeID, &idMu, "Cursor", func() string {
		diagMu.Lock()
		defer diagMu.Unlock()
		return lastDiag
	})
}

func cursorPromptAcknowledged(line string) bool {
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &message) != nil {
		return false
	}
	switch strings.ToLower(message.Type) {
	case "assistant", "result":
		return true
	default:
		return false
	}
}

func cursorSessionIDFromLine(line string) string {
	var payload map[string]interface{}
	if json.Unmarshal([]byte(line), &payload) != nil {
		return ""
	}
	for _, key := range []string{"session_id", "sessionId", "chat_id", "chatId"} {
		id := strings.TrimSpace(fmt.Sprint(payload[key]))
		if looksLikeCursorSessionID(id) {
			return id
		}
	}
	return ""
}

func looksLikeCursorSessionID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == "<nil>" {
		return false
	}
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		return true
	}
	return len(id) >= 8 && !strings.ContainsAny(id, " \t\n")
}

func (c *CursorCommander) SendPromptInDir(
	sessionID string,
	prompt string,
	workDir string,
	attachments []attach.LocalFile,
	onComplete func(),
) error {
	return c.startPromptInDir(sessionID, cursorResumeArgs(sessionID, prompt, workDir, attachments), workDir, onComplete, nil)
}

func (c *CursorCommander) startPromptInDir(
	sessionID string,
	args []string,
	workDir string,
	onComplete func(),
	onOutput func(source, line string),
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return fmt.Errorf("%w: cursor session %s is still running; wait for it to finish", ErrSessionBusy, sessionID)
	}
	if !c.IsAvailable() {
		return fmt.Errorf("cursor agent CLI not found")
	}

	now := time.Now().UnixNano()
	c.streams[sessionID] = &cursorStreamState{
		textID:    fmt.Sprintf("cursor_%s_text_%d", sessionID, now),
		thoughtID: fmt.Sprintf("cursor_%s_thought_%d", sessionID, now),
	}
	placeholder := sessionID == cursorStartPlaceholder
	executor := NewAgentExecutor("cursor", sessionID)
	diagnostics := &stderrDiagnostics{}
	executor.OnOutputSource = func(source, line string) {
		if onOutput != nil {
			onOutput(source, line)
		}
		if placeholder || diagnostics.suppress("cursor", sessionID, source, line) {
			return
		}
		c.handleProcessLine(sessionID, source, line)
	}
	executor.OnExit = func(exitCode int) {
		defer completePrompt(onComplete)
		if !placeholder {
			c.flushStream(sessionID)
			opslog.Info("daemon.agentexec", "process_exited", "agent process exited", "agent_type", "cursor", "session_id", sessionID, "status", exitCode)
			if message := diagnostics.exitFailure(
				"Cursor",
				exitCode,
				executor.WasIntentionallyStopped(),
			); message != "" && c.OnAgentOutput != nil {
				c.OnAgentOutput(sessionID, "error", message, "")
			}
		}
		c.mu.Lock()
		if cur, ok := c.executors[sessionID]; ok && cur == executor {
			delete(c.executors, sessionID)
			delete(c.streams, sessionID)
		}
		c.mu.Unlock()
		if !placeholder && c.OnTurnEnd != nil {
			c.OnTurnEnd(sessionID, exitCode, executor.WasIntentionallyStopped())
		}
	}

	if err := executor.StartWithDir(c.cliPath, args, nil, workDir); err != nil {
		delete(c.streams, sessionID)
		return fmt.Errorf("start cursor-agent: %w", err)
	}
	_ = executor.CloseStdin()
	c.executors[sessionID] = executor
	return nil
}

func (c *CursorCommander) handleProcessLine(sessionID, source, line string) {
	if source != "stdout" {
		return
	}
	c.parseAndForwardOutput(sessionID, line)
}

func (c *CursorCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil {
		return
	}
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		c.emitChunk(sessionID, "assistant", line)
		return
	}
	eventType := strings.ToLower(fmt.Sprint(msg["type"]))
	switch eventType {
	case "thinking", "reasoning":
		if text := extractClaudeText(msg); text != "" {
			c.emitChunk(sessionID, "thinking", text)
		}
	case "assistant":
		if text := extractClaudeText(msg); text != "" {
			c.emitChunk(sessionID, "assistant", text)
		}
	case "error":
		message := strings.TrimSpace(fmt.Sprint(msg["message"]))
		if message == "" {
			message = extractClaudeText(msg)
		}
		if message != "" && message != "<nil>" {
			c.OnAgentOutput(sessionID, "error", message, "")
		}
	case "result":
		// Final envelope repeats the completed assistant text; flush only.
		c.flushStream(sessionID)
	case "system", "status", "init", "user", "tool_call", "tool_use", "tool":
		// lifecycle / non-assistant
	default:
		// ignore unknown event types
	}
}

func (c *CursorCommander) emitChunk(sessionID, msgType, chunk string) {
	if chunk == "" || c.OnAgentOutput == nil {
		return
	}
	c.mu.Lock()
	state := c.streams[sessionID]
	if state == nil {
		now := time.Now().UnixNano()
		state = &cursorStreamState{
			textID:    fmt.Sprintf("cursor_%s_text_%d", sessionID, now),
			thoughtID: fmt.Sprintf("cursor_%s_thought_%d", sessionID, now),
		}
		c.streams[sessionID] = state
	}
	var content, msgID string
	var shouldEmit bool
	if msgType == "thinking" {
		appendBoundedChunk(&state.thought, chunk, cursorMaxStreamBytes)
		content = state.thought.String()
		msgID = state.thoughtID
		if state.thoughtEmitted == 0 ||
			state.thought.Len()-state.thoughtEmitted >= cursorEmitStepBytes ||
			(state.thought.Len() == cursorMaxStreamBytes && state.thoughtEmitted < state.thought.Len()) {
			state.thoughtEmitted = state.thought.Len()
			shouldEmit = true
		}
	} else {
		appendBoundedChunk(&state.text, chunk, cursorMaxStreamBytes)
		content = state.text.String()
		msgID = state.textID
		if state.textEmitted == 0 ||
			state.text.Len()-state.textEmitted >= cursorEmitStepBytes ||
			(state.text.Len() == cursorMaxStreamBytes && state.textEmitted < state.text.Len()) {
			state.textEmitted = state.text.Len()
			shouldEmit = true
		}
	}
	c.mu.Unlock()
	if shouldEmit {
		c.OnAgentOutput(sessionID, msgType, content, msgID)
	}
}

func (c *CursorCommander) flushStream(sessionID string) {
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
			pending = append(pending, pendingEvent{msgType: "assistant", content: state.text.String(), msgID: state.textID})
		}
		if state.thought.Len() > state.thoughtEmitted {
			state.thoughtEmitted = state.thought.Len()
			pending = append(pending, pendingEvent{msgType: "thinking", content: state.thought.String(), msgID: state.thoughtID})
		}
	}
	c.mu.Unlock()
	for _, event := range pending {
		c.OnAgentOutput(sessionID, event.msgType, event.content, event.msgID)
	}
}

func (c *CursorCommander) Approve(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: Cursor print mode cannot accept approval %s for session %s", approvalID, sessionID)
}

func (c *CursorCommander) Deny(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: Cursor print mode cannot deny approval %s for session %s", approvalID, sessionID)
}

func (c *CursorCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return executor.Interrupt()
}

func (c *CursorCommander) StopAll() {
	c.mu.Lock()
	list := make([]*AgentExecutor, 0, len(c.executors))
	for _, executor := range c.executors {
		list = append(list, executor)
	}
	c.executors = make(map[string]*AgentExecutor)
	c.streams = make(map[string]*cursorStreamState)
	c.mu.Unlock()
	for _, executor := range list {
		_ = executor.Stop()
	}
}
