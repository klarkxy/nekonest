package agentexec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/attach"
)

func TestZcodeAndCursorLaunchArgs(t *testing.T) {
	got := zcodeResumeArgs("sess_abc", "hello", `D:\repo`, []attach.LocalFile{{Path: `D:\tmp\a.png`}})
	want := []string{"--resume", "sess_abc", "--prompt", "hello", "--json", "--mode", "yolo", "--cwd", `D:\repo`, "--attach", `D:\tmp\a.png`}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("zcode resume = %#v", got)
	}
	got = zcodeStartArgs("hello", `D:\repo`, []attach.LocalFile{{Path: `D:\tmp\a.png`}})
	if strings.Join(got, " ") != "--prompt hello --json --mode yolo --cwd D:\\repo --attach D:\\tmp\\a.png" {
		t.Fatalf("zcode start = %#v", got)
	}
	cursorAttachment := filepath.Join(t.TempDir(), "a.png")
	cursorAttachmentDir := filepath.Dir(cursorAttachment)
	got = cursorResumeArgs("chat-1", "hello", `D:\repo`, []attach.LocalFile{{Path: cursorAttachment}})
	wantCursor := []string{"--resume", "chat-1", "-p", "hello", "--output-format", "stream-json", "--stream-partial-output", "--force", "--trust", "--workspace", `D:\repo`, "--add-dir", cursorAttachmentDir}
	if strings.Join(got, "\x00") != strings.Join(wantCursor, "\x00") {
		t.Fatalf("cursor resume = %#v", got)
	}
	got = cursorStartArgs("hello", `D:\repo`, []attach.LocalFile{{Path: cursorAttachment}})
	wantCursor = []string{"-p", "hello", "--output-format", "stream-json", "--stream-partial-output", "--force", "--trust", "--workspace", `D:\repo`, "--add-dir", cursorAttachmentDir}
	if strings.Join(got, "\x00") != strings.Join(wantCursor, "\x00") {
		t.Fatalf("cursor start = %#v", got)
	}
}

