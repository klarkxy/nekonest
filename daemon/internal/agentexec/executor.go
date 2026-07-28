package agentexec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
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
	// platformJob is a Windows Job Object handle (kill-on-close process tree).
	platformJob uintptr

	// Callbacks
	OnOutput       func(line string)                // compatibility callback for stdout/stderr
	OnOutputSource func(source string, line string) // source is "stdout" or "stderr"
	OnExit         func(exitCode int)
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
	cmdPath, cmdArgs, err := resolveLaunch(command, args)
	if err != nil {
		cancel()
		return err
	}
	cmd := exec.CommandContext(ctx, cmdPath, cmdArgs...)
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

	// On Windows this creates the process suspended, assigns it to a
	// kill-on-close Job Object, then resumes it. Other platforms call Start.
	job, err := startManagedProcess(cmd)
	if err != nil {
		cancel()
		return fmt.Errorf("start process: %w", err)
	}

	e.cmd = cmd
	e.cancel = cancel
	e.stdin = stdin
	e.running = true
	exitCh := make(chan struct{})
	e.exitCh = exitCh
	e.exitCode = -1
	e.platformJob = job

	// Line-based output reader for stdout
	go e.readLineOutput(stdout, "stdout")
	// Line-based output reader for stderr
	go e.readLineOutput(stderr, "stderr")

	// Wait for process exit in background
	go func() {
		err := cmd.Wait()
		e.mu.Lock()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		jobToRelease := uintptr(0)
		if e.cmd == cmd && e.exitCh == exitCh {
			e.exitCode = exitCode
			jobToRelease = e.platformJob
			e.platformJob = 0
			// Close this run's local channel before making the executor
			// restartable. A new Start cannot replace e.exitCh while the lock
			// is held, so an old waiter can never close a newer run's channel.
			close(exitCh)
			e.running = false
		}
		e.mu.Unlock()

		releaseJobObject(jobToRelease)
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

// CloseStdin closes the process stdin. Call after Start when the prompt is
// already on argv (kilo run / claude -p / codex exec) so the CLI does not
// block waiting for piped input.
func (e *AgentExecutor) CloseStdin() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stdin == nil {
		return nil
	}
	err := e.stdin.Close()
	e.stdin = nil
	return err
}

// Interrupt stops the agent process tree (Windows Job Object / SIGINT elsewhere).
func (e *AgentExecutor) Interrupt() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running || e.cmd == nil || e.cmd.Process == nil {
		return fmt.Errorf("agent not running")
	}
	if e.platformJob != 0 {
		releaseJobObject(e.platformJob)
		e.platformJob = 0
		return nil
	}
	if err := interruptProcess(e.cmd.Process); err != nil {
		return e.cmd.Process.Kill()
	}
	return nil
}

// Stop gracefully stops the agent process tree, with force kill fallback.
func (e *AgentExecutor) Stop() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}

	job := e.platformJob
	e.platformJob = 0
	proc := e.cmd
	cancel := e.cancel
	exitCh := e.exitCh
	e.mu.Unlock()

	// Closing the job with KILL_ON_JOB_CLOSE terminates the whole tree.
	if job != 0 {
		releaseJobObject(job)
	} else if proc != nil && proc.Process != nil {
		// A Job Object may be unavailable under restrictive host policies.
		// Terminate the tree while the parent PID still exists; cancelling the
		// CommandContext first could orphan its children.
		_ = interruptProcess(proc.Process)
	}
	if cancel != nil {
		cancel()
	}

	// Wait up to 5 seconds for graceful exit
	select {
	case <-exitCh:
		return nil
	case <-time.After(5 * time.Second):
		if proc != nil && proc.Process != nil {
			return proc.Process.Kill()
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
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exitCh
}

// ExitCode returns the exit code of the process. Only valid after WaitExit is closed.
func (e *AgentExecutor) ExitCode() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exitCode
}

// StdinOpen reports whether the process still accepts stdin writes.
func (e *AgentExecutor) StdinOpen() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running && e.stdin != nil
}

// readLineOutput reads from a pipe line by line and calls OnOutput for each line.
// This is better than chunk-based reading because agent output is typically line-delimited JSON.
func (e *AgentExecutor) readLineOutput(r io.ReadCloser, source string) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	// Kilo/Codex JSONL tool events can be multi‑MB single lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if e.OnOutputSource != nil {
			e.OnOutputSource(source, line)
		} else if e.OnOutput != nil {
			e.OnOutput(line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[%s] %s read error: %v", e.agentType, source, err)
	}
}
