package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nekonest/server/internal/db"
)

func TestMigrationArtifactsUsePrivateModesOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs rather than POSIX permission bits")
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "nekonest.db")
	database, err := db.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0o644); err != nil {
		t.Fatal(err)
	}
	attachmentDir := filepath.Join(dataDir, "attachments", "nested")
	if err := os.MkdirAll(attachmentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	attachmentPath := filepath.Join(attachmentDir, "secret.txt")
	if err := os.WriteFile(attachmentPath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	backupDir := filepath.Join(root, "backup")
	if err := runMigrateV1(dataDir, backupDir); err != nil {
		t.Fatal(err)
	}

	assertMode := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode=%#o want=%#o", path, got, want)
		}
	}
	for _, path := range []string{
		dataDir,
		filepath.Join(dataDir, "attachments"),
		backupDir,
		filepath.Join(backupDir, "attachments"),
		filepath.Join(backupDir, "attachments", "nested"),
	} {
		assertMode(path, privateDirectoryMode)
	}
	for _, path := range []string{
		dbPath,
		filepath.Join(backupDir, "nekonest.db"),
		filepath.Join(backupDir, "nekonest.db.sha256"),
		filepath.Join(backupDir, "attachments", "nested", "secret.txt"),
	} {
		assertMode(path, privateFileMode)
	}
}