func TestZcodeAndCursorSessionIDParsers(t *testing.T) {
	if got := zcodeSessionIDFromLine(`{"type":"session","session_id":"sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`); got != "sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("zcode id = %q", got)
	}
	if zcodeSessionIDFromLine(`{"id":"sess_subagent_agent_1"}`) != "" {
		t.Fatal("subagent id accepted")
	}
	if got := cursorSessionIDFromLine(`{"type":"system","subtype":"init","session_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("cursor id = %q", got)
	}
	if !zcodePromptAcknowledged(`{"type":"text","text":"hi"}`) || !cursorPromptAcknowledged(`{"type":"assistant"}`) {
		t.Fatal("ack")
	}
	if zcodePromptAcknowledged(`{"type":"session","session_id":"sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`) ||
		zcodePromptAcknowledged(`{"type":"init"}`) {
		t.Fatal("zcode lifecycle is not prompt ack")
	}
	if cursorPromptAcknowledged(`{"type":"system","session_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`) ||
		cursorPromptAcknowledged(`{"session_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`) {
		t.Fatal("cursor lifecycle is not prompt ack")
	}
}

func TestInstallGateRejectsEditorBinaries(t *testing.T) {
	root := t.TempDir()
	gui := filepath.Join(root, "ZCode.exe")
	if err := os.MkdirAll(filepath.Join(root, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources", "app.asar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gui, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isElectronGUIBinary(gui) {
		t.Fatal("ZCode.exe GUI not rejected")
	}
	if !isElectronGUIBinary(filepath.Join(root, "cursor.exe")) {
		t.Fatal("cursor.exe not rejected")
	}

	zcode := NewZCodeCommander()
	zcode.SetCLIPath(gui)
	if zcode.IsAvailable() {
		t.Fatal("ZCode GUI treated as CLI")
	}
	cursor := NewCursorCommander()
	cursor.SetCLIPath(filepath.Join(root, "cursor.exe"))
	if cursor.IsAvailable() {
		t.Fatal("Cursor editor treated as agent CLI")
	}
	agent := filepath.Join(root, "cursor-agent.exe")
	if err := os.WriteFile(agent, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cursor.SetCLIPath(agent)
	if !cursor.IsAvailable() {
		t.Fatal("cursor-agent should be available")
	}

	grokAgent := filepath.Join(root, "grok", "bin", "agent.exe")
	if err := os.MkdirAll(filepath.Dir(grokAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokAgent, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if looksLikeCursorAgentCLI(grokAgent) {
		t.Fatal("generic agent.exe accepted")
	}
	cursor.SetCLIPath(grokAgent)
	if cursor.IsAvailable() {
		t.Fatal("Grok agent.exe treated as Cursor CLI")
	}
	cursorDirAgent := filepath.Join(root, ".cursor", "bin", "agent.exe")
	if err := os.MkdirAll(filepath.Dir(cursorDirAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorDirAgent, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikeCursorAgentCLI(cursorDirAgent) {
		t.Fatal("agent.exe under a Cursor directory should be accepted")
	}

	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("NEKONEST_CURSOR_CLI", "")
	if findCursorAgentCLI() == "1" {
		t.Fatal("CURSOR_AGENT marker used as CLI path")
	}
	t.Setenv("NEKONEST_CURSOR_CLI", grokAgent)
	if findCursorAgentCLI() == grokAgent {
		t.Fatal("NEKONEST_CURSOR_CLI accepted a generic agent.exe")
	}
	t.Setenv("NEKONEST_CURSOR_CLI", agent)
	if findCursorAgentCLI() != agent {
		t.Fatalf("NEKONEST_CURSOR_CLI ignored: %q", findCursorAgentCLI())
	}

	shimDir := filepath.Join(root, "cursor-agent")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(shimDir, "cursor-agent.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !looksLikeCursorAgentCLI(shim) {
		t.Fatal("official cursor-agent.cmd shim rejected")
	}
}

func TestResolveCursorAgentLaunchUsesVersionedNode(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "cursor-agent.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	version := filepath.Join(root, "versions", "2026.08.11-e8db854")
	if err := os.MkdirAll(version, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
	}
	node := filepath.Join(version, nodeName)
	script := filepath.Join(version, "index.js")
	if err := os.WriteFile(node, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe, argv, handled, err := resolveCursorAgentLaunch(shim, []string{"--help"})
	if err != nil || !handled {
		t.Fatalf("resolve: handled=%v err=%v", handled, err)
	}
	if exe != node || len(argv) != 2 || argv[0] != script || argv[1] != "--help" {
		t.Fatalf("launch = %q %#v", exe, argv)
	}
}

func TestZcodeRuntimeDisabledByDefault(t *testing.T) {
	zcode := NewZCodeCommander()
	if zcode.IsAvailable() {
		t.Fatal("zcode must stay unavailable by default")
	}
	if err := zcode.ProbeThreadStart(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("default probe = %v", err)
	}
}

func TestZcodeProbeRequiresHeadlessConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZCODE_HOME", root)
	cli := writeFakeHeadlessCLI(t, root, "zcode.cjs")
	zcode := NewZCodeCommander()
	zcode.SetCLIPath(cli)
	if err := zcode.ProbeThreadStart(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("disabled probe = %v", err)
	}
	zcode.EnableRuntimeForTest()
	if err := zcode.ProbeThreadStart(context.Background()); err == nil || !strings.Contains(err.Error(), "model config is missing") {
		t.Fatalf("probe without config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cli", "config.json"), []byte(`{"provider":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	zcode.EnableRuntimeForTest()
	if err := zcode.ProbeThreadStart(context.Background()); err != nil {
		t.Fatalf("probe with config: %v", err)
	}
}

func TestZcodeStartFailsWhenProcessExitsWithoutAck(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZCODE_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cli", "config.json"), []byte(`{"provider":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cli := writeFakeHeadlessCLI(t, root, "zcode.cjs")
	zcode := NewZCodeCommander()
	zcode.SetCLIPath(cli)
	zcode.EnableRuntimeForTest()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	_, created, accepted, err := zcode.StartThread(ctx, root, "ping", nil, nil)
	if created || accepted || err == nil || !strings.Contains(err.Error(), "initial prompt was not confirmed") {
		t.Fatalf("start = created=%v accepted=%v err=%v", created, accepted, err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("start waited too long: %s", time.Since(started))
	}
}

func writeFakeHeadlessCLI(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	script := `const args = process.argv.slice(2).join(" ");
if (args.includes("--help")) {
  process.stdout.write("--resume --prompt --json --print --output-format --force --add-dir\n");
  process.exit(0);
}
process.stderr.write("Error: Model config is missing. Create config.json with an explicit model provider before running ZCode.\n");
process.exit(1);
`
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLiveZcodeAndCursorHelpProbes(t *testing.T) {
	zcode := NewZCodeCommander()
	if zcode.IsAvailable() {
		t.Fatal("installed zcode must stay unavailable while headless login is broken")
	}
	local := os.Getenv("LOCALAPPDATA")
	shim := filepath.Join(local, "cursor-agent", "cursor-agent.cmd")
	if !fileExists(shim) {
		t.Skip("official cursor-agent shim not installed")
	}
	t.Setenv("NEKONEST_CURSOR_CLI", "")
	cursor := NewCursorCommander()
	if !cursor.IsAvailable() {
		t.Fatalf("cursor available=false path=%q", cursor.CLIPath())
	}
	if err := cursor.ProbeThreadStart(context.Background()); err != nil {
		t.Fatalf("cursor probe: %v path=%q", err, cursor.CLIPath())
	}
}

func TestZcodeJSONStreamExtractsAssistantText(t *testing.T) {
	var events []string
	c := NewZCodeCommander()
	c.OnAgentOutput = func(sessionID, msgType, content, msgID string) {
		events = append(events, msgType+":"+content)
	}
	c.parseAndForwardOutput("sess", `{"type":"text","text":"hello from zcode"}`)
	c.parseAndForwardOutput("sess", `{"type":"session","session_id":"sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`)
	c.parseAndForwardOutput("sess", `{"type":"tool_call","text":"should not appear"}`)
	c.flushStream("sess")
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, "hello from zcode") || strings.Contains(joined, "should not appear") {
		t.Fatalf("events=%q", events)
	}
}

func TestCursorJSONStreamIgnoresResultAndNonAssistant(t *testing.T) {
	var events []string
	c := NewCursorCommander()
	c.OnAgentOutput = func(sessionID, msgType, content, msgID string) {
		events = append(events, msgType+":"+content)
	}
	c.parseAndForwardOutput("chat", `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	c.parseAndForwardOutput("chat", `{"type":"result","result":"hello"}`)
	c.parseAndForwardOutput("chat", `{"type":"user","message":{"content":[{"type":"text","text":"secret"}]}}`)
	c.flushStream("chat")
	joined := strings.Join(events, "\n")
	if strings.Count(joined, "hello") != 1 || strings.Contains(joined, "secret") {
		t.Fatalf("events=%q", events)
	}
}
