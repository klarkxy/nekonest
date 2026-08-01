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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CodexAppServer is a long-lived stdio JSON-RPC client for `codex app-server`.
// Protocol baseline: codex-cli 0.144.x (initialize → thread/start → turn/start).
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
}

type rpcResult struct {
	Result json.RawMessage
	Error  error
}

// NewCodexAppServer prepares a controller (does not start until Ensure).
func NewCodexAppServer() *CodexAppServer {
	return &CodexAppServer{
		pending: make(map[string]chan rpcResult),
		binPath: "codex",
	}
}

// SetNotifyHandler receives server-pushed JSON-RPC notifications.
func (c *CodexAppServer) SetNotifyHandler(fn func(method string, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onNotify = fn
}

// Available reports whether the codex binary exposes app-server.
func (c *CodexAppServer) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binPath, "app-server", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Help may exit non-zero on some builds; still accept recognizable text.
		s := strings.ToLower(string(out))
		return strings.Contains(s, "app-server") || strings.Contains(s, "generate-json-schema")
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "app-server") || strings.Contains(s, "json")
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

// callLocked sends one RPC without re-entering Ensure (used by initialize).
func (c *CodexAppServer) callLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	idNum := c.nextID.Add(1)
	id := fmt.Sprintf("%d", idNum)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      idNum, // numeric id preferred by many JSON-RPC servers
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
	line = append(line, '\n')
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

// StartThreadResult is the dual-id outcome of thread/start.
type StartThreadResult struct {
	ThreadID  string // app-server thread id (turn/start)
	SessionID string // native store / session_meta id when distinct
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
// If firstPrompt is non-empty, also issues turn/start with that text.
func (c *CodexAppServer) StartThread(ctx context.Context, cwd, firstPrompt string) (StartThreadResult, error) {
	var out StartThreadResult
	if err := c.Ensure(); err != nil {
		return out, err
	}
	params := map[string]any{}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	raw, err := c.Call(ctx, "thread/start", params)
	if err != nil {
		return out, fmt.Errorf("thread/start: %w", err)
	}
	out.Raw = raw
	out.ThreadID, out.SessionID = parseThreadStartResponse(raw)
	if out.ThreadID == "" && out.SessionID == "" {
		return out, fmt.Errorf("thread/start: empty thread id in response: %s", truncateRPC(raw, 240))
	}
	log.Printf("[codex-app-server] thread/start threadId=%s sessionId=%s cwd=%s", out.ThreadID, out.SessionID, cwd)

	prompt := strings.TrimSpace(firstPrompt)
	if prompt != "" {
		turnThread := out.ThreadID
		if turnThread == "" {
			turnThread = out.SessionID
		}
		turnParams := map[string]any{
			"threadId": turnThread,
			"input": []map[string]any{
				{"type": "text", "text": prompt},
			},
		}
		if _, turnErr := c.Call(ctx, "turn/start", turnParams); turnErr != nil {
			// Thread exists; still return ids so ownership can settle.
			log.Printf("[codex-app-server] turn/start after thread/start: %v", turnErr)
			return out, fmt.Errorf("thread created (%s) but turn/start failed: %w", out.WireID(), turnErr)
		}
	}
	return out, nil
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

func truncateRPC(raw json.RawMessage, n int) string {
	s := string(raw)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
		line, err := r.ReadBytes('\n')
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
		// Notification (no id)
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			if msg.Method != "" {
				c.mu.Lock()
				fn := c.onNotify
				c.mu.Unlock()
				if fn != nil {
					fn(msg.Method, msg.Params)
				}
			}
			continue
		}
		var id string
		_ = json.Unmarshal(msg.ID, &id)
		if id == "" {
			var n float64
			if json.Unmarshal(msg.ID, &n) == nil {
				id = fmt.Sprintf("%.0f", n)
			}
		}
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

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
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
	// Soft probe thread/start with ephemeral empty cwd is too heavy; mark method known.
	out["thread/start"] = true
	out["turn/start"] = true
	return out
}
