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
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/opslog"
)

const (
	zcodeMaxStreamBytes   = 64 * 1024
	zcodeEmitStepBytes    = 512
	zcodeStartPlaceholder = "zcode-start"
	// ZCodeUnavailableReason is the catalog/doctor text while upstream
	// `zcode login` cannot populate headless credentials.
	ZCodeUnavailableReason = "ZCode is unavailable: headless login is broken upstream"
)

// ZCodeCommander resumes ZCode CLI sessions in headless --json mode.
type ZCodeCommander struct {
	mu             sync.Mutex
	cliPath        string
	runtimeEnabled bool
	executors      map[string]*AgentExecutor
	streams        map[string]*zcodeStreamState

	OnAgentOutput func(sessionID, msgType, content, msgID string)
	OnTurnEnd     func(sessionID string, exitCode int, interrupted bool)
}

type zcodeStreamState struct {
	text           strings.Builder
	thought        strings.Builder
	textID         string
	thoughtID      string
	textEmitted    int
	thoughtEmitted int
}

func NewZCodeCommander() *ZCodeCommander {
	return &ZCodeCommander{
		cliPath:   findZcodeCLI(),
		executors: make(map[string]*AgentExecutor),
		streams:   make(map[string]*zcodeStreamState),
	}
}

func findZcodeCLI() string {
	if env := strings.TrimSpace(os.Getenv("ZCODE_CLI")); env != "" && !isElectronGUIBinary(env) {
		return env
	}
	if found := cliLookPath("zcode.exe", "zcode", "zcode.cmd"); found != "" {
		return found
	}
	home, _ := os.UserHomeDir()
	localApp := os.Getenv("LOCALAPPDATA")
	if found := firstExistingFile(
		filepath.Join(home, ".local", "bin", "zcode.exe"),
		filepath.Join(home, ".local", "bin", "zcode"),
		filepath.Join(home, ".local", "bin", "zcode.cmd"),
		filepath.Join(home, ".local", "bin", "zcode.cjs"),
		filepath.Join(localApp, "Programs", "ZCode", "resources", "glm", "zcode.cjs"),
	); found != "" {
		return found
	}
	if runtime.GOOS == "windows" {
		return "zcode.exe"
	}
	return "zcode"
}

func (c *ZCodeCommander) CLIPath() string { return c.cliPath }

func (c *ZCodeCommander) SetCLIPath(path string) { c.cliPath = strings.TrimSpace(path) }

// EnableRuntimeForTest turns the adapter back on for fixture tests. Production
// catalogs stay unavailable until upstream headless login works.
func (c *ZCodeCommander) EnableRuntimeForTest() {
	if c != nil {
		c.runtimeEnabled = true
	}
}

