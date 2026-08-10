package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestListenAddressDefaultsToLoopbackWithoutSecret(t *testing.T) {
	if got := listenAddress("8080", ""); got != "127.0.0.1:8080" {
		t.Fatalf("listenAddress without secret = %q", got)
	}
	if got := listenAddress("8080", "secret"); got != ":8080" {
		t.Fatalf("listenAddress with secret = %q", got)
	}
}

func TestDefaultLocalOriginsRejectsDNSRebindingHosts(t *testing.T) {
	got := defaultLocalOrigins("8080")
	if got != "http://127.0.0.1:8080,http://localhost:8080,http://[::1]:8080" {
		t.Fatalf("defaultLocalOrigins=%q", got)
	}
}

func TestPreparePrivateDirectoryTightensExistingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateDirectory(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != privateDirectoryMode {
		t.Fatalf("data directory mode=%#o want=%#o", got, privateDirectoryMode)
	}
}
