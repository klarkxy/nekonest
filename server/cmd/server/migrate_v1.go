package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/opslog"
)

// runMigrateV1 performs the offline destructive v0.1 → v1 content wipe
// while preserving device registrations/token hashes.
func runMigrateV1(dataDir, backupDir string) error {
	if dataDir == "" {
		return fmt.Errorf("-data required")
	}
	if backupDir == "" {
		return fmt.Errorf("-backup required (verified copy destination)")
	}
	if err := preparePrivateDirectory(dataDir); err != nil {
		return fmt.Errorf("secure data directory: %w", err)
	}
	dbPath := filepath.Join(dataDir, "nekonest.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("database not found: %w", err)
	}
	if absData, _ := filepath.Abs(dataDir); absData != "" {
		if absBak, _ := filepath.Abs(backupDir); absBak == absData {
			return fmt.Errorf("-backup must not equal -data")
		}
	}

	if err := preparePrivateDirectory(backupDir); err != nil {
		return err
	}
	if err := copyFile(dbPath, filepath.Join(backupDir, "nekonest.db")); err != nil {
		return fmt.Errorf("backup db: %w", err)
	}
	for _, suf := range []string{"-wal", "-shm"} {
		src := dbPath + suf
		if _, err := os.Stat(src); err == nil {
			_ = copyFile(src, filepath.Join(backupDir, "nekonest.db"+suf))
		}
	}
	attSrc := filepath.Join(dataDir, "attachments")
	attDst := filepath.Join(backupDir, "attachments")
	if st, err := os.Stat(attSrc); err == nil && st.IsDir() {
		if err := copyDir(attSrc, attDst); err != nil {
			return fmt.Errorf("backup attachments: %w", err)
		}
	}
	sum, err := fileSHA256(filepath.Join(backupDir, "nekonest.db"))
	if err != nil {
		return err
	}
	if err := writePrivateFile(filepath.Join(backupDir, "nekonest.db.sha256"), []byte(sum+"\n")); err != nil {
		return fmt.Errorf("write backup checksum: %w", err)
	}
	opslog.Info("server.migration", "backup_verified", "migration backup verified", "checksum_prefix", sum[:16])

	database, err := db.New(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.ClearPlaintextContentForV1(); err != nil {
		return err
	}
	opslog.Info("server.migration", "plaintext_cleared", "plaintext records cleared")
	opslog.Info("server.migration", "devices_preserved", "device identities preserved")
	opslog.Info("server.migration", "phone_reauth_required", "phone reauthentication required")

	if err := os.RemoveAll(attSrc); err == nil {
		if err := preparePrivateDirectory(attSrc); err != nil {
			return fmt.Errorf("recreate attachments directory: %w", err)
		}
	}
	opslog.Info("server.migration", "attachments_cleared", "live attachments cleared")
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := preparePrivateDirectory(filepath.Dir(dst)); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := out.Chmod(privateFileMode); err != nil {
		_ = out.Close()
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return preparePrivateDirectory(target)
		}
		return copyFile(path, target)
	})
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
