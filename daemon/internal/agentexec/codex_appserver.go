package agentexec

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nekonest/daemon/internal/attach"
)

// CodexAppServer is a long-lived stdio JSON-RPC client for codex app-server.
// Protocol baseline: codex-cli 0.144.x (initialize -> thread/start -> turn/start).
// Approvals are server->client JSON-RPC requests; the client answers with result frames.
type CodexAppServer struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	cancel      context.CancelFunc
	nextID      atomic.Int64
	pending     map[string]chan rpcResult
	running     bool
	initialized bool
	binPath     string
	onNotify    func(method string, params json.RawMessage)
	onRequest   func(req ServerRequest)

	activeTurn   map[string]string
	turnStatus   map[string]string
	threadAlias  map[string]string
	pendingAppr  map[string]*PendingApproval
	wireByThread map[string]string
}

type rpcResult struct {
	Result json.RawMessage
	Error  error
}

// ServerRequest is an inbound JSON-RPC request from codex app-server.
type ServerRequest struct {
	ID     string
	Method string
	Params json.RawMessage
}

// AppServerOutput is a normalized phone-visible event parsed from an app-server notification.
type AppServerOutput struct {
	ThreadID  string
	TurnID    string
	MessageID string
	Type      string
	Content   string
	Delta     bool
	Final     bool
}

// PendingApproval is a live app-server approval/user-input request awaiting a phone decision.
type PendingApproval struct {
	ID          string
	RequestID   string
	Method      string
	ThreadID    string
	TurnID      string
	WireID      string
	ToolName    string
	Description string
	Params      json.RawMessage
	Permissions json.RawMessage
	CreatedAt   time.Time
}

// ApprovalSnapshot is the phone-facing pending approval shape.
type ApprovalSnapshot struct {
	ID          string
	ToolName    string
	Description string
	ThreadID    string
	TurnID      string
	WireID      string
}

// NewCodexAppServer prepares a controller (does not start until Ensure).
func NewCodexAppServer() *CodexAppServer {
	return &CodexAppServer{
		pending:      make(map[string]chan rpcResult),
		activeTurn:   make(map[string]string),
		turnStatus:   make(map[string]string),
		threadAlias:  make(map[string]string),
		pendingAppr:  make(map[string]*PendingApproval),
		wireByThread: make(map[string]string),
		binPath:      "codex",
	}
}

// SetNotifyHandler receives server-pushed JSON-RPC notifications.
func (c *CodexAppServer) SetNotifyHandler(fn func(method string, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onNotify = fn
}

// SetRequestHandler receives server-initiated JSON-RPC requests (approvals, user input).
func (c *CodexAppServer) SetRequestHandler(fn func(req ServerRequest)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRequest = fn
}

// Available reports whether the codex binary exposes app-server.
func (c *CodexAppServer) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binPath, "app-server", "--help")
	out, err := cmd.CombinedOutput()
	s := strings.ToLower(string(out))
	if err != nil {
		return strings.Contains(s, "app-server") || strings.Contains(s, "generate-json-schema")
	}
	return strings.Contains(s, "app-server") || strings.Contains(s, "json")
}

// Initialized reports whether initialize completed on the current process.
func (c *CodexAppServer) Initialized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running && c.initialized
}

// Ensure starts the app-server process and completes initialize handshake.
func (c *CodexAppServer) Ensure() error {
	if err := c.ensureProcess(); err != nil {
		return err
	}
	return c.ensureInitialized()
}
func (c *CodexAppServer) ensureProcess() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, c.binPath, "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("start codex app-server: %w", err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.cancel = cancel
	c.running = true
	c.initialized = false
	c.pending = make(map[string]chan rpcResult)
	c.activeTurn = make(map[string]string)
	c.turnStatus = make(map[string]string)
	c.pendingAppr = make(map[string]*PendingApproval)
	go c.readLoop()
	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		c.running = false
		c.initialized = false
		for id, ch := range c.pending {
			ch <- rpcResult{Error: fmt.Errorf("app-server exited")}
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()
	log.Printf("[codex-app-server] started pid=%d", cmd.Process.Pid)
	c.mu.Unlock()
	return nil
}

func (c *CodexAppServer) ensureInitialized() error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := c.callLocked(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "nekonest-daemon",
			"title":   "NekoNest",
			"version": "0.1.0",
		},
	})
	if err != nil {
		return fmt.Errorf("codex app-server initialize: %w", err)
	}
	_ = c.writeNotification("initialized", map[string]any{})
	_ = c.writeNotification("notifications/initialized", map[string]any{})

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	log.Printf("[codex-app-server] initialized")
	return nil
}