func (c *ZCodeCommander) IsAvailable() bool {
	if c == nil || !c.runtimeEnabled || strings.TrimSpace(c.cliPath) == "" || isElectronGUIBinary(c.cliPath) {
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

func zcodeResumeArgs(sessionID, prompt, workDir string, attachments []attach.LocalFile) []string {
	args := []string{"--resume", sessionID, "--prompt", prompt, "--json", "--mode", "yolo"}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	for _, file := range attachments {
		if path := strings.TrimSpace(file.Path); path != "" {
			args = append(args, "--attach", path)
		}
	}
	return args
}

func zcodeStartArgs(prompt, workDir string, attachments []attach.LocalFile) []string {
	args := []string{"--prompt", prompt, "--json", "--mode", "yolo"}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	for _, file := range attachments {
		if path := strings.TrimSpace(file.Path); path != "" {
			args = append(args, "--attach", path)
		}
	}
	return args
}

func (c *ZCodeCommander) ProbeThreadStart(ctx context.Context) error {
	if c == nil || !c.runtimeEnabled {
		return fmt.Errorf("%s", ZCodeUnavailableReason)
	}
	if !c.IsAvailable() {
		return fmt.Errorf("zcode CLI not found")
	}
	if err := probeCLIHelp(ctx, c.cliPath, "--resume", "--prompt", "--json"); err != nil {
		return err
	}
	return zcodeHeadlessConfigured()
}

func zcodeCLIConfigPath() string {
	home, _ := os.UserHomeDir()
	root := strings.TrimSpace(os.Getenv("ZCODE_HOME"))
	if root == "" {
		root = filepath.Join(home, ".zcode")
	}
	return filepath.Join(root, "cli", "config.json")
}

func zcodeHeadlessConfigured() error {
	path := zcodeCLIConfigPath()
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("ZCode headless model config is missing; create %s with an explicit model provider", path)
	}
	return nil
}

func (c *ZCodeCommander) StartThread(ctx context.Context, workDir, prompt string, attachments []attach.LocalFile, onComplete func()) (string, bool, bool, error) {
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
	placeholder := zcodeStartPlaceholder + "-" + uuid.NewString()
	wrappedComplete := func() {
		exitOnce.Do(func() { close(exited) })
		if onComplete != nil {
			onComplete()
		}
	}
	if err := c.startPromptInDir(placeholder, zcodeStartArgs(prompt, workDir, attachments), workDir, wrappedComplete, func(source, line string) {
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
		if id := zcodeSessionIDFromLine(line); id != "" {
			idMu.Lock()
			if nativeID == "" {
				nativeID = id
			}
			idMu.Unlock()
		}
		if zcodePromptAcknowledged(line) {
			ackOnce.Do(func() { ack <- struct{}{} })
		}
	}); err != nil {
		return "", false, false, err
	}
	return waitPromptAck(ctx, ack, exited, &nativeID, &idMu, "ZCode", func() string {
		diagMu.Lock()
		defer diagMu.Unlock()
		return lastDiag
	})
}

func zcodePromptAcknowledged(line string) bool {
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &message) != nil {
		return false
	}
	switch strings.ToLower(message.Type) {
	case "text", "assistant", "message", "result", "thought", "thinking", "tool", "tool_call":
		return true
	default:
		return false
	}
}

func zcodeSessionIDFromLine(line string) string {
	var payload map[string]interface{}
	if json.Unmarshal([]byte(line), &payload) != nil {
		return ""
	}
	for _, key := range []string{"session_id", "sessionId", "sessionID", "id"} {
		id := strings.TrimSpace(fmt.Sprint(payload[key]))
		if looksLikeZCodeSessionID(id) {
			return id
		}
	}
	if nested, ok := payload["session"].(map[string]interface{}); ok {
		for _, key := range []string{"id", "session_id", "sessionId"} {
			id := strings.TrimSpace(fmt.Sprint(nested[key]))
			if looksLikeZCodeSessionID(id) {
				return id
			}
		}
	}
	return ""
}

func looksLikeZCodeSessionID(id string) bool {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "sess_") || strings.Contains(id, "subagent") {
		return false
	}
	return len(id) > 10
}

func (c *ZCodeCommander) SendPromptInDir(
	sessionID string,
	prompt string,
	workDir string,
	attachments []attach.LocalFile,
	onComplete func(),
) error {
	return c.startPromptInDir(sessionID, zcodeResumeArgs(sessionID, prompt, workDir, attachments), workDir, onComplete, nil)
}

func (c *ZCodeCommander) startPromptInDir(
	sessionID string,
	args []string,
	workDir string,
	onComplete func(),
	onOutput func(source, line string),
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return fmt.Errorf("%w: zcode session %s is still running; wait for it to finish", ErrSessionBusy, sessionID)
	}
	if !c.IsAvailable() {
		return fmt.Errorf("zcode CLI not found")
	}

	now := time.Now().UnixNano()
	c.streams[sessionID] = &zcodeStreamState{
		textID:    fmt.Sprintf("zcode_%s_text_%d", sessionID, now),
		thoughtID: fmt.Sprintf("zcode_%s_thought_%d", sessionID, now),
	}
	placeholder := strings.HasPrefix(sessionID, zcodeStartPlaceholder)
	executor := NewAgentExecutor("zcode", sessionID)
	diagnostics := &stderrDiagnostics{}
	executor.OnOutputSource = func(source, line string) {
		if onOutput != nil {
			onOutput(source, line)
		}
		if placeholder || diagnostics.suppress("zcode", sessionID, source, line) {
			return
		}
		c.handleProcessLine(sessionID, source, line)
	}
	executor.OnExit = func(exitCode int) {
		defer completePrompt(onComplete)
		if !placeholder {
			c.flushStream(sessionID)
			opslog.Info("daemon.agentexec", "process_exited", "agent process exited", "agent_type", "zcode", "session_id", sessionID, "status", exitCode)
			if message := diagnostics.exitFailure(
				"ZCode",
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
		return fmt.Errorf("start zcode: %w", err)
	}
	_ = executor.CloseStdin()
	c.executors[sessionID] = executor
	return nil
}

func (c *ZCodeCommander) handleProcessLine(sessionID, source, line string) {
	if source != "stdout" {
		return
	}
	c.parseAndForwardOutput(sessionID, line)
}

func (c *ZCodeCommander) parseAndForwardOutput(sessionID, line string) {
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
	case "thought", "thinking", "reasoning":
		if text := zcodeTextFromMessage(msg); text != "" {
			c.emitChunk(sessionID, "thinking", text)
		}
	case "text", "assistant", "message", "result":
		if text := zcodeTextFromMessage(msg); text != "" {
			c.emitChunk(sessionID, "assistant", text)
		}
	case "error":
		message := strings.TrimSpace(fmt.Sprint(msg["message"]))
		if message == "" {
			message = zcodeTextFromMessage(msg)
		}
		if message != "" && message != "<nil>" {
			c.OnAgentOutput(sessionID, "error", message, "")
		}
	case "end", "done", "complete":
		c.flushStream(sessionID)
	case "system", "status", "init", "session", "tool", "tool_call", "tool_use":
		// lifecycle / non-assistant
	default:
		// ignore unknown event types
	}
}

