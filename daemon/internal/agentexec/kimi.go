package agentexec

import (
	"context"
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

	"github.com/nekonest/daemon/internal/attach"
)

// KimiCommander resumes Kimi Code sessions in stream-json print mode.
type KimiCommander struct {
	mu        sync.Mutex
	cliPath   string
	executors map[string]*AgentExecutor
	acpRuns   map[string]*ACPProcess
	acpChunks map[string]map[string]string // sessionID -> messageID -> cumulative content
	acpIDs    map[string]string            // sessionID -> fallback messageID for an ID-less first turn
	nextACP   uint64
	sequences map[string]uint64
	runIDs    map[string]string
	nextRun   uint64
	helpOnce  sync.Once
	helpText  string
	helpErr   error
	helpProbe func() (string, error)

	OnAgentOutput func(sessionID, msgType, content, msgID string)
}

func NewKimiCommander() *KimiCommander {
	return &KimiCommander{
		cliPath:   findKimiCLI(),
		executors: make(map[string]*AgentExecutor),
		acpRuns:   make(map[string]*ACPProcess),
		acpChunks: make(map[string]map[string]string),
		acpIDs:    make(map[string]string),
		sequences: make(map[string]uint64),
		runIDs:    make(map[string]string),
	}
}

// ProbeThreadStart verifies Kimi's ACP endpoint without creating a session.
func (c *KimiCommander) ProbeThreadStart(ctx context.Context) error {
	if !c.IsAvailable() {
		return fmt.Errorf("kimi CLI not found")
	}
	if err := probeCLIHelp(ctx, c.cliPath, "acp"); err != nil {
		return err
	}
	return ProbeACPStart(ctx, c.cliPath, []string{"acp"}, "")
}

// StartThread creates a Kimi-native session with ACP. The ACP ID is native;
// callers must namespace it at the adapter boundary and confirm store ownership.
func (c *KimiCommander) StartThread(ctx context.Context, workDir, prompt string) (string, bool, bool, error) {
	if err := c.ProbeThreadStart(ctx); err != nil {
		return "", false, false, err
	}
	var stateMu sync.Mutex
	var createdID string
	exited := false
	started, err := StartACPThread(ctx, ACPStartOptions{
		Command: c.cliPath,
		Args:    []string{"acp"},
		Dir:     workDir,
		OnSessionCreated: func(sessionID string) {
			c.beginACPStream(sessionID)
			stateMu.Lock()
			createdID = sessionID
			stateMu.Unlock()
		},
		OnUpdate: func(sessionID string, update map[string]any) {
			c.handleACPUpdate(sessionID, update)
		},
		OnPromptResult: func(sessionID string, promptErr error) {
			if promptErr != nil {
				c.emit(sessionID, "error", promptErr.Error(), fmt.Sprintf("kimi_acp_prompt_error_%d", time.Now().UnixNano()))
			}
		},
		OnExit: func(exitCode int) {
			stateMu.Lock()
			exited = true
			id := createdID
			stateMu.Unlock()
			c.mu.Lock()
			delete(c.acpRuns, id)
			delete(c.acpChunks, id)
			delete(c.acpIDs, id)
			c.mu.Unlock()
			_ = exitCode
		},
	})
	if err != nil {
		return started.SessionID, started.NativeCreatePossible, started.PromptAccepted, err
	}
	stateMu.Lock()
	if !exited {
		c.mu.Lock()
		c.acpRuns[started.SessionID] = started.Process
		c.mu.Unlock()
	}
	stateMu.Unlock()
	return started.SessionID, started.NativeCreatePossible, started.PromptAccepted, nil
}