// Close stops the app-server process.
func (c *CodexAppServer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.running = false
	c.initialized = false
	return nil
}

// Call issues a JSON-RPC request and waits for the matching response.
func (c *CodexAppServer) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := c.Ensure(); err != nil {
		return nil, err
	}
	return c.callLocked(ctx, method, params)
}

func (c *CodexAppServer) callLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	idNum := c.nextID.Add(1)
	id := fmt.Sprintf("%d", idNum)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      idNum,
		"method":  method,
		"params":  params,
	}
	if params == nil {
		req["params"] = map[string]any{}
	}
	ch := make(chan rpcResult, 1)
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil, fmt.Errorf("app-server not running")
	}
	c.pending[id] = ch
	line, err := json.Marshal(req)
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	line = append(line, byte(10))
	if _, err := c.stdin.Write(line); err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case res := <-ch:
		return res.Result, res.Error
	}
}
func (c *CodexAppServer) writeNotification(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stdin == nil {
		return fmt.Errorf("app-server not running")
	}
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	if params == nil {
		msg["params"] = map[string]any{}
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, byte(10))
	_, err = c.stdin.Write(line)
	return err
}

// Respond writes a JSON-RPC response to a server-initiated request.
func (c *CodexAppServer) Respond(requestID string, result any) error {
	idVal, err := encodeRequestID(requestID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stdin == nil {
		return fmt.Errorf("app-server not running")
	}
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      idVal,
		"result":  result,
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, byte(10))
	_, err = c.stdin.Write(line)
	return err
}

// RespondError writes a JSON-RPC error response to a server-initiated request.
func (c *CodexAppServer) RespondError(requestID string, code int, message string) error {
	idVal, err := encodeRequestID(requestID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stdin == nil {
		return fmt.Errorf("app-server not running")
	}
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      idVal,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, byte(10))
	_, err = c.stdin.Write(line)
	return err
}

func encodeRequestID(id string) (any, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("empty request id")
	}
	if isAllDigits(id) {
		var i int64
		if _, err := fmt.Sscan(id, &i); err == nil {
			return i, nil
		}
	}
	return id, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// StartThreadResult is the dual-id outcome of thread/start.
type StartThreadResult struct {
	ThreadID  string
	SessionID string
	TurnID    string
	Raw       json.RawMessage
}

// WireID prefers the id used in Codex rollout ownership (sessionId, else threadId).
func (r StartThreadResult) WireID() string {
	if strings.TrimSpace(r.SessionID) != "" {
		return r.SessionID
	}
	return r.ThreadID
}

// StartThread creates a native Codex thread in cwd (thread/start).
func (c *CodexAppServer) StartThread(ctx context.Context, cwd, firstPrompt string) (StartThreadResult, error) {
	return c.StartThreadWithAttachments(ctx, cwd, firstPrompt, nil)
}

// StartThreadWithAttachments starts a thread and optional first turn with attachments.
func (c *CodexAppServer) StartThreadWithAttachments(ctx context.Context, cwd, firstPrompt string, files []attach.LocalFile) (StartThreadResult, error) {
	var out StartThreadResult
	if err := c.Ensure(); err != nil {
		return out, err
	}
	params := buildThreadStartParams(cwd)
	raw, err := c.Call(ctx, "thread/start", params)
	if err != nil {
		return out, fmt.Errorf("thread/start: %w", err)
	}
	out.Raw = raw
	out.ThreadID, out.SessionID = parseThreadStartResponse(raw)
	if out.ThreadID == "" && out.SessionID == "" {
		return out, fmt.Errorf("thread/start: empty thread id in response: %s", truncateRPC(raw, 240))
	}
	c.RegisterThreadIDs(out.ThreadID, out.SessionID, out.WireID())
	log.Printf("[codex-app-server] thread/start threadId=%s sessionId=%s cwd=%s", out.ThreadID, out.SessionID, cwd)

	prompt := strings.TrimSpace(firstPrompt)
	if prompt != "" || len(files) > 0 {
		turnThread := out.ThreadID
		if turnThread == "" {
			turnThread = out.SessionID
		}
		turnID, turnErr := c.StartTurn(ctx, turnThread, prompt, files)
		out.TurnID = turnID
		if turnErr != nil {
			log.Printf("[codex-app-server] turn/start after thread/start: %v", turnErr)
			return out, fmt.Errorf("thread created (%s) but turn/start failed: %w", out.WireID(), turnErr)
		}
	}
	return out, nil
}

