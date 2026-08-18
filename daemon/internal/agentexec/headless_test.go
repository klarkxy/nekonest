package agentexec

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/attach"
)

func TestCodexResumeArgsAttachOnlySupportedImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "files with spaces")
	image := filepath.Join(dir, "photo.png")
	fallbackImage := filepath.Join(dir, "scan.JPEG")
	textFile := filepath.Join(dir, "notes.txt")
	svgFile := filepath.Join(dir, "vector.svg")
	got := codexResumeArgs("session", "--keep-as-prompt", []attach.LocalFile{
		{Path: image, MIME: "image/png"},
		{Path: textFile, MIME: "text/plain"},
		{Path: fallbackImage},
		{Path: svgFile, MIME: "image/svg+xml"},
	})
	want := []string{
		"exec", "--json",
		"--add-dir", dir,
		"resume",
		"--image", image,
		"--image", fallbackImage,
		"session", "--", "--keep-as-prompt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codexResumeArgs() = %#v, want %#v", got, want)
	}
}

func TestClaudeResumeArgsAuthorizesLocalDirectoryWithoutFileFlag(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "attachments")
	got := claudeResumeArgs("session", "inspect paths in prompt", []attach.LocalFile{
		{Path: filepath.Join(dir, "photo.png"), MIME: "image/png"},
		{Path: filepath.Join(dir, "report.pdf"), MIME: "application/pdf"},
	})
	want := []string{
		"--resume", "session",
		"--add-dir", dir,
		"-p", "inspect paths in prompt",
		"--output-format", "stream-json",
		"--verbose",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claudeResumeArgs() = %#v, want %#v", got, want)
	}
	if strings.Contains(strings.Join(got, "\x00"), "--file") {
		t.Fatalf("Claude local paths used remote --file syntax: %#v", got)
	}
}

func TestNativeStartArgsUseExplicitSessionIDs(t *testing.T) {
	claude := claudeStartArgs("8d0c7f6f-a7ca-4e35-b63f-a3d6f3f42ee5", "first prompt")
	if !reflect.DeepEqual(claude, []string{
		"--session-id", "8d0c7f6f-a7ca-4e35-b63f-a3d6f3f42ee5",
		"-p", "first prompt", "--output-format", "stream-json", "--verbose",
	}) {
		t.Fatalf("claude start args = %#v", claude)
	}
	grok := grokStartArgs("7fcd5e7f-7c88-407a-9c12-a162d4e50c35", "first prompt", `D:\repo`)
	if !reflect.DeepEqual(grok, []string{
		"--session-id", "7fcd5e7f-7c88-407a-9c12-a162d4e50c35",
		"-p", "first prompt", "--output-format", "streaming-json", "--permission-mode", "auto", "--cwd", `D:\repo`,
	}) {
		t.Fatalf("grok start args = %#v", grok)
	}
}

