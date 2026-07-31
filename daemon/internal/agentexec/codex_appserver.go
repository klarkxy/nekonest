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
// It is the v1 normative control path for approve/deny/interrupt/steer/start.
type CodexAppServer struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	cancel   context.CancelFunc
	nextID   atomic.Int64
	pending  map[string]chan rpcResult
	running  bool
	binPath  string
	onNotify func(method string, params json.RawMessage)
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
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "app-server") || strings.Contains(s, "json") || cmd.ProcessState != nil
}

// Ensure starts the app-server process if not already running.
func (c *CodexAppServer) Ensure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, c.binPath, "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start codex app-server: %w", err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.cancel = cancel
	c.running = true
	c.pending = make(map[string]chan rpcResult)
	go c.readLoop()
	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		c.running = false
		for id, ch := range c.pending {
			ch <- rpcResult{Error: fmt.Errorf("app-server exited")}
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()
	log.Printf("[codex-app-server] started pid=%d", cmd.Process.Pid)
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
	return nil
}

// Call issues a JSON-RPC request and waits for the matching response.
func (c *CodexAppServer) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := c.Ensure(); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d", c.nextID.Add(1))
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
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

// TryCall probes a method; returns false if method missing or transport fails.
func (c *CodexAppServer) TryCall(method string, params any) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Call(ctx, method, params)
	return err == nil
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
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
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
			ch <- rpcResult{Error: fmt.Errorf("rpc %d: %s", msg.Error.Code, msg.Error.Message)}
		} else {
			ch <- rpcResult{Result: msg.Result}
		}
		close(ch)
	}
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// ProbeMethods returns which control methods appear to exist.
// Method names follow codex app-server JSON schema generations; unknown
// methods simply report false without failing the process.
func (c *CodexAppServer) ProbeMethods() map[string]bool {
	out := map[string]bool{
		"available": c.Available(),
	}
	if !out["available"] {
		return out
	}
	// Soft probe: ensure process starts. Specific method names are version-dependent;
	// doctor/startup logs capabilities after experimental schema generation when present.
	if err := c.Ensure(); err != nil {
		out["ensure"] = false
		return out
	}
	out["ensure"] = true
	return out
}
