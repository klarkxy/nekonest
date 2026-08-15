package agentexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/attach"
)

func liveCodexWorkspace(t *testing.T) string {
	t.Helper()
	if configured := strings.TrimSpace(os.Getenv("NEKONEST_LIVE_CODEX_WORKSPACE")); configured != "" {
		workspace, err := filepath.Abs(configured)
		if err != nil {
			t.Fatal(err)
		}
		return workspace
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve live Codex smoke workspace")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
}

func liveCodexReportPath(t *testing.T, name string) string {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv("NEKONEST_LIVE_CODEX_REPORT_DIR"))
	if dir == "" {
		dir = t.TempDir()
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func TestLiveCodexAppServerSmoke(t *testing.T) {
	if os.Getenv("NEKONEST_LIVE_CODEX") != "1" {
		t.Skip("set NEKONEST_LIVE_CODEX=1 for live app-server smoke")
	}
	workspace := liveCodexWorkspace(t)
	reportPath := liveCodexReportPath(t, "live-appserver-smoke.json")
	report := map[string]any{"started_at": time.Now().Format(time.RFC3339)}
	defer func() {
		report["finished_at"] = time.Now().Format(time.RFC3339)
		b, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(reportPath, b, 0o644)
		t.Logf("live report written to %s", reportPath)
	}()

	c := NewCodexAppServer()
	defer c.Close()

	report["available"] = c.Available()
	if !c.Available() {
		t.Fatal("codex app-server not available")
	}
	if err := c.Ensure(); err != nil {
		report["ensure_error"] = err.Error()
		t.Fatalf("ensure: %v", err)
	}
	report["ensure"] = true
	report["initialized"] = c.Initialized()
	if !c.Initialized() {
		t.Fatal("expected initialized")
	}

	cwd := workspace
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := c.StartThread(ctx, cwd, "")
	if err != nil {
		report["start_error"] = err.Error()
		t.Fatalf("thread/start: %v", err)
	}
	report["threadId"] = res.ThreadID
	report["sessionId"] = res.SessionID
	report["wire"] = res.WireID()
	if res.WireID() == "" {
		t.Fatal("empty wire id")
	}

	turnID, err := c.StartTurn(ctx, res.WireID(), "Reply with exactly one word: pong", nil)
	if err != nil {
		report["turn_start_error"] = err.Error()
	} else {
		report["turn_start"] = true
		report["turnId"] = turnID
	}

	active := c.ActiveTurnID(res.WireID())
	report["active_turn"] = active
	if active == "" && turnID != "" {
		c.SetActiveTurn(res.WireID(), turnID)
		active = turnID
	}

	if active != "" {
		if err := c.InterruptTurn(ctx, res.WireID()); err != nil {
			report["interrupt_error"] = err.Error()
		} else {
			report["interrupt"] = true
		}
	} else {
		report["interrupt_skipped"] = "no active turn id"
	}

	if err := c.SteerTurn(ctx, res.WireID(), "noop"); err != nil {
		report["steer_error"] = err.Error()
		report["steer_failed_closed"] = true
	} else {
		report["steer_ok"] = true
	}

	params := fmt.Sprintf(`{"threadId":%q,"turnId":"turn-synth","itemId":"item-synth","command":"echo hi"}`, res.WireID())
	p := c.TrackServerRequest(ServerRequest{
		ID:     "1001",
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(params),
	})
	if p == nil {
		t.Fatal("expected synthetic pending approval")
	}
	report["synthetic_approval_id"] = p.ID
	resAccept, err := buildApprovalResult(p, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(resAccept)
	report["synthetic_accept"] = string(b)
	if string(b) != `{"decision":"accept"}` {
		t.Fatalf("accept payload %s", b)
	}
	probe := c.ProbeMethods()
	report["probe"] = probe
	if !probe["full_control"] {
		t.Fatalf("installed Codex does not expose the required 0.146 full-control surface: %#v", probe)
	}
}

func TestLiveCodexAppServerSteerSmoke(t *testing.T) {
	if os.Getenv("NEKONEST_LIVE_CODEX") != "1" {
		t.Skip("set NEKONEST_LIVE_CODEX=1 for live app-server smoke")
	}
	workspace := liveCodexWorkspace(t)
	reportPath := liveCodexReportPath(t, "live-appserver-steer-smoke.json")
	report := map[string]any{"started_at": time.Now().Format(time.RFC3339)}
	defer func() {
		report["finished_at"] = time.Now().Format(time.RFC3339)
		body, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(reportPath, body, 0o644)
		t.Logf("live report written to %s", reportPath)
	}()

	type liveEvent struct {
		method string
		output *AppServerOutput
		status string
	}
	events := make(chan liveEvent, 128)
	c := NewCodexAppServer()
	c.SetNotifyHandler(func(method string, params json.RawMessage) {
		event := liveEvent{method: method}
		if output, ok := ParseAppServerOutputNotification(method, params); ok {
			copy := output
			event.output = &copy
		}
		if method == "turn/completed" {
			var completed struct {
				Turn *struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(params, &completed)
			if completed.Turn != nil {
				event.status = completed.Turn.Status
			}
		}
		select {
		case events <- event:
		default:
		}
	})
	defer c.Close()
	if err := c.Ensure(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started, err := c.StartThread(ctx, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	report["thread_id"] = started.ThreadID
	turnID, err := c.StartTurn(
		ctx,
		started.WireID(),
		"NEKONEST_STEER_LIVE_BASE: Run PowerShell Start-Sleep -Seconds 20 without modifying files, then reply BASE_DONE.",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	report["turn_id"] = turnID
	if err := c.SteerTurn(
		ctx,
		started.WireID(),
		"Change the current task now: do not run or continue any sleep command; reply exactly NEKONEST_STEER_LIVE_OK and do nothing else.",
	); err != nil {
		report["steer_error"] = err.Error()
		t.Fatal(err)
	}
	report["steer_accepted"] = true

	var finalAssistant string
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.method == "turn/started" {
				report["turn_started_notification"] = true
			}
			if event.output != nil && event.output.Type == "assistant" && event.output.Final {
				finalAssistant = event.output.Content
			}
			if event.method != "turn/completed" {
				continue
			}
			report["turn_status"] = event.status
			report["assistant_final_seen"] = finalAssistant != ""
			markerSeen := strings.Contains(finalAssistant, "NEKONEST_STEER_LIVE_OK")
			report["steer_marker_seen"] = markerSeen
			if event.status != "completed" || !markerSeen {
				t.Fatalf("turn status=%q marker_seen=%v", event.status, markerSeen)
			}
			return
		case <-deadline.C:
			t.Fatal("timed out waiting for steered turn completion")
		}
	}
}

func TestLiveCodexAppServerApprovalSmoke(t *testing.T) {
	if os.Getenv("NEKONEST_LIVE_CODEX") != "1" {
		t.Skip("set NEKONEST_LIVE_CODEX=1 for live app-server smoke")
	}
	workspace := liveCodexWorkspace(t)
	reportPath := liveCodexReportPath(t, "live-appserver-approval-smoke.json")
	report := map[string]any{"started_at": time.Now().Format(time.RFC3339)}
	defer func() {
		report["finished_at"] = time.Now().Format(time.RFC3339)
		body, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(reportPath, body, 0o644)
		t.Logf("live report written to %s", reportPath)
	}()

	type approvalEvent struct {
		method string
		output *AppServerOutput
		status string
	}
	requests := make(chan ServerRequest, 8)
	events := make(chan approvalEvent, 256)
	c := NewCodexAppServer()
	c.SetRequestHandler(func(request ServerRequest) {
		select {
		case requests <- request:
		default:
		}
	})
	c.SetNotifyHandler(func(method string, params json.RawMessage) {
		event := approvalEvent{method: method}
		if output, ok := ParseAppServerOutputNotification(method, params); ok {
			copy := output
			event.output = &copy
		}
		if method == "turn/completed" {
			var completed struct {
				Turn *struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(params, &completed)
			if completed.Turn != nil {
				event.status = completed.Turn.Status
			}
		}
		select {
		case events <- event:
		default:
		}
	})
	defer c.Close()
	if err := c.Ensure(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	started, err := c.StartThread(ctx, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	report["thread_id"] = started.ThreadID

	run := func(label, prompt, marker string, accept bool) {
		t.Helper()
		result := map[string]any{}
		report[label] = result
		turnID, err := c.StartTurn(ctx, started.WireID(), prompt, nil)
		if err != nil {
			t.Fatal(err)
		}
		result["turn_id"] = turnID

		var request ServerRequest
		select {
		case request = <-requests:
			result["request_method"] = request.Method
		case <-time.After(45 * time.Second):
			t.Fatalf("%s: timed out waiting for approval request", label)
		}
		pending := c.PendingApprovalFor(started.WireID())
		if pending == nil {
			t.Fatalf("%s: request %s was not tracked", label, request.Method)
		}
		result["pending_tool"] = pending.ToolName
		if accept {
			err = c.ApprovePending(pending.ID)
			result["decision"] = "accept"
		} else {
			err = c.DenyPending(pending.ID)
			result["decision"] = "decline"
		}
		if err != nil {
			t.Fatal(err)
		}

		var finalAssistant string
		deadline := time.NewTimer(45 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case event := <-events:
				if event.output != nil && event.output.Type == "assistant" && event.output.Final {
					finalAssistant = event.output.Content
				}
				if event.method != "turn/completed" {
					continue
				}
				result["turn_status"] = event.status
				markerSeen := strings.Contains(finalAssistant, marker)
				result["marker_seen"] = markerSeen
				if event.status != "completed" || !markerSeen {
					t.Fatalf("%s: turn status=%q marker_seen=%v", label, event.status, markerSeen)
				}
				return
			case <-deadline.C:
				t.Fatalf("%s: timed out waiting for turn completion", label)
			}
		}
	}

	run(
		"approve",
		"Run PowerShell Write-Output APPROVAL_ACCEPT_SAFE with sandbox_permissions=require_escalated. Do not modify files. After approval reply exactly NEKONEST_APPROVAL_ACCEPT_OK.",
		"NEKONEST_APPROVAL_ACCEPT_OK",
		true,
	)
	run(
		"deny",
		"Request PowerShell Write-Output APPROVAL_DENY_SAFE with sandbox_permissions=require_escalated. Do not modify files. If denied, do not retry and reply exactly NEKONEST_APPROVAL_DENY_OK.",
		"NEKONEST_APPROVAL_DENY_OK",
		false,
	)
}

func TestLiveCodexAppServerUserInputSmoke(t *testing.T) {
	if os.Getenv("NEKONEST_LIVE_CODEX") != "1" {
		t.Skip("set NEKONEST_LIVE_CODEX=1 for live app-server smoke")
	}
	workspace := liveCodexWorkspace(t)
	requests := make(chan ServerRequest, 8)
	events := make(chan struct {
		method string
		output *AppServerOutput
		status string
	}, 256)
	c := NewCodexAppServer()
	c.SetRequestHandler(func(request ServerRequest) {
		select {
		case requests <- request:
		default:
		}
	})
	c.SetNotifyHandler(func(method string, params json.RawMessage) {
		event := struct {
			method string
			output *AppServerOutput
			status string
		}{method: method}
		if output, ok := ParseAppServerOutputNotification(method, params); ok {
			copy := output
			event.output = &copy
		}
		if method == "turn/completed" {
			var completed struct {
				Turn *struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(params, &completed)
			if completed.Turn != nil {
				event.status = completed.Turn.Status
			}
		}
		select {
		case events <- event:
		default:
		}
	})
	defer c.Close()
	if err := c.Ensure(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	started, err := c.StartThread(ctx, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.StartTurnWithCollaborationMode(
		ctx,
		started.WireID(),
		"Use the request_user_input tool exactly once. Ask one question with id `choice`, header `Choice`, question `Pick one`, and options `Alpha` and `Beta`. After the answer, reply exactly NEKONEST_USER_INPUT_OK and do nothing else.",
		nil,
		"plan",
	); err != nil {
		t.Fatal(err)
	}

	var request ServerRequest
	select {
	case request = <-requests:
	case <-time.After(45 * time.Second):
		t.Fatal("timed out waiting for requestUserInput")
	}
	if request.Method != "item/tool/requestUserInput" {
		t.Fatalf("unexpected server request %q", request.Method)
	}
	pending := c.PendingUserInputFor(started.WireID())
	if pending == nil || len(pending.Questions) != 1 {
		t.Fatalf("structured request was not tracked: %#v", pending)
	}
	questionID := pending.Questions[0].ID
	if questionID == "" {
		t.Fatal("requestUserInput question id is empty")
	}
	if status, err := c.RespondUserInput(pending.RequestID, map[string][]string{questionID: {"Alpha"}}); err != nil || status != "accepted" {
		t.Fatalf("respond user input status=%q err=%v", status, err)
	}

	var finalAssistant string
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.output != nil && event.output.Type == "assistant" && event.output.Final {
				finalAssistant = event.output.Content
			}
			if event.method != "turn/completed" {
				continue
			}
			if event.status != "completed" || !strings.Contains(finalAssistant, "NEKONEST_USER_INPUT_OK") {
				t.Fatalf("turn status=%q assistant=%q", event.status, finalAssistant)
			}
			return
		case <-deadline.C:
			t.Fatal("timed out waiting for requestUserInput turn completion")
		}
	}
}

func TestLiveCodexAppServerExitRecoverySmoke(t *testing.T) {
	if os.Getenv("NEKONEST_LIVE_CODEX") != "1" {
		t.Skip("set NEKONEST_LIVE_CODEX=1 for live app-server smoke")
	}
	c := NewCodexAppServer()
	defer c.Close()
	exits := make(chan AppServerExit, 1)
	c.SetExitHandler(func(event AppServerExit) {
		select {
		case exits <- event:
		default:
		}
	})
	if err := c.Ensure(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started, err := c.StartThread(ctx, liveCodexWorkspace(t), "")
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := c.StartTurn(ctx, started.WireID(), "Think silently for a while, then reply RECOVERY_TEST_DONE.", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the real wire id bound to the current process generation even if a
	// very fast model completes before the deliberate kill reaches the process.
	c.SetActiveTurn(started.WireID(), turnID)
	c.mu.Lock()
	generation := c.generation
	process := c.cmd.Process
	c.mu.Unlock()
	if process == nil {
		t.Fatal("app-server process is missing")
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-exits:
		if event.Generation != generation || len(event.Sessions) == 0 {
			t.Fatalf("unexpected exit event: %#v", event)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for app-server exit event")
	}
	if c.Initialized() || c.ActiveTurnID(started.WireID()) != "" {
		t.Fatal("unexpected exit retained initialized/active state")
	}
	if err := c.Ensure(); err != nil {
		t.Fatalf("bounded recovery initialize: %v", err)
	}
	c.mu.Lock()
	recoveredGeneration := c.generation
	c.mu.Unlock()
	if recoveredGeneration <= generation || !c.Initialized() {
		t.Fatalf("recovery generation=%d old=%d initialized=%v", recoveredGeneration, generation, c.Initialized())
	}
}

func TestLiveCodexAppServerAttachmentSmoke(t *testing.T) {
	if os.Getenv("NEKONEST_LIVE_CODEX") != "1" {
		t.Skip("set NEKONEST_LIVE_CODEX=1 for live app-server smoke")
	}
	workspace := liveCodexWorkspace(t)
	reportPath := liveCodexReportPath(t, "live-appserver-attachment-smoke.json")
	report := map[string]any{"started_at": time.Now().Format(time.RFC3339)}
	defer func() {
		report["finished_at"] = time.Now().Format(time.RFC3339)
		body, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(reportPath, body, 0o644)
		t.Logf("live report written to %s", reportPath)
	}()

	textPath := filepath.Join(t.TempDir(), "live-attachment-final.txt")
	imagePath := filepath.Join(workspace, "pwa", "public", "pwa-192x192.png")
	if err := os.WriteFile(textPath, []byte("PHONE_FILE_ATTACHMENT_FINAL_OK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(textPath)
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatal(err)
	}
	report["text_file_ready"] = true
	report["image_file_ready"] = true

	type attachmentEvent struct {
		method string
		output *AppServerOutput
		status string
	}
	requests := make(chan ServerRequest, 8)
	events := make(chan attachmentEvent, 256)
	c := NewCodexAppServer()
	c.SetRequestHandler(func(request ServerRequest) {
		select {
		case requests <- request:
		default:
		}
	})
	c.SetNotifyHandler(func(method string, params json.RawMessage) {
		event := attachmentEvent{method: method}
		if output, ok := ParseAppServerOutputNotification(method, params); ok {
			copy := output
			event.output = &copy
		}
		if method == "turn/completed" {
			var completed struct {
				Turn *struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(params, &completed)
			if completed.Turn != nil {
				event.status = completed.Turn.Status
			}
		}
		select {
		case events <- event:
		default:
		}
	})
	defer c.Close()
	if err := c.Ensure(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	started, err := c.StartThreadWithAttachments(
		ctx, workspace,
		"Inspect both supplied attachments without modifying them. The image should visibly show lavender cat ears around a peach face. Read the text file too. If both checks succeed, reply exactly NEKONEST_ATTACHMENT_LIVE_OK.",
		[]attach.LocalFile{
			{Path: imagePath, Name: "pwa-192x192.png", MIME: "image/png"},
			{Path: textPath, Name: "live-attachment-final.txt", MIME: "text/plain"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	report["thread_id"] = started.ThreadID
	report["turn_id"] = started.TurnID

	var finalAssistant string
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case request := <-requests:
			pending := c.PendingApprovalFor(started.WireID())
			if pending == nil {
				t.Fatalf("request %s was not tracked", request.Method)
			}
			report["request_method"] = request.Method
			if err := c.ApprovePending(pending.ID); err != nil {
				t.Fatal(err)
			}
			report["approval_decision"] = "accept"
		case event := <-events:
			if event.output != nil && event.output.Type == "assistant" && event.output.Final {
				finalAssistant = event.output.Content
			}
			if event.method != "turn/completed" {
				continue
			}
			report["turn_status"] = event.status
			markerSeen := strings.Contains(finalAssistant, "NEKONEST_ATTACHMENT_LIVE_OK")
			report["attachment_marker_seen"] = markerSeen
			if event.status != "completed" || !markerSeen {
				t.Fatalf("turn status=%q marker_seen=%v", event.status, markerSeen)
			}
			return
		case <-deadline.C:
			t.Fatal("timed out waiting for attachment turn completion")
		}
	}
}
