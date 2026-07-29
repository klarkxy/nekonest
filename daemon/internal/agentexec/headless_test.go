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

func TestKiloResumeArgsAttachEveryFileAndTerminateOptions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "attachments")
	first := filepath.Join(dir, "photo.png")
	second := filepath.Join(dir, "notes.txt")
	got := kiloResumeArgs("session", "--help", `D:\project`, []attach.LocalFile{
		{Path: first, MIME: "image/png"},
		{Path: second, MIME: "text/plain"},
	})
	want := []string{
		"run",
		"--session", "session",
		"--format", "json",
		"--dir", `D:\project`,
		"--file", first,
		"--file", second,
		"--", "--help",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kiloResumeArgs() = %#v, want %#v", got, want)
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
	kilo := NewKiloCommander()
	codex := NewCodexCommander()
	claude := NewClaudeCommander()
	var grokEvents, kimiEvents, kiloEvents, codexEvents, claudeEvents int
	grok.OnAgentOutput = func(_, _, _, _ string) { grokEvents++ }
	kimi.OnAgentOutput = func(_, _, _, _ string) { kimiEvents++ }
	kilo.OnAgentOutput = func(_ string, _ uint64, _, _, _ string) { kiloEvents++ }
	codex.OnAgentOutput = func(_, _, _ string) { codexEvents++ }
	claude.OnAgentOutput = func(_, _, _ string) { claudeEvents++ }

	grok.handleProcessLine("g", "stderr", "diagnostic")
	kimi.handleProcessLine("k", "stderr", "resume notice")
	kilo.handleProcessLine("o", 1, "stderr", "provider diagnostic")
	codex.handleProcessLine(
		"c",
		"stderr",
		"2026-07-29T04:48:01.778780Z WARN codex_core_skills::loader: ignoring invalid icon",
	)
	claude.handleProcessLine("a", "stderr", "plugin warning")
	if grokEvents != 0 || kimiEvents != 0 || kiloEvents != 0 ||
		codexEvents != 0 || claudeEvents != 0 {
		t.Fatalf(
			"stderr forwarded: grok=%d kimi=%d kilo=%d codex=%d claude=%d",
			grokEvents,
			kimiEvents,
			kiloEvents,
			codexEvents,
			claudeEvents,
		)
	}

	grok.handleProcessLine("g", "stdout", `{"type":"text","data":"ok"}`)
	kimi.handleProcessLine("k", "stdout", `{"role":"assistant","content":"ok"}`)
	kilo.handleProcessLine("o", 1, "stdout", `{"type":"text","text":"ok"}`)
	codex.handleProcessLine("c", "stdout", `{"role":"assistant","content":"ok"}`)
	claude.handleProcessLine(
		"a",
		"stdout",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`,
	)
	if grokEvents != 1 || kimiEvents != 1 || kiloEvents != 1 ||
		codexEvents != 1 || claudeEvents != 1 {
		t.Fatalf(
			"stdout not forwarded: grok=%d kimi=%d kilo=%d codex=%d claude=%d",
			grokEvents,
			kimiEvents,
			kiloEvents,
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
