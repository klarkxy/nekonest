package agentexec

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestACPFixtureProcess is launched as a child test binary. It is a stdio-only
// fixture: no installed Kimi CLI or native store is needed.
func TestACPFixtureProcess(t *testing.T) {
	mode := os.Getenv("NEKONEST_ACP_FIXTURE")
	if mode == "" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &request)
		switch request.Method {
		case "initialize":
			version := 1
			if mode == "bad-probe" {
				version = 2
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": version}})
		case "session/new":
			if mode == "partial" {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -1, "message": "new rejected"}})
				return
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"sessionId": "native-acp-id"}})
		case "session/prompt":
			if mode == "prompt-reject" {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -2, "message": "prompt rejected"}})
				return
			}
			if mode == "slow-prompt" {
				time.Sleep(2 * time.Second)
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "method": "session/update",
				"params": map[string]any{
					"sessionId": "native-acp-id",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk", "messageId": "message-1",
						"content": map[string]any{"type": "text", "text": "fixture response"},
					},
				},
			})
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "end_turn"}})
			if mode != "linger" {
				return
			}
		}
	}
}

func TestStartACPThreadStopsStartOnlyProcessAfterPromptResponse(t *testing.T) {
	exited := make(chan int, 1)
	promptResult := make(chan error, 1)
	options := acpFixtureOptions(t, "linger")
	options.Prompt = "first prompt"
	options.OnExit = func(code int) { exited <- code }
	options.OnPromptResult = func(_ string, err error) { promptResult <- err }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := StartACPThread(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.PromptAccepted {
		t.Fatalf("start result = %#v", result)
	}
	select {
	case promptErr := <-promptResult:
		if promptErr != nil {
			t.Fatalf("prompt result: %v", promptErr)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive ACP prompt result")
	}
	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("intentional ACP completion exit code = %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ACP start-only process remained alive after prompt response")
	}
}

func acpFixtureOptions(t *testing.T, mode string) ACPStartOptions {
	t.Helper()
	t.Setenv("NEKONEST_ACP_FIXTURE", mode)
	return ACPStartOptions{Command: os.Args[0], Args: []string{"-test.run=TestACPFixtureProcess"}}
}

func TestStartACPThreadWritesFirstPromptAndStreamsUpdate(t *testing.T) {
	updates := make(chan map[string]any, 1)
	promptResult := make(chan error, 1)
	options := acpFixtureOptions(t, "ok")
	options.Prompt = "first prompt"
	options.OnUpdate = func(_ string, update map[string]any) { updates <- update }
	options.OnPromptResult = func(_ string, err error) { promptResult <- err }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := StartACPThread(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ProcessStarted || !result.NativeCreatePossible || result.PromptAccepted || result.SessionID != "native-acp-id" {
		t.Fatalf("start result = %#v", result)
	}
	select {
	case update := <-updates:
		if update["sessionUpdate"] != "agent_message_chunk" {
			t.Fatalf("update = %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive ACP session update")
	}
	select {
	case promptErr := <-promptResult:
		if promptErr != nil {
			t.Fatalf("prompt result: %v", promptErr)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive ACP prompt result")
	}
}

func TestStartACPThreadReportsPartialStart(t *testing.T) {
	options := acpFixtureOptions(t, "partial")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := StartACPThread(ctx, options)
	if err == nil {
		t.Fatal("expected session/new failure")
	}
	if !result.ProcessStarted || result.NativeCreatePossible || result.PromptAccepted || result.SessionID != "" {
		t.Fatalf("partial start result = %#v", result)
	}
}

func TestStartACPThreadRequiresPositivePromptResponse(t *testing.T) {
	promptResult := make(chan error, 1)
	options := acpFixtureOptions(t, "prompt-reject")
	options.Prompt = "first prompt"
	options.OnPromptResult = func(_ string, err error) { promptResult <- err }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := StartACPThread(ctx, options)
	if err != nil {
		t.Fatalf("session creation should return before prompt result: %v", err)
	}
	if !result.NativeCreatePossible || result.PromptAccepted || result.SessionID != "native-acp-id" {
		t.Fatalf("prompt rejection result = %#v", result)
	}
	select {
	case promptErr := <-promptResult:
		if promptErr == nil {
			t.Fatal("expected asynchronous session/prompt rejection")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive ACP prompt rejection")
	}
}

func TestStartACPThreadDoesNotTieFirstTurnToStartContext(t *testing.T) {
	promptResult := make(chan error, 1)
	options := acpFixtureOptions(t, "slow-prompt")
	options.Prompt = "long first prompt"
	options.OnPromptResult = func(_ string, err error) { promptResult <- err }
	ctx, cancel := context.WithCancel(context.Background())

	startedAt := time.Now()
	result, err := StartACPThread(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("start waited for the first turn: %v", elapsed)
	}
	if !result.NativeCreatePossible || result.PromptAccepted || result.SessionID != "native-acp-id" {
		t.Fatalf("start result = %#v", result)
	}
	cancel()
	select {
	case promptErr := <-promptResult:
		if promptErr != nil {
			t.Fatalf("long first turn was tied to expired start context: %v", promptErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long first turn did not finish independently")
	}
}

func TestProbeACPStartRejectsUnsupportedProtocol(t *testing.T) {
	options := acpFixtureOptions(t, "bad-probe")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := ProbeACPStart(ctx, options.Command, options.Args, ""); err == nil {
		t.Fatal("expected incompatible protocol to be rejected")
	}
}