func TestHeadlessStartRequiresPositivePromptOutput(t *testing.T) {
	if claudePromptAcknowledged(`{"type":"system","subtype":"init"}`) {
		t.Fatal("Claude init event was treated as prompt acknowledgement")
	}
	if !claudePromptAcknowledged(`{"type":"assistant","message":{"content":[]}}`) {
		t.Fatal("Claude assistant event did not acknowledge prompt processing")
	}
	if grokPromptAcknowledged(`{"type":"error","message":"denied"}`) {
		t.Fatal("Grok error event was treated as prompt acknowledgement")
	}
	if !grokPromptAcknowledged(`{"type":"tool_call"}`) {
		t.Fatal("Grok tool event did not acknowledge prompt processing")
	}
	if !grokPromptAcknowledged(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}}`) {
		t.Fatal("Grok ACP agent_message_chunk did not acknowledge prompt processing")
	}
	if !grokPromptAcknowledged(`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"plan"}}}}`) {
		t.Fatal("Grok ACP agent_thought_chunk did not acknowledge prompt processing")
	}
	if grokPromptAcknowledged(`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"echo"}}}}`) {
		t.Fatal("Grok ACP user echo was treated as prompt acknowledgement")
	}
	if grokPromptAcknowledged(`{"method":"session/update","params":{"update":{"sessionUpdate":"retry_status","attempt":2}}}`) {
		t.Fatal("Grok ACP retry metadata was treated as prompt acknowledgement")
	}
	if grokPromptAcknowledged(`{"method":"session/update","params":{}}`) {
		t.Fatal("malformed Grok ACP update was treated as prompt acknowledgement")
	}
	if grokPromptAcknowledged(`not-json`) {
		t.Fatal("malformed Grok JSON was treated as prompt acknowledgement")
	}
}

func TestGrokResumeArgs(t *testing.T) {
	got := grokResumeArgs("native", "hello %!\nworld", `D:\repo`)
	want := []string{
		"--resume", "native",
		"-p", "hello %!\nworld",
		"--output-format", "streaming-json",
		"--permission-mode", "auto",
		"--cwd", `D:\repo`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grokResumeArgs() = %#v", got)
	}
}

func TestGrokStreamingOutputAccumulatesStableMessages(t *testing.T) {
	commander := NewGrokCommander()
	var events []struct {
		typ, content, id string
	}
	commander.OnAgentOutput = func(_ string, typ, content, id string) {
		events = append(events, struct {
			typ, content, id string
		}{typ, content, id})
	}
	commander.parseAndForwardOutput("s", `{"type":"text","data":"hello"}`)
	commander.parseAndForwardOutput("s", `{"type":"text","data":" world"}`)
	commander.parseAndForwardOutput("s", `{"type":"thought","data":"think"}`)
	commander.parseAndForwardOutput("s", `{"type":"end"}`)
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].content != "hello" ||
		events[2].content != "hello world" ||
		events[0].id == "" ||
		events[0].id != events[2].id {
		t.Fatalf("text events = %#v", events)
	}
	if events[1].typ != "thinking" || events[1].content != "think" {
		t.Fatalf("thought event = %#v", events[1])
	}
}

func TestGrokStreamingOutputIsBoundedAndBatched(t *testing.T) {
	commander := NewGrokCommander()
	var (
		eventCount int
		last       string
	)
	commander.OnAgentOutput = func(_ string, typ, content, _ string) {
		if typ != "assistant" {
			return
		}
		eventCount++
		last = content
	}
	chunk := strings.Repeat("x", 1024)
	for i := 0; i < 200; i++ {
		commander.parseAndForwardOutput(
			"bounded",
			`{"type":"text","data":"`+chunk+`"}`,
		)
	}
	commander.parseAndForwardOutput("bounded", `{"type":"end"}`)

	if len(last) != grokMaxStreamBytes {
		t.Fatalf("bounded content bytes = %d, want %d", len(last), grokMaxStreamBytes)
	}
	if eventCount > grokMaxStreamBytes/grokEmitStepBytes+2 {
		t.Fatalf("stream emitted %d cumulative frames", eventCount)
	}
}

func TestGrokACPOutputAccumulatesStableMessages(t *testing.T) {
	commander := NewGrokCommander()
	var events []struct {
		typ, content, id string
	}
	commander.OnAgentOutput = func(_ string, typ, content, id string) {
		events = append(events, struct {
			typ, content, id string
		}{typ, content, id})
	}
	commander.parseAndForwardOutput("s", `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","messageId":"msg-a","content":{"type":"text","text":"hello"}}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","messageId":"msg-a","content":{"type":"text","text":" world"}}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","messageId":"thought-a","content":{"type":"text","text":"think"}}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"echo"}}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"run"}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`)
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].typ != "assistant" || events[0].content != "hello" || events[0].id != "msg-a" {
		t.Fatalf("first assistant = %#v", events[0])
	}
	if events[1].typ != "thinking" || events[1].content != "think" || events[1].id != "thought-a" {
		t.Fatalf("thinking = %#v", events[1])
	}
	if events[2].typ != "assistant" || events[2].content != "hello world" || events[2].id != "msg-a" {
		t.Fatalf("flushed assistant = %#v", events[2])
	}
	if events[0].id == events[1].id {
		t.Fatal("assistant and thinking reused the same message id")
	}
}