func findKimiCLI() string {
	for _, name := range []string{"kimi.exe", "kimi", "kimi.cmd", "kimi-cli.exe", "kimi-cli"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	appData := os.Getenv("APPDATA")
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		filepath.Join(appData, "npm", "kimi.exe"),
		filepath.Join(appData, "npm", "kimi.cmd"),
		filepath.Join(home, ".local", "bin", "kimi"),
		filepath.Join(home, ".local", "bin", "kimi.exe"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	if runtime.GOOS == "windows" {
		return "kimi.exe"
	}
	return "kimi"
}

func (c *KimiCommander) CLIPath() string { return c.cliPath }

func (c *KimiCommander) IsAvailable() bool {
	if c.cliPath == "" {
		return false
	}
	if st, err := os.Stat(c.cliPath); err == nil && !st.IsDir() {
		return true
	}
	_, err := exec.LookPath(c.cliPath)
	return err == nil
}

func kimiResumeArgs(sessionID, prompt string, legacyPrint bool) []string {
	args := []string{"--session", sessionID, "-p", prompt, "--output-format", "stream-json"}
	if legacyPrint {
		args = append([]string{"--print"}, args...)
	}
	return args
}

func (c *KimiCommander) SendPromptInDir(
	sessionID string,
	prompt string,
	workDir string,
	attachments []attach.LocalFile,
	onComplete func(),
) error {
	return c.sendPromptInDir(
		sessionID,
		prompt,
		workDir,
		false,
		attachments,
		onComplete,
	)
}

// SendLegacyPromptInDir enables the explicit print mode required by kimi-cli.
func (c *KimiCommander) SendLegacyPromptInDir(
	sessionID string,
	prompt string,
	workDir string,
	attachments []attach.LocalFile,
	onComplete func(),
) error {
	return c.sendPromptInDir(
		sessionID,
		prompt,
		workDir,
		true,
		attachments,
		onComplete,
	)
}

func (c *KimiCommander) sendPromptInDir(
	sessionID string,
	prompt string,
	workDir string,
	legacyStore bool,
	_ []attach.LocalFile,
	onComplete func(),
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if executor, ok := c.executors[sessionID]; ok && executor.IsRunning() {
		return fmt.Errorf("%w: kimi session %s is still running; wait for it to finish", ErrSessionBusy, sessionID)
	}
	if !c.IsAvailable() {
		return fmt.Errorf("kimi CLI not found")
	}
	args, err := c.resumeArgs(sessionID, prompt, legacyStore)
	if err != nil {
		return err
	}

	c.beginRunLocked(sessionID)
	executor := NewAgentExecutor("kimi_cli", sessionID)
	diagnostics := &stderrDiagnostics{}
	executor.OnOutputSource = func(source, line string) {
		if diagnostics.suppress("kimi", sessionID, source) {
			return
		}
		c.handleProcessLine(sessionID, source, line)
	}
	executor.OnExit = func(exitCode int) {
		defer completePrompt(onComplete)
		log.Printf("[kimi] session %s process exited with code %d", sessionID, exitCode)
		if message := diagnostics.exitFailure(
			"Kimi CLI",
			exitCode,
			executor.WasIntentionallyStopped(),
		); message != "" {
			c.emit(sessionID, "error", message, "")
		}
		c.mu.Lock()
		if cur, ok := c.executors[sessionID]; ok && cur == executor {
			delete(c.executors, sessionID)
			delete(c.sequences, sessionID)
			delete(c.runIDs, sessionID)
		}
		c.mu.Unlock()
	}

	if err := executor.StartWithDir(c.cliPath, args, nil, workDir); err != nil {
		delete(c.sequences, sessionID)
		delete(c.runIDs, sessionID)
		return fmt.Errorf("start kimi: %w", err)
	}
	_ = executor.CloseStdin()
	c.executors[sessionID] = executor
	return nil
}

func (c *KimiCommander) resumeArgs(sessionID, prompt string, legacyStore bool) ([]string, error) {
	legacyCLI, err := c.supportsLegacyPrint()
	if err != nil {
		return nil, fmt.Errorf(
			"Kimi session is read-only because CLI capability detection failed: %w",
			err,
		)
	}
	if legacyStore && !legacyCLI {
		return nil, fmt.Errorf(
			"legacy Kimi session is read-only with the installed Kimi Code CLI; migrate it into .kimi-code first",
		)
	}
	if !legacyStore && legacyCLI {
		return nil, fmt.Errorf(
			"current Kimi Code session is read-only with the installed legacy kimi-cli",
		)
	}
	return kimiResumeArgs(sessionID, prompt, legacyCLI), nil
}

func (c *KimiCommander) supportsLegacyPrint() (bool, error) {
	c.helpOnce.Do(func() {
		probe := c.helpProbe
		if probe == nil {
			probe = c.probeHelp
		}
		c.helpText, c.helpErr = probe()
	})
	if c.helpErr != nil {
		return false, c.helpErr
	}
	return strings.Contains(c.helpText, "--print"), nil
}

func (c *KimiCommander) probeHelp() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	executable, args, err := resolveLaunch(c.cliPath, []string{"--help"})
	if err != nil {
		return "", err
	}
	output, runErr := exec.CommandContext(ctx, executable, args...).CombinedOutput()
	text := string(output)
	if runErr != nil && strings.TrimSpace(text) == "" {
		return "", runErr
	}
	return text, nil
}

func (c *KimiCommander) handleProcessLine(sessionID, source, line string) {
	if source != "stdout" {
		return
	}
	c.parseAndForwardOutput(sessionID, line)
}

func (c *KimiCommander) parseAndForwardOutput(sessionID, line string) {
	if c.OnAgentOutput == nil {
		return
	}
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		c.emit(sessionID, "assistant", line, "")
		return
	}
	role, _ := msg["role"].(string)
	eventType, _ := msg["type"].(string)
	content := extractKimiText(msg)
	msgID, _ := msg["id"].(string)
	if msgID == "" {
		msgID, _ = msg["tool_call_id"].(string)
	}
	switch {
	case role == "assistant" && content != "":
		c.emit(sessionID, "assistant", content, msgID)
	case role == "user" && content != "":
		c.emit(sessionID, "user", content, msgID)
	case role == "tool" && content != "":
		c.emit(sessionID, "tool_result", content, msgID)
	case eventType == "error":
		if content == "" {
			content, _ = msg["message"].(string)
		}
		if content != "" {
			c.emit(sessionID, "error", content, msgID)
		}
	default:
		// Ignore lifecycle records and empty messages.
	}
}

