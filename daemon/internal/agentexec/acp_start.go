package agentexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// ACPStartOptions describes the smallest ACP exchange needed to create a
// native session and submit its first prompt.  ACP owns the native ID; callers
// must still verify it against their native store before treating it as owned.
type ACPStartOptions struct {
	Command          string
	Args             []string
	Dir              string
	Prompt           string
	OnSessionCreated func(sessionID string)
	OnUpdate         func(sessionID string, update map[string]any)
	OnPromptResult   func(sessionID string, err error)
	OnExit           func(exitCode int)
}

// ACPStartResult distinguishes a spawned ACP subprocess from a prompt that was
// successfully written to that subprocess. Neither observation proves native
// store ownership.
type ACPStartResult struct {
	SessionID            string
	ProcessStarted       bool
	NativeCreatePossible bool
	PromptAccepted       bool
	Process              *ACPProcess
	PromptResult         <-chan error
}

type acpRemoteError struct {
	method string
	body   string
}

func (e *acpRemoteError) Error() string { return fmt.Sprintf("ACP %s: %s", e.method, e.body) }

// ACPProcess keeps a short-lived ACP connection alive while the first turn
// streams. It intentionally exposes only cancellation/termination primitives;
// follow-up turns continue through each adapter's existing resume path.
type ACPProcess struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	sessionID   string
	writeMu     sync.Mutex
	job         uintptr
	done        chan struct{}
	intentional bool
}

func (p *ACPProcess) SessionID() string { return p.sessionID }

func (p *ACPProcess) write(value any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.mu.Lock()
	stdin := p.stdin
	p.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("ACP stdin is closed")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = stdin.Write(append(encoded, '\n'))
	return err
}

// Cancel asks the ACP agent to stop the active first turn. A hard process stop
// remains the fallback used during daemon shutdown.
func (p *ACPProcess) Cancel() error {
	if p == nil || p.sessionID == "" {
		return fmt.Errorf("ACP session is not ready")
	}
	return p.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/cancel",
		"params":  map[string]any{"sessionId": p.sessionID},
	})
}

func (p *ACPProcess) Stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	p.mu.Lock()
	job := p.job
	p.job = 0
	p.mu.Unlock()
	if job != 0 {
		releaseJobObject(job)
		return nil
	}
	return interruptProcess(p.cmd.Process)
}

func (p *ACPProcess) finishAfterPrompt() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.intentional = true
	stdin := p.stdin
	p.stdin = nil
	done := p.done
	p.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	go func() {
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = p.Stop()
		}
	}()
}

type acpEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// StartACPThread performs initialize -> session/new, writes session/prompt,
// then returns while the first turn keeps streaming. ACP answers
// session/prompt only when that turn ends, so waiting for it here would couple
// a potentially long-running turn to the coordinator's short start timeout.
// Merely writing the request is not enough for the phone to clear its draft;
// completion or rejection is reported asynchronously through OnPromptResult.
func StartACPThread(ctx context.Context, options ACPStartOptions) (ACPStartResult, error) {
	if options.Command == "" {
		return ACPStartResult{}, fmt.Errorf("ACP CLI path is empty")
	}
	executable, args, err := resolveLaunch(options.Command, options.Args)
	if err != nil {
		return ACPStartResult{}, fmt.Errorf("resolve ACP launcher: %w", err)
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = options.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ACPStartResult{}, fmt.Errorf("open ACP stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ACPStartResult{}, fmt.Errorf("open ACP stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ACPStartResult{}, fmt.Errorf("open ACP stderr: %w", err)
	}
	job, err := startManagedProcess(cmd)
	if err != nil {
		return ACPStartResult{}, fmt.Errorf("start ACP: %w", err)
	}

	result := ACPStartResult{ProcessStarted: true}
	promptResult := make(chan error, 1)
	result.PromptResult = promptResult
	var promptResultOnce sync.Once
	resolvePromptResult := func(err error) {
		promptResultOnce.Do(func() {
			promptResult <- err
			close(promptResult)
		})
	}
	process := &ACPProcess{cmd: cmd, stdin: stdin, job: job, done: make(chan struct{})}
	result.Process = process
	responses := make(map[string]chan acpEnvelope)
	var responsesMu sync.Mutex
	responseFor := func(id string) chan acpEnvelope {
		responsesMu.Lock()
		defer responsesMu.Unlock()
		ch := make(chan acpEnvelope, 1)
		responses[id] = ch
		return ch
	}
	deliver := func(id string, envelope acpEnvelope) {
		responsesMu.Lock()
		ch := responses[id]
		delete(responses, id)
		responsesMu.Unlock()
		if ch != nil {
			ch <- envelope
		}
	}
	go drainACPStderr(stderr)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			var envelope acpEnvelope
			if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
				continue
			}
			if len(envelope.ID) != 0 && envelope.Method == "" {
				var id string
				if json.Unmarshal(envelope.ID, &id) != nil {
					var number int
					if json.Unmarshal(envelope.ID, &number) == nil {
						id = fmt.Sprintf("%d", number)
					}
				}
				deliver(id, envelope)
				continue
			}
			if envelope.Method == "session/update" && options.OnUpdate != nil {
				var params struct {
					SessionID string         `json:"sessionId"`
					Update    map[string]any `json:"update"`
				}
				if json.Unmarshal(envelope.Params, &params) == nil && params.SessionID != "" {
					options.OnUpdate(params.SessionID, params.Update)
				}
			}
			// We do not advertise ACP client filesystem, terminal, or approval
			// capabilities. Explicitly reject reverse requests rather than let an
			// agent block indefinitely waiting for an unsupported feature.
			if len(envelope.ID) != 0 && envelope.Method != "" {
				_ = process.write(map[string]any{
					"jsonrpc": "2.0",
					"id":      json.RawMessage(envelope.ID),
					"error": map[string]any{
						"code":    -32601,
						"message": "NekoNest ACP client capability is not available",
					},
				})
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		process.mu.Lock()
		if process.job != 0 {
			releaseJobObject(process.job)
			process.job = 0
		}
		process.stdin = nil
		intentional := process.intentional
		process.mu.Unlock()
		close(process.done)
		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if err != nil {
			exitCode = -1
		}
		if intentional {
			exitCode = 0
		}
		if options.OnExit != nil {
			options.OnExit(exitCode)
		}
	}()

	call := func(id int, method string, params any) (acpEnvelope, error) {
		key := fmt.Sprintf("%d", id)
		response := responseFor(key)
		if err := process.write(map[string]any{
			"jsonrpc": "2.0", "id": id, "method": method, "params": params,
		}); err != nil {
			return acpEnvelope{}, err
		}
		validate := func(reply acpEnvelope) (acpEnvelope, error) {
			if len(reply.Error) != 0 && string(reply.Error) != "null" {
				return acpEnvelope{}, &acpRemoteError{method: method, body: string(reply.Error)}
			}
			return reply, nil
		}
		var timeout <-chan time.Time
		var timer *time.Timer
		if method != "session/prompt" {
			timer = time.NewTimer(5 * time.Second)
			timeout = timer.C
			defer timer.Stop()
		}
		select {
		case response := <-response:
			return validate(response)
		case <-ctx.Done():
			return acpEnvelope{}, ctx.Err()
		case <-process.done:
			select {
			case response := <-response:
				return validate(response)
			default:
				return acpEnvelope{}, fmt.Errorf("ACP %s process exited before responding", method)
			}
		case <-timeout:
			return acpEnvelope{}, fmt.Errorf("ACP %s timed out", method)
		}
	}

	initialize, err := call(1, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"clientInfo":         map[string]any{"name": "nekonest", "version": "dev"},
	})
	if err != nil {
		resolvePromptResult(err)
		_ = process.Stop()
		return result, err
	}
	var initialized struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if json.Unmarshal(initialize.Result, &initialized) != nil || initialized.ProtocolVersion != 1 {
		resolvePromptResult(errors.New("ACP initialize returned unsupported protocol"))
		_ = process.Stop()
		return result, fmt.Errorf("ACP initialize returned unsupported protocol")
	}
	// Once session/new is written, a transport failure or malformed response
	// cannot prove that the agent did not create a native session. An explicit
	// JSON-RPC rejection is the only definitive pre-creation failure here.
	result.NativeCreatePossible = true
	created, err := call(2, "session/new", map[string]any{
		"cwd": options.Dir, "mcpServers": []any{},
	})
	if err != nil {
		var remote *acpRemoteError
		if errors.As(err, &remote) {
			result.NativeCreatePossible = false
		}
		resolvePromptResult(err)
		_ = process.Stop()
		return result, err
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(created.Result, &session) != nil || session.SessionID == "" {
		resolvePromptResult(errors.New("ACP session/new returned no sessionId"))
		_ = process.Stop()
		return result, fmt.Errorf("ACP session/new returned no sessionId")
	}
	process.sessionID = session.SessionID
	result.SessionID = session.SessionID
	if options.OnSessionCreated != nil {
		options.OnSessionCreated(session.SessionID)
	}
	promptResponse := responseFor("3")
	err = process.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": session.SessionID,
			"prompt":    []any{map[string]any{"type": "text", "text": options.Prompt}},
		},
	})
	if err != nil {
		resolvePromptResult(err)
		if options.OnPromptResult != nil {
			options.OnPromptResult(session.SessionID, err)
		}
		_ = process.Stop()
		return result, err
	}
	go func() {
		var promptErr error
		finishProcess := false
		select {
		case reply := <-promptResponse:
			finishProcess = true
			if len(reply.Error) != 0 && string(reply.Error) != "null" {
				promptErr = &acpRemoteError{method: "session/prompt", body: string(reply.Error)}
			}
		case <-process.done:
			// A response may have been delivered immediately before process exit.
			select {
			case reply := <-promptResponse:
				if len(reply.Error) != 0 && string(reply.Error) != "null" {
					promptErr = &acpRemoteError{method: "session/prompt", body: string(reply.Error)}
				}
			default:
				promptErr = fmt.Errorf("ACP session/prompt process exited before responding")
			}
		}
		if options.OnPromptResult != nil {
			options.OnPromptResult(session.SessionID, promptErr)
		}
		resolvePromptResult(promptErr)
		// The start-only ACP process is no longer needed after a terminal prompt
		// response. Mark shutdown intentional so a clean close never becomes a
		// false adapter error; crashed processes were already observed above.
		if finishProcess {
			process.finishAfterPrompt()
		}
	}()
	// The request was written, but ACP has not yet positively answered it. The
	// coordinator can confirm native ownership now while the phone safely keeps
	// the draft prompt until a future history refresh proves the turn exists.
	result.PromptAccepted = false
	return result, nil
}

func drainACPStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	for scanner.Scan() {
		// ACP stderr is diagnostic only. Existing agent commanders surface a
		// terminal process error through their normal output sink when needed.
	}
}

// ProbeACPStart performs only the non-mutating initialize handshake. It does
// not create a session, so adapters may use it when advertising start support.
func ProbeACPStart(ctx context.Context, command string, args []string, dir string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// StartACPThread would create a session, so probe through a small local ACP
	// server exchange that writes initialize then stops after its response.
	executable, launchArgs, err := resolveLaunch(command, args)
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, launchArgs...)
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	job, err := startManagedProcess(cmd)
	if err != nil {
		return err
	}
	defer func() {
		releaseJobObject(job)
		_ = interruptProcess(cmd.Process)
		_ = cmd.Wait()
	}()
	request := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": 1, "clientCapabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "nekonest", "version": "dev"},
		},
	}
	encoded, _ := json.Marshal(request)
	if _, err := stdin.Write(append(encoded, '\n')); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	line := make(chan []byte, 1)
	go func() {
		if scanner.Scan() {
			line <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	select {
	case raw := <-line:
		var response struct {
			Result struct {
				ProtocolVersion int `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Result.ProtocolVersion != 1 {
			return fmt.Errorf("ACP initialize did not negotiate protocol v1")
		}
		return nil
	case <-probeCtx.Done():
		return probeCtx.Err()
	}
}