// ResumeThread asks app-server to resume/rejoin a native thread id.
func (c *CodexAppServer) ResumeThread(ctx context.Context, threadID string) (threadOut, sessionOut string, err error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", "", fmt.Errorf("thread id required")
	}
	raw, err := c.Call(ctx, "thread/resume", map[string]any{"threadId": threadID})
	if err != nil {
		return "", "", err
	}
	tid, sid := parseThreadStartResponse(raw)
	if tid == "" {
		tid = threadID
	}
	c.RegisterThreadIDs(tid, sid, threadID)
	return tid, sid, nil
}

// StartTurn starts a turn on an existing thread.
func (c *CodexAppServer) StartTurn(ctx context.Context, threadID, prompt string, files []attach.LocalFile) (turnID string, err error) {
	threadID = c.ResolveThreadID(threadID)
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("thread id required")
	}
	input, err := buildTurnInput(prompt, files)
	if err != nil {
		return "", err
	}
	raw, err := c.Call(ctx, "turn/start", buildTurnStartParams(threadID, input))
	if err != nil {
		return "", err
	}
	turnID = parseTurnID(raw)
	if turnID != "" {
		c.setStartingTurn(threadID, turnID)
	}
	return turnID, nil
}

// InterruptTurn interrupts the active turn for threadID.
func (c *CodexAppServer) InterruptTurn(ctx context.Context, threadID string) error {
	threadID = c.ResolveThreadID(threadID)
	turnID := c.ActiveTurnID(threadID)
	if threadID == "" {
		return fmt.Errorf("thread id required")
	}
	if turnID == "" {
		return fmt.Errorf("no active turn id for thread %s", threadID)
	}
	if err := c.waitForStartedTurn(ctx, threadID, turnID); err != nil {
		return err
	}
	_, err := c.Call(ctx, "turn/interrupt", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	return err
}

// SteerTurn steers the active turn for threadID.
func (c *CodexAppServer) SteerTurn(ctx context.Context, threadID, text string) error {
	threadID = c.ResolveThreadID(threadID)
	turnID := c.ActiveTurnID(threadID)
	text = strings.TrimSpace(text)
	if threadID == "" {
		return fmt.Errorf("thread id required")
	}
	if turnID == "" {
		return fmt.Errorf("no active turn id for thread %s", threadID)
	}
	if text == "" {
		return fmt.Errorf("steer text required")
	}
	if err := c.waitForStartedTurn(ctx, threadID, turnID); err != nil {
		return err
	}
	_, err := c.Call(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input": []map[string]any{
			{"type": "text", "text": text},
		},
	})
	return err
}

// ApprovePending answers a tracked approval request with accept/approved semantics.
func (c *CodexAppServer) ApprovePending(approvalID string) error {
	return c.DecidePendingWithFallback(approvalID, true)
}

// DenyPending answers a tracked approval request with decline/denied semantics.
func (c *CodexAppServer) DenyPending(approvalID string) error {
	return c.DecidePendingWithFallback(approvalID, false)
}

// DecidePendingWithFallback responds using result payload or error frame when needed.
func (c *CodexAppServer) DecidePendingWithFallback(approvalID string, accept bool) error {
	c.mu.Lock()
	p := c.pendingAppr[approvalID]
	if p == nil {
		for _, cand := range c.pendingAppr {
			if cand.RequestID == approvalID || cand.ID == approvalID {
				p = cand
				break
			}
		}
	}
	if p != nil {
		delete(c.pendingAppr, p.ID)
	}
	c.mu.Unlock()
	if p == nil {
		return fmt.Errorf("no pending app-server approval %q", approvalID)
	}
	result, err := buildApprovalResult(p, accept)
	if err != nil {
		if !accept {
			if respErr := c.RespondError(p.RequestID, 4001, "user denied"); respErr != nil {
				c.mu.Lock()
				c.pendingAppr[p.ID] = p
				c.mu.Unlock()
				return respErr
			}
			return nil
		}
		c.mu.Lock()
		c.pendingAppr[p.ID] = p
		c.mu.Unlock()
		return err
	}
	if err := c.Respond(p.RequestID, result); err != nil {
		c.mu.Lock()
		c.pendingAppr[p.ID] = p
		c.mu.Unlock()
		return err
	}
	return nil
}