func (c *KimiCommander) handleACPUpdate(sessionID string, update map[string]any) {
	if updateType, _ := update["sessionUpdate"].(string); updateType != "agent_message_chunk" {
		return
	}
	content, _ := update["content"].(map[string]any)
	text, _ := content["text"].(string)
	messageID, _ := update["messageId"].(string)
	if strings.TrimSpace(text) == "" {
		return
	}
	c.mu.Lock()
	if messageID == "" {
		messageID = c.acpIDs[sessionID]
		if messageID == "" {
			c.nextACP++
			messageID = fmt.Sprintf("kimi_acp_%s_%d", sessionID, c.nextACP)
			c.acpIDs[sessionID] = messageID
		}
	}
	if c.acpChunks[sessionID] == nil {
		c.acpChunks[sessionID] = make(map[string]string)
	}
	c.acpChunks[sessionID][messageID] += text
	cumulative := c.acpChunks[sessionID][messageID]
	c.mu.Unlock()
	c.emit(sessionID, "assistant", cumulative, messageID)
}

func (c *KimiCommander) beginACPStream(sessionID string) {
	c.mu.Lock()
	c.acpChunks[sessionID] = make(map[string]string)
	delete(c.acpIDs, sessionID)
	c.mu.Unlock()
}

func (c *KimiCommander) emit(sessionID, msgType, content, msgID string) {
	content = strings.TrimSpace(content)
	if content == "" || c.OnAgentOutput == nil {
		return
	}
	if msgID == "" {
		c.mu.Lock()
		if c.runIDs[sessionID] == "" {
			c.beginRunLocked(sessionID)
		}
		c.sequences[sessionID]++
		msgID = fmt.Sprintf(
			"kimi_%s_%s_%d",
			sessionID,
			c.runIDs[sessionID],
			c.sequences[sessionID],
		)
		c.mu.Unlock()
	}
	c.OnAgentOutput(sessionID, msgType, content, msgID)
}

func (c *KimiCommander) beginRun(sessionID string) {
	c.mu.Lock()
	c.beginRunLocked(sessionID)
	c.mu.Unlock()
}

func (c *KimiCommander) beginRunLocked(sessionID string) {
	c.nextRun++
	c.sequences[sessionID] = 0
	c.runIDs[sessionID] = fmt.Sprintf("%d_%d", time.Now().UnixNano(), c.nextRun)
}

func extractKimiText(msg map[string]interface{}) string {
	content, ok := msg["content"]
	if !ok {
		if text, _ := msg["text"].(string); text != "" {
			return text
		}
		return ""
	}
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		var parts []string
		for _, raw := range value {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if text, _ := block["text"].(string); text != "" {
				parts = append(parts, text)
			}
			if output, _ := block["output"].(string); output != "" {
				parts = append(parts, output)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func (c *KimiCommander) Approve(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: Kimi print mode cannot accept approval %s for session %s", approvalID, sessionID)
}

func (c *KimiCommander) Deny(sessionID, approvalID string) error {
	return fmt.Errorf("approval_unavailable: Kimi print mode cannot deny approval %s for session %s", approvalID, sessionID)
}

func (c *KimiCommander) Interrupt(sessionID string) error {
	c.mu.Lock()
	executor, ok := c.executors[sessionID]
	acp := c.acpRuns[sessionID]
	c.mu.Unlock()
	if ok {
		return executor.Interrupt()
	}
	if acp != nil {
		return acp.Cancel()
	}
	if !ok {
		return fmt.Errorf("no running executor for session %s", sessionID)
	}
	return nil
}

func (c *KimiCommander) StopAll() {
	c.mu.Lock()
	list := make([]*AgentExecutor, 0, len(c.executors))
	for _, executor := range c.executors {
		list = append(list, executor)
	}
	c.executors = make(map[string]*AgentExecutor)
	acpList := make([]*ACPProcess, 0, len(c.acpRuns))
	for _, process := range c.acpRuns {
		acpList = append(acpList, process)
	}
	c.acpRuns = make(map[string]*ACPProcess)
	c.acpChunks = make(map[string]map[string]string)
	c.acpIDs = make(map[string]string)
	c.sequences = make(map[string]uint64)
	c.runIDs = make(map[string]string)
	c.mu.Unlock()
	for _, executor := range list {
		_ = executor.Stop()
	}
	for _, process := range acpList {
		_ = process.Stop()
	}
}
