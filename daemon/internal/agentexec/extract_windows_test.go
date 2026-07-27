//go:build windows

package agentexec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitQuoted(t *testing.T) {
	parts := splitQuoted(`"C:\n\node.exe" "C:\n\a.js" %*`)
	if len(parts) < 2 {
		t.Fatalf("%#v", parts)
	}
}

func TestResolveInstalledCodexNpmShim(t *testing.T) {
	shim := filepath.Join(os.Getenv("APPDATA"), "npm", "codex.cmd")
	if _, err := os.Stat(shim); err != nil {
		t.Skip("codex.cmd is not installed through npm")
	}
	node, script, ok := resolveNpmCmdShim(shim)
	if !ok {
		t.Fatalf("installed npm shim did not resolve: %s", shim)
	}
	if strings.EqualFold(filepath.Ext(node), ".cmd") || strings.EqualFold(filepath.Ext(node), ".bat") {
		t.Fatalf("resolved through a command shell: %s", node)
	}
	if _, err := os.Stat(node); err != nil {
		t.Fatalf("resolved node missing: %s: %v", node, err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("resolved Codex script missing: %s: %v", script, err)
	}
	t.Logf("resolved installed codex.cmd to %s %s", node, script)
}