func TestGrokInterleavedPendingStreamsFlushWithinEmitLatency(t *testing.T) {
	prev := grokEmitLatency
	grokEmitLatency = 20 * time.Millisecond
	defer func() { grokEmitLatency = prev }()

	commander := NewGrokCommander()
	var events []struct {
		typ, content, id string
	}
	commander.OnAgentOutput = func(_ string, typ, content, id string) {
		events = append(events, struct {
			typ, content, id string
		}{typ, content, id})
	}
	commander.parseAndForwardOutput("s", `{"type":"text","data":"a"}`)
	commander.parseAndForwardOutput("s", `{"type":"text","data":"b"}`)
	commander.parseAndForwardOutput("s", `{"type":"thought","data":"t"}`)
	if len(events) != 2 || events[0].content != "a" || events[1].typ != "thinking" || events[1].content != "t" {
		t.Fatalf("immediate events = %#v", events)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for len(events) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(events) != 3 || events[2].typ != "assistant" || events[2].content != "ab" {
		t.Fatalf("pending assistant should flush within latency after interleaved thinking: %#v", events)
	}
}

func TestGrokCumulativeCallbacksRemainOrdered(t *testing.T) {
	prev := grokEmitLatency
	grokEmitLatency = 10 * time.Millisecond
	defer func() { grokEmitLatency = prev }()

	commander := NewGrokCommander()
	var (
		mu      sync.Mutex
		lengths []int
		once    sync.Once
	)
	timerStarted := make(chan struct{})
	releaseTimer := make(chan struct{})
	commander.OnAgentOutput = func(_ string, typ, content, _ string) {
		if typ != "assistant" {
			return
		}
		if content == "ab" {
			once.Do(func() { close(timerStarted) })
			<-releaseTimer
		}
		mu.Lock()
		lengths = append(lengths, len(content))
		mu.Unlock()
	}

	commander.parseAndForwardOutput("ordered", `{"type":"text","data":"a"}`)
	commander.parseAndForwardOutput("ordered", `{"type":"text","data":"b"}`)
	select {
	case <-timerStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer flush did not reach the sink")
	}

	newerDone := make(chan struct{})
	go func() {
		commander.parseAndForwardOutput(
			"ordered",
			`{"type":"text","data":"`+strings.Repeat("x", grokEmitStepBytes)+`"}`,
		)
		close(newerDone)
	}()
	select {
	case <-newerDone:
		close(releaseTimer)
		t.Fatal("newer cumulative callback overtook the blocked timer callback")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseTimer)
	select {
	case <-newerDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("newer cumulative callback did not resume")
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(lengths); i++ {
		if lengths[i] < lengths[i-1] {
			t.Fatalf("cumulative callback lengths regressed: %v", lengths)
		}
	}
}

func TestGrokACPMessageIDsStayStableAfterStreamStarts(t *testing.T) {
	commander := NewGrokCommander()
	var ids []string
	commander.OnAgentOutput = func(_ string, typ, _, id string) {
		if typ == "assistant" {
			ids = append(ids, id)
		}
	}
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","messageId":"native-a","content":{"type":"text","text":"one"}}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","messageId":"native-b","content":{"type":"text","text":" two"}}}}`)
	commander.parseAndForwardOutput("s", `{"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`)
	if len(ids) != 2 || ids[0] != "native-a" || ids[1] != "native-a" {
		t.Fatalf("assistant ids switched after stream start: %#v", ids)
	}
}