// PendingApprovalFor returns a phone-facing snapshot for wire/session id if any.
func (c *CodexAppServer) PendingApprovalFor(sessionOrThreadID string) *ApprovalSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := strings.TrimSpace(sessionOrThreadID)
	canon := c.resolveThreadIDLocked(id)
	var best *PendingApproval
	for _, p := range c.pendingAppr {
		if p.ThreadID == id || p.WireID == id || p.ThreadID == canon || p.WireID == canon {
			if best == nil || p.CreatedAt.After(best.CreatedAt) {
				best = p
			}
		}
	}
	if best == nil {
		return nil
	}
	return &ApprovalSnapshot{
		ID:          best.ID,
		ToolName:    best.ToolName,
		Description: best.Description,
		ThreadID:    best.ThreadID,
		TurnID:      best.TurnID,
		WireID:      best.WireID,
	}
}

// HasPendingApproval reports whether approvalID is tracked.
func (c *CodexAppServer) HasPendingApproval(approvalID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.pendingAppr[approvalID]; ok {
		return true
	}
	for _, p := range c.pendingAppr {
		if p.RequestID == approvalID || p.ID == approvalID {
			return true
		}
	}
	return false
}

// ActiveTurnID returns the current turn id for a thread/session alias.
func (c *CodexAppServer) ActiveTurnID(sessionOrThreadID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activeTurnIDLocked(sessionOrThreadID)
}

// LastTurnStatus returns the latest positive app-server turn status for an alias.
func (c *CodexAppServer) LastTurnStatus(sessionOrThreadID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastTurnStatusLocked(sessionOrThreadID)
}

func (c *CodexAppServer) lastTurnStatusLocked(sessionOrThreadID string) string {
	id := strings.TrimSpace(sessionOrThreadID)
	if id == "" {
		return ""
	}
	if status := c.turnStatus[id]; status != "" {
		return status
	}
	canon := c.resolveThreadIDLocked(id)
	if status := c.turnStatus[canon]; status != "" {
		return status
	}
	for key, status := range c.turnStatus {
		if c.resolveThreadIDLocked(key) == canon && status != "" {
			return status
		}
	}
	return ""
}

func (c *CodexAppServer) waitForStartedTurn(ctx context.Context, threadID, turnID string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		c.mu.Lock()
		current := c.activeTurnIDLocked(threadID)
		status := c.lastTurnStatusLocked(threadID)
		c.mu.Unlock()
		if current == "" {
			return fmt.Errorf("no active turn id for thread %s", threadID)
		}
		if current != turnID {
			return fmt.Errorf("active turn changed for thread %s", threadID)
		}
		if strings.EqualFold(status, "inProgress") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *CodexAppServer) activeTurnIDLocked(sessionOrThreadID string) string {
	id := strings.TrimSpace(sessionOrThreadID)
	if id == "" {
		return ""
	}
	if t := c.activeTurn[id]; t != "" {
		return t
	}
	canon := c.resolveThreadIDLocked(id)
	if t := c.activeTurn[canon]; t != "" {
		return t
	}
	for k, v := range c.activeTurn {
		if c.resolveThreadIDLocked(k) == canon && v != "" {
			return v
		}
	}
	return ""
}

// SetActiveTurn records the live turn id for a thread and aliases.
func (c *CodexAppServer) SetActiveTurn(threadID, turnID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}
	canon := c.resolveThreadIDLocked(threadID)
	if canon == "" {
		canon = threadID
		c.threadAlias[threadID] = threadID
	}
	c.activeTurn[canon] = turnID
	c.activeTurn[threadID] = turnID
	c.turnStatus[canon] = "inProgress"
	c.turnStatus[threadID] = "inProgress"
	if wire := c.wireByThread[canon]; wire != "" {
		c.activeTurn[wire] = turnID
		c.turnStatus[wire] = "inProgress"
	}
}

func (c *CodexAppServer) setStartingTurn(threadID, turnID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}
	if c.activeTurnIDLocked(threadID) == turnID && strings.EqualFold(c.lastTurnStatusLocked(threadID), "inProgress") {
		return
	}
	canon := c.resolveThreadIDLocked(threadID)
	if canon == "" {
		canon = threadID
		c.threadAlias[threadID] = threadID
	}
	c.activeTurn[canon] = turnID
	c.activeTurn[threadID] = turnID
	c.turnStatus[canon] = "starting"
	c.turnStatus[threadID] = "starting"
	if wire := c.wireByThread[canon]; wire != "" {
		c.activeTurn[wire] = turnID
		c.turnStatus[wire] = "starting"
	}
}

