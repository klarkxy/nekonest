package agentexec

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
