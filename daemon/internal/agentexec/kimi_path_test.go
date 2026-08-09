package agentexec

import (
	"path/filepath"
	"testing"
)

func TestKimiSearchPathsIncludesCodeHomeAndDefaultInstall(t *testing.T) {
	paths := kimiSearchPaths(`C:\AppData`, `C:\Users\neko`, `D:\kimi-home`)
	want := map[string]bool{
		filepath.Join(`D:\kimi-home`, "bin", "kimi.exe"):            false,
		filepath.Join(`C:\Users\neko`, ".kimi-code", "bin", "kimi"): false,
	}
	for _, path := range paths {
		if _, ok := want[path]; ok {
			want[path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Fatalf("missing Kimi candidate %s in %#v", path, paths)
		}
	}
}
