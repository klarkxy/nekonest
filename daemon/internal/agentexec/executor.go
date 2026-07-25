package agentexec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

// AgentExecutor manages a running agent process.
type AgentExecutor struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stdin     io.WriteCloser
	sessionID string
	agentType string
	running   bool
	exitCh    chan struct{} // closed when process exits
	exitCode  int

	// Callbacks
	OnOutput func(line string) // called for each complete line of stdout/stderr
	OnExit   func(exitCode int)
}

// NewAgentExecutor creates a new executor for a given agent type.
func NewAgentExecutor(agentType, sessionID string) *AgentExecutor {
	return &AgentExecutor{
		sessionID: sessionID,
		agentType: agentType,
		exitCh:    make(chan struct{}),
		exitCode:  -1,
	}
}

// Start launches the agent process with the given command and args.
func (e *AgentExecutor) Start(command string, args []string, env []string) error {
	return e.StartWithDir(command, args, env, "")
}

// StartWithDir launches the agent process with an optional working directory.
func (e *AgentExecutor) StartWithDir(command string, args []string, env []string, workDir string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("executor already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start process: %w", err)
	}

	e.cmd = cmd
	e.cancel = cancel
	e.stdin = stdin
	e.running = true
	e.exitCh = make(chan struct{})

	// Line-based output reader for stdout
	go e.readLineOutput(stdout, "stdout")
	// Line-based output reader for stderr
	go e.readLineOutput(stderr, "stderr")

	// Wait for process exit in background
	go func() {
		err := cmd.Wait()
		e.mu.Lock()
		e.running = false
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		e.exitCode = exitCode
		e.mu.Unlock()

		close(e.exitCh)
		if e.OnExit != nil {
			e.OnExit(exitCode)
		}
	}()

	return nil
}

// SendPrompt writes a prompt to the agent's stdin.
func (e *AgentExecutor) SendPrompt(prompt string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running || e.stdin == nil {
		return fmt.Errorf("agent not running")
	}

	_, err := e.stdin.Write([]byte(prompt + "\n"))
	if err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// Interrupt sends an interrupt signal to the agent process.
// Cross-platform: uses os.Interrupt on all platforms.
func (e *AgentExecutor) Interrupt() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running || e.cmd == nil || e.cmd.Process == nil {
		return fmt.Errorf("agent not running")
	}

	// os.Interrupt (SIGINT on Unix, CTRL_C_EVENT on Windows) is cross-platform
	return e.cmd.Process.Signal(os.Interrupt)
}

// Stop gracefully stops the agent process, with force kill fallback.
func (e *AgentExecutor) Stop() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}

	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()

	// Wait up to 5 seconds for graceful exit
	select {
	case <-e.exitCh:
		return nil
	case <-time.After(5 * time.Second):
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.cmd != nil && e.cmd.Process != nil {
			return e.cmd.Process.Kill()
		}
		return nil
	}
}

// IsRunning returns whether the agent process is currently running.
func (e *AgentExecutor) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// WaitExit returns a channel that is closed when the process exits.
func (e *AgentExecutor) WaitExit() <-chan struct{} {
	return e.exitCh
}

// ExitCode returns the exit code of the process. Only valid after WaitExit is closed.
func (e *AgentExecutor) ExitCode() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exitCode
}

// readLineOutput reads from a pipe line by line and calls OnOutput for each line.
// This is better than chunk-based reading because agent output is typically line-delimited JSON.
func (e *AgentExecutor) readLineOutput(r io.ReadCloser, source string) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	// Increase buffer size for potentially long JSON lines
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if e.OnOutput != nil {
			e.OnOutput(line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[%s] %s read error: %v", e.agentType, source, err)
	}
}