// ClearActiveTurn clears turn tracking when a turn completes/interrupts.
func (c *CodexAppServer) ClearActiveTurn(threadID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	canon := c.resolveThreadIDLocked(threadID)
	for k := range c.activeTurn {
		if k == threadID || k == canon || c.resolveThreadIDLocked(k) == canon {
			delete(c.activeTurn, k)
		}
	}
}

func (c *CodexAppServer) completeTurn(threadID, turnID, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	status = strings.TrimSpace(status)
	canon := c.resolveThreadIDLocked(threadID)
	current := c.activeTurnIDLocked(threadID)
	if current != "" && turnID != "" && turnID != current {
		return
	}
	for key := range c.activeTurn {
		if key == threadID || key == canon || c.resolveThreadIDLocked(key) == canon {
			delete(c.activeTurn, key)
		}
	}
	if status != "" {
		c.turnStatus[canon] = status
		c.turnStatus[threadID] = status
		if wire := c.wireByThread[canon]; wire != "" {
			c.turnStatus[wire] = status
		}
	}
	for id, approval := range c.pendingAppr {
		approvalCanon := c.resolveThreadIDLocked(approval.ThreadID)
		if approvalCanon == canon && (turnID == "" || approval.TurnID == "" || approval.TurnID == turnID) {
			delete(c.pendingAppr, id)
		}
	}
}

// RegisterThreadIDs records thread/session aliases and preferred wire id.
func (c *CodexAppServer) RegisterThreadIDs(threadID, sessionID, wireID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerThreadIDsLocked(threadID, sessionID, wireID)
}

func (c *CodexAppServer) registerThreadIDsLocked(threadID, sessionID, wireID string) {
	threadID = strings.TrimSpace(threadID)
	sessionID = strings.TrimSpace(sessionID)
	wireID = strings.TrimSpace(wireID)
	canon := threadID
	if canon == "" {
		canon = sessionID
	}
	if canon == "" {
		canon = wireID
	}
	if canon == "" {
		return
	}
	c.threadAlias[canon] = canon
	if threadID != "" {
		c.threadAlias[threadID] = canon
	}
	if sessionID != "" {
		c.threadAlias[sessionID] = canon
	}
	if wireID != "" {
		c.threadAlias[wireID] = canon
		c.wireByThread[canon] = wireID
	} else if sessionID != "" {
		c.wireByThread[canon] = sessionID
	} else {
		c.wireByThread[canon] = canon
	}
}

// ResolveThreadID maps a phone/session id to the canonical app-server thread id.
func (c *CodexAppServer) ResolveThreadID(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resolveThreadIDLocked(id)
}

// WireIDForThread maps an app-server thread id back to the public session id.
func (c *CodexAppServer) WireIDForThread(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	canon := c.resolveThreadIDLocked(id)
	if wire := c.wireByThread[canon]; wire != "" {
		return wire
	}
	return id
}

func (c *CodexAppServer) resolveThreadIDLocked(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if canon := c.threadAlias[id]; canon != "" {
		return canon
	}
	return id
}

// TrackServerRequest records approval/user-input server requests for later phone decisions.
func (c *CodexAppServer) TrackServerRequest(req ServerRequest) *PendingApproval {
	if !isApprovalMethod(req.Method) {
		return nil
	}
	p := pendingFromServerRequest(req)
	if p == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.ThreadID != "" {
		canon := c.resolveThreadIDLocked(p.ThreadID)
		c.registerThreadIDsLocked(p.ThreadID, "", "")
		if wire := c.wireByThread[canon]; wire != "" {
			p.WireID = wire
		} else {
			p.WireID = p.ThreadID
		}
		if p.TurnID != "" {
			c.activeTurn[canon] = p.TurnID
			c.activeTurn[p.ThreadID] = p.TurnID
			c.turnStatus[canon] = "inProgress"
			c.turnStatus[p.ThreadID] = "inProgress"
			if p.WireID != "" {
				c.activeTurn[p.WireID] = p.TurnID
				c.turnStatus[p.WireID] = "inProgress"
			}
		}
	}
	c.pendingAppr[p.ID] = p
	return p
}

