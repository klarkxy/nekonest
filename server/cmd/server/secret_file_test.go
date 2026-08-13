package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadAdminSecretFromPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-secret")
	if err := os.WriteFile(path, []byte("secret-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEKONEST_ADMIN_SECRET_FILE", path)
	t.Setenv("NEKONEST_ADMIN_SECRET", "")
	t.Setenv("NEKONEST_PHONE_SECRET", "")
	secret, deprecated, err := loadAdminSecret()
	if err != nil {
		t.Fatal(err)
	}
	if secret != "secret-from-file" || deprecated {
		t.Fatalf("secret=%q deprecated=%v", secret, deprecated)
	}
}

func TestLoadAdminSecretRejectsAmbiguousInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-secret")
	if err := os.WriteFile(path, []byte("file-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEKONEST_ADMIN_SECRET_FILE", path)
	t.Setenv("NEKONEST_ADMIN_SECRET", "inline-value")
	if _, _, err := loadAdminSecret(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("ambiguous secret error = %v", err)
	}
}

func TestReadPrivateSecretFileRejectsUnsafeOrOversizedInput(t *testing.T) {
	dir := t.TempDir()
	unsafe := filepath.Join(dir, "unsafe")
	if err := os.WriteFile(unsafe, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if _, err := readPrivateSecretFile(unsafe); err == nil || !strings.Contains(err.Error(), "group/world") {
			t.Fatalf("unsafe mode error = %v", err)
		}
	}
	// WriteFile only applies the mode when creating a file. Reuse a new path so
	// the oversized check is not preempted by leftover 0644 permissions.
	oversized := filepath.Join(dir, "oversized")
	if err := os.WriteFile(oversized, make([]byte, maxAdminSecretFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateSecretFile(oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized secret error = %v", err)
	}
}

func TestReadPrivateSecretFileRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "admin-secret")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readPrivateSecretFile(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}
