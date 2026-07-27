package agentexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExtractKiloText(t *testing.T) {
	if extractKiloText(map[string]interface{}{"text": "a"}) != "a" {
		t.Fatal("text")
	}
	if extractKiloText(map[string]interface{}{
		"part": map[string]interface{}{"text": "p"},
	}) != "p" {
		t.Fatal("part")
	}
}

func TestExtractClaudeText(t *testing.T) {
	msg := map[string]interface{}{
		"message": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hi"},
				map[string]interface{}{"type": "tool_use", "name": "Read"},
			},
		},
	}
	got := extractClaudeText(msg)
	if !strings.Contains(got, "hi") || !strings.Contains(got, "[tool: Read]") {
		t.Fatalf("%q", got)
	}
}

func TestExtractCodexText(t *testing.T) {
	if extractCodexText(map[string]interface{}{"content": "c"}) != "c" {
		t.Fatal("str")
	}
}

func TestResolveLaunchNoCmdShell(t *testing.T) {
	exe, args, err := resolveLaunch("foo.exe", []string{"a", "b%PATH%"})
	if err != nil || exe != "foo.exe" || args[1] != "b%PATH%" {
		t.Fatalf("%s %#v %v", exe, args, err)
	}
	if runtime.GOOS != "windows" {
		return
	}
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "tool.cmd")
	// Unresolvable shim must error (never cmd.exe)
	_ = os.WriteFile(cmdPath, []byte("@echo off\r\necho hi\r\n"), 0o644)
	_, _, err = resolveLaunch(cmdPath, []string{"%PATH%&calc"})
	if err == nil {
		t.Fatal("expected refuse unresolved .cmd")
	}
	// Resolvable npm-style shim
	node := filepath.Join(dir, "node.exe")
	script := filepath.Join(dir, "cli.js")
	_ = os.WriteFile(node, []byte("MZ"), 0o755)
	_ = os.WriteFile(script, []byte("console.log(1)"), 0o644)
	shim := filepath.Join(dir, "pkg.cmd")
	content := "@ECHO off\r\n" +
		`"` + node + `" "` + script + `" %*` + "\r\n"
	_ = os.WriteFile(shim, []byte(content), 0o644)
	exe, args, err = resolveLaunch(shim, []string{"hello%PATH%", "line2"})
	if err != nil {
		t.Fatal(err)
	}
	if exe != node || len(args) < 3 || args[0] != script || args[1] != "hello%PATH%" {
		t.Fatalf("resolved %#v %#v", exe, args)
	}

	// Exact structure emitted by current npm on Windows. In particular, the
	// final command uses %_prog% and %dp0%, not the older %~dp0 form.
	npmDir := filepath.Join(dir, "npm")
	if err := os.MkdirAll(filepath.Join(npmDir, "node_modules", "@openai", "codex", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	localNode := filepath.Join(npmDir, "node.exe")
	codexJS := filepath.Join(npmDir, "node_modules", "@openai", "codex", "bin", "codex.js")
	if err := os.WriteFile(localNode, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexJS, []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	realisticShim := filepath.Join(npmDir, "codex.cmd")
	realisticContent := `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
  SET PATHEXT=%PATHEXT:;.JS;=;%
)

endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@openai\codex\bin\codex.js" %*
`
	if err := os.WriteFile(realisticShim, []byte(realisticContent), 0o644); err != nil {
		t.Fatal(err)
	}
	exe, args, err = resolveLaunch(realisticShim, []string{"100% safe", "line1\nline2"})
	if err != nil {
		t.Fatalf("real npm shim did not resolve: %v", err)
	}
	if exe != localNode || len(args) != 3 || args[0] != codexJS ||
		args[1] != "100% safe" || args[2] != "line1\nline2" {
		t.Fatalf("real npm shim resolved %#v %#v", exe, args)
	}
}

func TestParseSessionIDFromPath(t *testing.T) {
	if ParseSessionIDFromPath(`/x/abc-def.jsonl`) != "abc-def" {
		t.Fatal("id")
	}
}

func TestWindowsManagedProcessStartsAndResumes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Job Object smoke test")
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not available")
	}
	executor := NewAgentExecutor("test", "managed-process")
	output := make(chan string, 1)
	executor.OnOutput = func(line string) {
		select {
		case output <- line:
		default:
		}
	}
	if err := executor.Start(powershell, []string{
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"Write-Output managed-ok",
	}, nil); err != nil {
		t.Fatalf("start managed process: %v", err)
	}
	_ = executor.CloseStdin()

	select {
	case line := <-output:
		if strings.TrimSpace(line) != "managed-ok" {
			t.Fatalf("unexpected process output %q", line)
		}
	case <-time.After(5 * time.Second):
		_ = executor.Stop()
		t.Fatal("managed process was not resumed")
	}
	select {
	case <-executor.WaitExit():
	case <-time.After(5 * time.Second):
		_ = executor.Stop()
		t.Fatal("managed process did not exit")
	}
}

func TestAgentExecutorImmediateExitHelper(t *testing.T) {
	if os.Getenv("NEKONEST_EXECUTOR_EXIT_HELPER") != "1" {
		return
	}
}

func TestAgentExecutorExitChannelBelongsToOneRun(t *testing.T) {
	executor := NewAgentExecutor("test", "reused")
	for iteration := 0; iteration < 25; iteration++ {
		if err := executor.Start(
			os.Args[0],
			[]string{"-test.run=^TestAgentExecutorImmediateExitHelper$"},
			[]string{"NEKONEST_EXECUTOR_EXIT_HELPER=1"},
		); err != nil {
			t.Fatalf("start iteration %d: %v", iteration, err)
		}
		runExit := executor.WaitExit()
		deadline := time.Now().Add(5 * time.Second)
		for executor.IsRunning() && time.Now().Before(deadline) {
			runtime.Gosched()
		}
		if executor.IsRunning() {
			_ = executor.Stop()
			t.Fatalf("iteration %d did not exit", iteration)
		}
		// running=false is published only after this exact run channel is
		// closed. This invariant prevents a new Start from replacing exitCh
		// before the old waiter signals completion.
		select {
		case <-runExit:
		default:
			t.Fatalf("iteration %d became restartable before its exit channel closed", iteration)
		}
	}
}

func TestAgentExecutorSleepHelper(t *testing.T) {
	if os.Getenv("NEKONEST_EXECUTOR_SLEEP_HELPER") != "1" {
		return
	}
	time.Sleep(3 * time.Second)
}

func TestAgentExecutorStopUsesOnlyStoppedRunSnapshots(t *testing.T) {
	executor := NewAgentExecutor("test", "stop-restart")
	startSleep := func() error {
		return executor.Start(
			os.Args[0],
			[]string{"-test.run=^TestAgentExecutorSleepHelper$"},
			[]string{"NEKONEST_EXECUTOR_SLEEP_HELPER=1"},
		)
	}
	if err := startSleep(); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() {
		stopDone <- executor.Stop()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for executor.IsRunning() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if executor.IsRunning() {
		t.Fatal("stopped run did not exit")
	}
	if err := startSleep(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("old Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = executor.Stop()
		t.Fatal("old Stop waited on the restarted run")
	}
	time.Sleep(50 * time.Millisecond)
	if !executor.IsRunning() {
		t.Fatal("old Stop cancelled or killed the restarted run")
	}
	if err := executor.Stop(); err != nil {
		t.Fatalf("stop restarted run: %v", err)
	}
}