// HandleNotification updates turn tracking from app-server notifications.
func (c *CodexAppServer) HandleNotification(method string, params json.RawMessage) {
	switch method {
	case "turn/started":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     *struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil && p.ThreadID != "" && p.Turn != nil && p.Turn.ID != "" {
			c.SetActiveTurn(p.ThreadID, p.Turn.ID)
		}
	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil && p.ThreadID != "" {
			if p.Turn == nil {
				c.completeTurn(p.ThreadID, "", "")
			} else {
				c.completeTurn(p.ThreadID, p.Turn.ID, p.Turn.Status)
			}
		}
	case "thread/started":
		var p struct {
			Thread *struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionId"`
			} `json:"thread"`
		}
		if json.Unmarshal(params, &p) == nil && p.Thread != nil {
			c.RegisterThreadIDs(p.Thread.ID, p.Thread.SessionID, "")
		}
	}
}

// ParseAppServerOutputNotification converts app-server streaming/final notifications
// into the normalized message types understood by the daemon output pipeline.
func ParseAppServerOutputNotification(method string, params json.RawMessage) (AppServerOutput, bool) {
	type deltaParams struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Delta    string `json:"delta"`
	}

	switch method {
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta":
		var p deltaParams
		if json.Unmarshal(params, &p) != nil || p.ThreadID == "" || p.ItemID == "" || p.Delta == "" {
			return AppServerOutput{}, false
		}
		messageType := "assistant"
		messageID := p.ItemID
		if method == "item/reasoning/summaryTextDelta" {
			messageType = "thinking"
			messageID += ":summary"
		}
		return AppServerOutput{
			ThreadID:  p.ThreadID,
			TurnID:    p.TurnID,
			MessageID: messageID,
			Type:      messageType,
			Content:   p.Delta,
			Delta:     true,
		}, true

	case "item/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Item     struct {
				ID      string   `json:"id"`
				Type    string   `json:"type"`
				Text    string   `json:"text"`
				Summary []string `json:"summary"`
				Content []string `json:"content"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID == "" || p.Item.ID == "" {
			return AppServerOutput{}, false
		}
		switch p.Item.Type {
		case "agentMessage":
			if p.Item.Text == "" {
				return AppServerOutput{}, false
			}
			return AppServerOutput{
				ThreadID:  p.ThreadID,
				TurnID:    p.TurnID,
				MessageID: p.Item.ID,
				Type:      "assistant",
				Content:   p.Item.Text,
				Final:     true,
			}, true
		case "reasoning":
			parts := p.Item.Summary
			messageID := p.Item.ID + ":summary"
			if len(parts) == 0 {
				parts = p.Item.Content
				messageID = p.Item.ID + ":content"
			}
			content := strings.Join(parts, "\n")
			if content == "" {
				return AppServerOutput{}, false
			}
			return AppServerOutput{
				ThreadID:  p.ThreadID,
				TurnID:    p.TurnID,
				MessageID: messageID,
				Type:      "thinking",
				Content:   content,
				Final:     true,
			}, true
		}

	case "error":
		var p struct {
			ThreadID  string `json:"threadId"`
			TurnID    string `json:"turnId"`
			WillRetry bool   `json:"willRetry"`
			Error     *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID == "" || p.WillRetry || p.Error == nil || p.Error.Message == "" {
			return AppServerOutput{}, false
		}
		return AppServerOutput{
			ThreadID:  p.ThreadID,
			TurnID:    p.TurnID,
			MessageID: p.TurnID + ":error",
			Type:      "error",
			Content:   p.Error.Message,
			Final:     true,
		}, true

	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID == "" || p.Turn == nil || p.Turn.Status != "failed" || p.Turn.Error == nil || p.Turn.Error.Message == "" {
			return AppServerOutput{}, false
		}
		return AppServerOutput{
			ThreadID:  p.ThreadID,
			TurnID:    p.Turn.ID,
			MessageID: p.Turn.ID + ":error",
			Type:      "error",
			Content:   p.Turn.Error.Message,
			Final:     true,
		}, true
	}

	return AppServerOutput{}, false
}

// IsTurnActive reports whether app-server currently tracks an active turn.
func (c *CodexAppServer) IsTurnActive(sessionOrThreadID string) bool {
	return c.ActiveTurnID(sessionOrThreadID) != ""
}

// ProbeMethods reports app-server readiness after initialize.
func (c *CodexAppServer) ProbeMethods() map[string]bool {
	out := map[string]bool{
		"available": c.Available(),
	}
	if !out["available"] {
		return out
	}
	if err := c.Ensure(); err != nil {
		out["ensure"] = false
		out["initialize"] = false
		log.Printf("[codex-app-server] probe ensure: %v", err)
		return out
	}
	out["ensure"] = true
	out["initialize"] = true
	out["thread/start"] = true
	out["turn/start"] = true
	out["turn/interrupt"] = true
	out["turn/steer"] = true
	return out
}
func (c *CodexAppServer) readLoop() {
	for {
		c.mu.Lock()
		r := c.stdout
		running := c.running
		c.mu.Unlock()
		if !running || r == nil {
			return
		}
		line, err := r.ReadBytes(byte(10))
		if err != nil {
			return
		}
		line = bytesTrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int             `json:"code"`
				Message string          `json:"message"`
				Data    json.RawMessage `json:"data"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("[codex-app-server] skip non-json line: %s", truncateRPC(line, 120))
			continue
		}

		hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
		// Server-initiated request: id + method (+ typically params)
		if hasID && msg.Method != "" && msg.Result == nil && msg.Error == nil {
			reqID := rawIDString(msg.ID)
			req := ServerRequest{ID: reqID, Method: msg.Method, Params: msg.Params}
			if p := c.TrackServerRequest(req); p != nil {
				log.Printf("[codex-app-server] pending approval id=%s method=%s thread=%s", p.ID, p.Method, p.ThreadID)
			}
			c.mu.Lock()
			fn := c.onRequest
			c.mu.Unlock()
			if fn != nil {
				fn(req)
			}
			continue
		}

		// Notification (no id)
		if !hasID {
			if msg.Method != "" {
				c.HandleNotification(msg.Method, msg.Params)
				c.mu.Lock()
				fn := c.onNotify
				c.mu.Unlock()
				if fn != nil {
					fn(msg.Method, msg.Params)
				}
			}
			continue
		}

		// Response to our outbound call
		id := rawIDString(msg.ID)
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if msg.Error != nil {
			detail := msg.Error.Message
			if len(msg.Error.Data) > 0 {
				detail = detail + " " + truncateRPC(msg.Error.Data, 200)
			}
			ch <- rpcResult{Error: fmt.Errorf("rpc %d: %s", msg.Error.Code, detail)}
		} else {
			ch <- rpcResult{Result: msg.Result}
		}
		close(ch)
	}
}