func zcodeTextFromMessage(msg map[string]interface{}) string {
	if text := extractClaudeText(msg); text != "" {
		return text
	}
	for _, key := range []string{"text", "data", "delta", "output"} {
		if value, ok := msg[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *ZCodeCommander) emitChunk(sessionID, msgType, chunk string) {
	if chunk == "" || c.OnAgentOutput == nil {
		return
	}
	c.mu.Lock()
	state := c.streams[sessionID]
	if state == nil {
		now := time.Now().UnixNano()
		state = &zcodeStreamState{
			textID:    fmt.Sprintf("zcode_%s_text_%d", sessionID, now),
			thoughtID: fmt.Sprintf("zcode_%s_thought_%d", sessionID, now),
		}
		c.streams[sessionID] = state
	}
	var content, msgID string
	var shouldEmit bool
	if msgType == "thinking" {
		appendBoundedChunk(&state.thought, chunk, zcodeMaxStreamBytes)
		content = state.thought.String()
		msgID = state.thoughtID
		if state.thoughtEmitted == 0 ||
			state.thought.Len()-state.thoughtEmitted >= zcodeEmitStepBytes ||
			(state.thought.Len() == zcodeMaxStreamBytes && state.thoughtEmitted < state.thought.Len()) {
			state.thoughtEmitted = state.thought.Len()
			shouldEmit = true
		}
	} else {
		appendBoundedChunk(&state.text, chunk, zcodeMaxStreamBytes)
		content = state.text.String()
		msgID = state.textID
		if state.textEmitted == 0 ||
			state.text.Len()-state.textEmitted >= zcodeEmitStepBytes ||
			(state.text.Len() == zcodeMaxStreamBytes && state.textEmitted < state.text.Len()) {
			state.textEmitted = state.text.Len()
			shouldEmit = true
		}
	}
	c.mu.Unlock()
	if shouldEmit {
		c.OnAgentOutput(sessionID, msgType, content, msgID)
	}
}

func appendBoundedChunk(builder *strings.Builder, chunk string, max int) {
	remaining := max - builder.Len()
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

func (c *ZCodeCommander) flushStream(sessionID string) {
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

func (c *ZCodeCommander) Approve(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: ZCode headless mode cannot accept approval %s for session %s", approvalID, sessionID)
}

func (c *ZCodeCommander) Deny(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: ZCode headless mode cannot deny approval %s for session %s", approvalID, sessionID)
}

func (c *ZCodeCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return executor.Interrupt()
}

func (c *ZCodeCommander) StopAll() {
	c.mu.Lock()
	list := make([]*AgentExecutor, 0, len(c.executors))
	for _, executor := range c.executors {
		list = append(list, executor)
	}
	c.executors = make(map[string]*AgentExecutor)
	c.streams = make(map[string]*zcodeStreamState)
	c.mu.Unlock()
	for _, executor := range list {
		_ = executor.Stop()
	}
}
