package agentexec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/attach"
)

func TestParseThreadStartResponse(t *testing.T) {
	raw := json.RawMessage(`{
		"thread": {
			"id": "thr_abc",
			"sessionId": "ses_xyz"
		}
	}`)
	id, sid := parseThreadStartResponse(raw)
	if id != "thr_abc" || sid != "ses_xyz" {
		t.Fatalf("id=%q sid=%q", id, sid)
	}
}

func TestParseThreadStartResponseFlat(t *testing.T) {
	raw := json.RawMessage(`{"threadId":"t1","sessionId":"s1"}`)
	id, sid := parseThreadStartResponse(raw)
	if id != "t1" || sid != "s1" {
		t.Fatalf("id=%q sid=%q", id, sid)
	}
}

func TestParseTurnID(t *testing.T) {
	raw := json.RawMessage(`{"turn":{"id":"turn-1","status":"inProgress","items":[]}}`)
	if got := parseTurnID(raw); got != "turn-1" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildTurnInputImagesAndFiles(t *testing.T) {
	in, err := buildTurnInput("hello", []attach.LocalFile{
		{Path: `D:\tmp\a.png`, Name: "a.png", MIME: "image/png"},
		{Path: `D:\tmp\notes.txt`, Name: "notes.txt", MIME: "text/plain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 2 {
		t.Fatalf("len=%d %#v", len(in), in)
	}
	if in[0]["type"] != "text" {
		t.Fatalf("first=%#v", in[0])
	}
	text, _ := in[0]["text"].(string)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "notes.txt") {
		t.Fatalf("text=%q", text)
	}
	if in[1]["type"] != "localImage" || in[1]["path"] != `D:\tmp\a.png` {
		t.Fatalf("image=%#v", in[1])
	}
}

func TestBuildControlParamsForcePhoneReview(t *testing.T) {
	thread := buildThreadStartParams(`D:\nekonest`)
	if thread["cwd"] != `D:\nekonest` || thread["approvalPolicy"] != "on-request" || thread["approvalsReviewer"] != "user" {
		t.Fatalf("thread params=%#v", thread)
	}
	turn := buildTurnStartParams("thread-1", []map[string]any{{"type": "text", "text": "hello"}})
	if turn["threadId"] != "thread-1" || turn["approvalPolicy"] != "on-request" || turn["approvalsReviewer"] != "user" {
		t.Fatalf("turn params=%#v", turn)
	}
}

func TestTrackServerRequestCommandApproval(t *testing.T) {
	c := NewCodexAppServer()
	c.RegisterThreadIDs("thr1", "ses1", "ses1")
	p := c.TrackServerRequest(ServerRequest{
		ID:     "9",
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(`{"threadId":"thr1","turnId":"turn9","itemId":"item9","command":"rm -rf /tmp/x","reason":"cleanup"}`),
	})
	if p == nil {
		t.Fatal("expected pending")
	}
	if p.RequestID != "9" || p.ThreadID != "thr1" || p.TurnID != "turn9" {
		t.Fatalf("%#v", p)
	}
	if p.ToolName != "command" {
		t.Fatalf("tool=%s", p.ToolName)
	}
	if c.ActiveTurnID("ses1") != "turn9" {
		t.Fatalf("active turn=%q", c.ActiveTurnID("ses1"))
	}
	snap := c.PendingApprovalFor("ses1")
	if snap == nil || snap.ID != p.ID {
		t.Fatalf("snap=%#v", snap)
	}
	res, err := buildApprovalResult(p, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	if string(b) != `{"decision":"accept"}` {
		t.Fatalf("result=%s", b)
	}
	resDeny, err := buildApprovalResult(p, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(resDeny)
	if string(b) != `{"decision":"decline"}` {
		t.Fatalf("deny=%s", b)
	}
}

func TestBuildApprovalResultLegacyExec(t *testing.T) {
	p := &PendingApproval{Method: "execCommandApproval"}
	res, err := buildApprovalResult(p, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	if string(b) != `{"decision":"approved"}` {
		t.Fatalf("%s", b)
	}
}

func TestHandleNotificationTurnLifecycle(t *testing.T) {
	c := NewCodexAppServer()
	c.RegisterThreadIDs("thr1", "ses1", "ses1")
	c.HandleNotification("turn/started", json.RawMessage(`{"threadId":"thr1","turn":{"id":"t1","status":"inProgress","items":[]}}`))
	if c.ActiveTurnID("ses1") != "t1" {
		t.Fatalf("got %q", c.ActiveTurnID("ses1"))
	}
	if c.LastTurnStatus("ses1") != "inProgress" {
		t.Fatalf("status=%q", c.LastTurnStatus("ses1"))
	}
	c.HandleNotification("turn/completed", json.RawMessage(`{"threadId":"thr1","turn":{"id":"t1","status":"completed","items":[]}}`))
	if c.ActiveTurnID("ses1") != "" {
		t.Fatalf("expected clear, got %q", c.ActiveTurnID("ses1"))
	}
	if c.LastTurnStatus("ses1") != "completed" {
		t.Fatalf("status=%q", c.LastTurnStatus("ses1"))
	}
}

func TestStartingTurnWaitsForStartedNotification(t *testing.T) {
	c := NewCodexAppServer()
	c.RegisterThreadIDs("thr1", "ses1", "wire1")
	c.setStartingTurn("thr1", "turn1")
	if c.LastTurnStatus("wire1") != "starting" {
		t.Fatalf("status=%q", c.LastTurnStatus("wire1"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.HandleNotification("turn/started", json.RawMessage(`{"threadId":"thr1","turn":{"id":"turn1","status":"inProgress"}}`))
		close(started)
	}()
	if err := c.waitForStartedTurn(ctx, "wire1", "turn1"); err != nil {
		t.Fatal(err)
	}
	<-started
	if c.LastTurnStatus("wire1") != "inProgress" {
		t.Fatalf("status=%q", c.LastTurnStatus("wire1"))
	}
	c.setStartingTurn("thr1", "turn1")
	if c.LastTurnStatus("wire1") != "inProgress" {
		t.Fatalf("late turn/start response downgraded status=%q", c.LastTurnStatus("wire1"))
	}
}

func TestHandleNotificationInterruptedAndStaleCompletion(t *testing.T) {
	c := NewCodexAppServer()
	c.RegisterThreadIDs("thr1", "ses1", "wire1")
	c.SetActiveTurn("thr1", "turn-new")
	c.HandleNotification("turn/completed", json.RawMessage(`{"threadId":"thr1","turn":{"id":"turn-old","status":"completed"}}`))
	if c.ActiveTurnID("wire1") != "turn-new" || c.LastTurnStatus("wire1") != "inProgress" {
		t.Fatalf("stale completion changed active state: turn=%q status=%q", c.ActiveTurnID("wire1"), c.LastTurnStatus("wire1"))
	}
	c.HandleNotification("turn/completed", json.RawMessage(`{"threadId":"thr1","turn":{"id":"turn-new","status":"interrupted"}}`))
	if c.ActiveTurnID("wire1") != "" || c.LastTurnStatus("wire1") != "interrupted" {
		t.Fatalf("interrupt state: turn=%q status=%q", c.ActiveTurnID("wire1"), c.LastTurnStatus("wire1"))
	}
}

func TestParseAppServerAssistantOutput(t *testing.T) {
	delta, ok := ParseAppServerOutputNotification(
		"item/agentMessage/delta",
		json.RawMessage(`{"threadId":"thr1","turnId":"turn1","itemId":"item1","delta":"hel"}`),
	)
	if !ok || !delta.Delta || delta.Final || delta.Type != "assistant" || delta.Content != "hel" || delta.MessageID != "item1" {
		t.Fatalf("delta=%#v ok=%v", delta, ok)
	}

	final, ok := ParseAppServerOutputNotification(
		"item/completed",
		json.RawMessage(`{"threadId":"thr1","turnId":"turn1","item":{"id":"item1","type":"agentMessage","text":"hello"}}`),
	)
	if !ok || final.Delta || !final.Final || final.Type != "assistant" || final.Content != "hello" || final.MessageID != "item1" {
		t.Fatalf("final=%#v ok=%v", final, ok)
	}
}

func TestParseAppServerReasoningAndTerminalError(t *testing.T) {
	reasoning, ok := ParseAppServerOutputNotification(
		"item/reasoning/summaryTextDelta",
		json.RawMessage(`{"threadId":"thr1","turnId":"turn1","itemId":"why1","summaryIndex":0,"delta":"checking"}`),
	)
	if !ok || reasoning.Type != "thinking" || reasoning.MessageID != "why1:summary" {
		t.Fatalf("reasoning=%#v ok=%v", reasoning, ok)
	}

	if _, ok := ParseAppServerOutputNotification(
		"error",
		json.RawMessage(`{"threadId":"thr1","turnId":"turn1","willRetry":true,"error":{"message":"temporary"}}`),
	); ok {
		t.Fatal("retryable errors must not be emitted as terminal failures")
	}
	terminal, ok := ParseAppServerOutputNotification(
		"turn/completed",
		json.RawMessage(`{"threadId":"thr1","turn":{"id":"turn1","status":"failed","error":{"message":"boom"},"items":[]}}`),
	)
	if !ok || terminal.Type != "error" || terminal.Content != "boom" || terminal.MessageID != "turn1:error" {
		t.Fatalf("terminal=%#v ok=%v", terminal, ok)
	}
}

func TestWireIDForThread(t *testing.T) {
	c := NewCodexAppServer()
	c.RegisterThreadIDs("thr1", "ses1", "wire1")
	if got := c.WireIDForThread("thr1"); got != "wire1" {
		t.Fatalf("got %q", got)
	}
	if got := c.WireIDForThread("ses1"); got != "wire1" {
		t.Fatalf("alias got %q", got)
	}
}

func TestEncodeRequestID(t *testing.T) {
	v, err := encodeRequestID("42")
	if err != nil {
		t.Fatal(err)
	}
	if v.(int64) != 42 {
		t.Fatalf("%#v", v)
	}
	v, err = encodeRequestID("abc")
	if err != nil {
		t.Fatal(err)
	}
	if v.(string) != "abc" {
		t.Fatalf("%#v", v)
	}
}

func TestPendingApprovalUserInputTool(t *testing.T) {
	c := NewCodexAppServer()
	p := c.TrackServerRequest(ServerRequest{
		ID:     "1",
		Method: "item/tool/requestUserInput",
		Params: json.RawMessage(`{"threadId":"thr","turnId":"t","itemId":"i","questions":[]}`),
	})
	if p.ToolName != "user_input" {
		t.Fatalf("%s", p.ToolName)
	}
	if time.Since(p.CreatedAt) > time.Minute {
		t.Fatal("createdAt")
	}
}