func rawIDString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return fmt.Sprintf("%.0f", n)
	}
	return strings.Trim(string(raw), "\"")
}

func parseThreadStartResponse(raw json.RawMessage) (threadID, sessionID string) {
	var resp struct {
		Thread *struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
		} `json:"thread"`
		ID        string `json:"id"`
		ThreadID  string `json:"threadId"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", ""
	}
	if resp.Thread != nil {
		return resp.Thread.ID, resp.Thread.SessionID
	}
	if resp.ThreadID != "" {
		return resp.ThreadID, resp.SessionID
	}
	return resp.ID, resp.SessionID
}

func parseTurnID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var resp struct {
		Turn *struct {
			ID string `json:"id"`
		} `json:"turn"`
		TurnID string `json:"turnId"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ""
	}
	if resp.Turn != nil && resp.Turn.ID != "" {
		return resp.Turn.ID
	}
	if resp.TurnID != "" {
		return resp.TurnID
	}
	return resp.ID
}

func truncateRPC(raw json.RawMessage, n int) string {
	s := string(raw)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func isApprovalMethod(method string) bool {
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval",
		"item/tool/requestUserInput",
		"applyPatchApproval",
		"execCommandApproval":
		return true
	default:
		return false
	}
}
func pendingFromServerRequest(req ServerRequest) *PendingApproval {
	var meta struct {
		ThreadID       string          `json:"threadId"`
		TurnID         string          `json:"turnId"`
		ItemID         string          `json:"itemId"`
		ApprovalID     string          `json:"approvalId"`
		CallID         string          `json:"callId"`
		ConversationID string          `json:"conversationId"`
		Command        json.RawMessage `json:"command"`
		Cwd            string          `json:"cwd"`
		Reason         string          `json:"reason"`
		Permissions    json.RawMessage `json:"permissions"`
	}
	_ = json.Unmarshal(req.Params, &meta)
	threadID := meta.ThreadID
	if threadID == "" {
		threadID = meta.ConversationID
	}
	tool := "approval"
	desc := strings.TrimSpace(meta.Reason)
	switch req.Method {
	case "item/commandExecution/requestApproval", "execCommandApproval":
		tool = "command"
		if cmd := commandDescription(meta.Command); cmd != "" {
			if desc == "" {
				desc = cmd
			} else {
				desc = desc + ": " + cmd
			}
		}
	case "item/fileChange/requestApproval", "applyPatchApproval":
		tool = "file_change"
		if desc == "" {
			desc = "Codex requests file change approval"
		}
	case "item/permissions/requestApproval":
		tool = "permissions"
		if desc == "" {
			desc = "Codex requests additional permissions"
		}
	case "item/tool/requestUserInput":
		tool = "user_input"
		if desc == "" {
			desc = "Codex is waiting for user input"
		}
	}
	if desc == "" {
		desc = "Codex approval required"
	}
	id := strings.TrimSpace(meta.ApprovalID)
	if id == "" {
		id = strings.TrimSpace(meta.ItemID)
	}
	if id == "" {
		id = strings.TrimSpace(meta.CallID)
	}
	if id == "" {
		id = req.ID
	}
	phoneID := id
	if req.ID != "" && req.ID != id {
		phoneID = id + ":" + req.ID
	}
	return &PendingApproval{
		ID:          phoneID,
		RequestID:   req.ID,
		Method:      req.Method,
		ThreadID:    threadID,
		TurnID:      meta.TurnID,
		ToolName:    tool,
		Description: truncateString(desc, 240),
		Params:      append(json.RawMessage(nil), req.Params...),
		Permissions: append(json.RawMessage(nil), meta.Permissions...),
		CreatedAt:   time.Now(),
	}
}