func TestGrokShortChunksFlushWithinEmitLatency(t *testing.T) {
	prev := grokEmitLatency
	grokEmitLatency = 20 * time.Millisecond
	defer func() { grokEmitLatency = prev }()

	commander := NewGrokCommander()
	var events []string
	commander.OnAgentOutput = func(_ string, typ, content, _ string) {
		if typ == "assistant" {
			events = append(events, content)
		}
	}
	commander.parseAndForwardOutput("s", `{"type":"text","data":"a"}`)
	if len(events) != 1 || events[0] != "a" {
		t.Fatalf("first chunk should emit immediately: %#v", events)
	}
	commander.parseAndForwardOutput("s", `{"type":"text","data":"b"}`)
	if len(events) != 1 {
		t.Fatalf("second short chunk should wait for latency window: %#v", events)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for len(events) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(events) != 2 || events[1] != "ab" {
		t.Fatalf("latency flush = %#v", events)
	}
}

func TestGrokStopAllClearsPendingEmitTimer(t *testing.T) {
	prev := grokEmitLatency
	grokEmitLatency = 30 * time.Millisecond
	defer func() { grokEmitLatency = prev }()

	commander := NewGrokCommander()
	var events int
	commander.OnAgentOutput = func(_, _, _, _ string) { events++ }
	commander.parseAndForwardOutput("s", `{"type":"text","data":"a"}`)
	commander.parseAndForwardOutput("s", `{"type":"text","data":"b"}`)
	commander.StopAll()
	time.Sleep(80 * time.Millisecond)
	if events != 1 {
		t.Fatalf("late timer emitted after StopAll: events=%d", events)
	}
	commander.parseAndForwardOutput("s", `{"type":"text","data":"next"}`)
	if events != 2 {
		t.Fatalf("new run after StopAll = %d events", events)
	}
}

func TestHeadlessCommandersDoNotForwardStderrAsAssistant(t *testing.T) {
	grok := NewGrokCommander()
	kimi := NewKimiCommander()
	codex := NewCodexCommander()
	claude := NewClaudeCommander()
	var grokEvents, kimiEvents, codexEvents, claudeEvents int
	grok.OnAgentOutput = func(_, _, _, _ string) { grokEvents++ }
	kimi.OnAgentOutput = func(_, _, _, _ string) { kimiEvents++ }
	codex.OnAgentOutput = func(_, _, _ string) { codexEvents++ }
	claude.OnAgentOutput = func(_, _, _ string) { claudeEvents++ }

	grok.handleProcessLine("g", "stderr", "diagnostic")
	kimi.handleProcessLine("k", "stderr", "resume notice")
	codex.handleProcessLine(
		"c",
		"stderr",
		"2026-07-29T04:48:01.778780Z WARN codex_core_skills::loader: ignoring invalid icon",
	)
	claude.handleProcessLine("a", "stderr", "plugin warning")
	if grokEvents != 0 || kimiEvents != 0 ||
		codexEvents != 0 || claudeEvents != 0 {
		t.Fatalf(
			"stderr forwarded: grok=%d kimi=%d codex=%d claude=%d",
			grokEvents,
			kimiEvents,
			codexEvents,
			claudeEvents,
		)
	}

	grok.handleProcessLine("g", "stdout", `{"type":"text","data":"ok"}`)
	kimi.handleProcessLine("k", "stdout", `{"role":"assistant","content":"ok"}`)
	codex.handleProcessLine("c", "stdout", `{"role":"assistant","content":"ok"}`)
	claude.handleProcessLine(
		"a",
		"stdout",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`,
	)
	if grokEvents != 1 || kimiEvents != 1 ||
		codexEvents != 1 || claudeEvents != 1 {
		t.Fatalf(
			"stdout not forwarded: grok=%d kimi=%d codex=%d claude=%d",
			grokEvents,
			kimiEvents,
			codexEvents,
			claudeEvents,
		)
	}
}

func TestKimiResumeArgsAndStreamOutput(t *testing.T) {
	got := kimiResumeArgs("native", "hello", false)
	want := []string{"--session", "native", "-p", "hello", "--output-format", "stream-json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kimiResumeArgs() = %#v", got)
	}
	legacy := kimiResumeArgs("native", "hello", true)
	legacyWant := []string{"--print", "--session", "native", "-p", "hello", "--output-format", "stream-json"}
	if !reflect.DeepEqual(legacy, legacyWant) {
		t.Fatalf("legacy kimiResumeArgs() = %#v", legacy)
	}

	commander := NewKimiCommander()
	var typ, content, id string
	commander.OnAgentOutput = func(_ string, gotType, gotContent, gotID string) {
		typ, content, id = gotType, gotContent, gotID
	}
	commander.parseAndForwardOutput("s", `{"role":"assistant","content":[{"type":"text","text":"hello"}]}`)
	if typ != "assistant" || content != "hello" || id == "" {
		t.Fatalf("output = %q %q %q", typ, content, id)
	}
}

func TestKimiSynthesizedMessageIDsDoNotCollideAcrossRuns(t *testing.T) {
	commander := NewKimiCommander()
	var ids []string
	commander.OnAgentOutput = func(_, _, _, id string) {
		ids = append(ids, id)
	}

	commander.beginRun("same-session")
	commander.parseAndForwardOutput(
		"same-session",
		`{"role":"assistant","content":"first run"}`,
	)
	commander.beginRun("same-session")
	commander.parseAndForwardOutput(
		"same-session",
		`{"role":"assistant","content":"second run"}`,
	)

	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		t.Fatalf("synthesized ids = %#v", ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("message id reused across runs: %q", ids[0])
	}
}

func TestACPMessageChunksAccumulateBeforeOutput(t *testing.T) {
	kimi := NewKimiCommander()
	var kimiEvents []struct{ content, id string }
	kimi.OnAgentOutput = func(_ string, _ string, content string, id string) {
		kimiEvents = append(kimiEvents, struct{ content, id string }{content, id})
	}
	kimi.handleACPUpdate("kimi-native", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "kimi-message",
		"content":       map[string]any{"type": "text", "text": "hello"},
	})
	kimi.handleACPUpdate("kimi-native", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "kimi-message",
		"content":       map[string]any{"type": "text", "text": " world"},
	})
	if len(kimiEvents) != 2 || kimiEvents[1].content != "hello world" ||
		kimiEvents[0].id != "kimi-message" || kimiEvents[1].id != "kimi-message" {
		t.Fatalf("Kimi ACP events = %#v", kimiEvents)
	}
}

func TestACPMessageChunkFallbackIDsRemainStable(t *testing.T) {
	kimi := NewKimiCommander()
	var ids []string
	kimi.OnAgentOutput = func(_ string, _ string, _ string, id string) { ids = append(ids, id) }
	for _, text := range []string{"one", " two"} {
		kimi.handleACPUpdate("kimi-native", map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": text},
		})
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("Kimi ACP fallback ids = %#v", ids)
	}
}

func TestStopAllClearsACPChunkAccumulators(t *testing.T) {
	kimi := NewKimiCommander()
	kimi.handleACPUpdate("kimi-native", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": "partial"},
	})
	kimi.StopAll()
	if len(kimi.acpChunks) != 0 || len(kimi.acpIDs) != 0 {
		t.Fatalf("Kimi ACP state survived StopAll: %#v %#v", kimi.acpChunks, kimi.acpIDs)
	}
}

func TestKimiLegacyStoreArgsFollowInstalledCLICapability(t *testing.T) {
	oldCLI := NewKimiCommander()
	oldCLI.helpProbe = func() (string, error) {
		return "Usage: kimi-cli [OPTIONS]\n  --print  Run non-interactively", nil
	}
	oldArgs, err := oldCLI.resumeArgs("legacy", "hello", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oldArgs, []string{
		"--print", "--session", "legacy", "-p", "hello", "--output-format", "stream-json",
	}) {
		t.Fatalf("old CLI args = %#v", oldArgs)
	}
	if _, err := oldCLI.resumeArgs("current", "hello", false); err == nil {
		t.Fatal("legacy CLI accepted a current Kimi Code store")
	}

	newCLI := NewKimiCommander()
	newCLI.helpProbe = func() (string, error) {
		return "Usage: kimi [OPTIONS]\n  -p, --prompt <PROMPT>", nil
	}
	newArgs, err := newCLI.resumeArgs("current", "hello", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(newArgs, []string{
		"--session", "current", "-p", "hello", "--output-format", "stream-json",
	}) {
		t.Fatalf("new CLI args = %#v", newArgs)
	}
	if _, err := newCLI.resumeArgs("legacy", "hello", true); err == nil {
		t.Fatal("current Kimi Code CLI accepted a legacy .kimi store")
	}

	unknownCLI := NewKimiCommander()
	unknownCLI.helpProbe = func() (string, error) {
		return "", errors.New("help failed")
	}
	if _, err := unknownCLI.resumeArgs("legacy", "hello", true); err == nil {
		t.Fatal("legacy store stayed writable when CLI capability was unknown")
	}
}