func commandDescription(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return strings.TrimSpace(string(raw))
}

func buildApprovalResult(p *PendingApproval, accept bool) (any, error) {
	switch p.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if accept {
			return map[string]any{"decision": "accept"}, nil
		}
		return map[string]any{"decision": "decline"}, nil
	case "applyPatchApproval", "execCommandApproval":
		if accept {
			return map[string]any{"decision": "approved"}, nil
		}
		return map[string]any{"decision": "denied"}, nil
	case "item/permissions/requestApproval":
		if !accept {
			return nil, fmt.Errorf("permissions deny uses error response")
		}
		perms := any(map[string]any{})
		if len(p.Permissions) > 0 {
			var decoded any
			if json.Unmarshal(p.Permissions, &decoded) == nil {
				perms = decoded
			}
		}
		return map[string]any{
			"permissions": perms,
			"scope":       "turn",
		}, nil
	case "item/tool/requestUserInput":
		if !accept {
			return nil, fmt.Errorf("user input deny uses error response")
		}
		return map[string]any{"answers": map[string]any{}}, nil
	default:
		if accept {
			return map[string]any{"decision": "accept"}, nil
		}
		return map[string]any{"decision": "decline"}, nil
	}
}

func buildTurnInput(prompt string, files []attach.LocalFile) ([]map[string]any, error) {
	var input []map[string]any
	var extra []string
	for _, f := range files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}
		if isImageAttachment(f) {
			input = append(input, map[string]any{
				"type": "localImage",
				"path": path,
			})
			continue
		}
		name := f.Name
		if name == "" {
			name = filepath.Base(path)
		}
		extra = append(extra, fmt.Sprintf("Attached file: %s (%s)", name, path))
	}
	text := strings.TrimSpace(prompt)
	if len(extra) > 0 {
		if text != "" {
			text = text + "\n\n" + strings.Join(extra, "\n")
		} else {
			text = strings.Join(extra, "\n")
		}
	}
	if text != "" {
		input = append([]map[string]any{{"type": "text", "text": text}}, input...)
	}
	if len(input) == 0 {
		return nil, fmt.Errorf("turn input is empty")
	}
	return input, nil
}

func buildThreadStartParams(cwd string) map[string]any {
	params := map[string]any{
		"approvalPolicy":    "on-request",
		"approvalsReviewer": "user",
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	return params
}

func buildTurnStartParams(threadID string, input []map[string]any) map[string]any {
	return map[string]any{
		"threadId":          threadID,
		"input":             input,
		"approvalPolicy":    "on-request",
		"approvalsReviewer": "user",
	}
}

func isImageAttachment(f attach.LocalFile) bool {
	mime := strings.ToLower(strings.TrimSpace(f.MIME))
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(f.Path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
