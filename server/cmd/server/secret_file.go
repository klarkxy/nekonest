package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

const maxAdminSecretFileBytes = 8 << 10

func loadAdminSecret() (secret string, deprecatedAlias bool, err error) {
	filePath := strings.TrimSpace(os.Getenv("NEKONEST_ADMIN_SECRET_FILE"))
	adminSecret := strings.TrimSpace(os.Getenv("NEKONEST_ADMIN_SECRET"))
	phoneSecret := strings.TrimSpace(os.Getenv("NEKONEST_PHONE_SECRET"))
	if filePath != "" {
		if adminSecret != "" || phoneSecret != "" {
			return "", false, fmt.Errorf("NEKONEST_ADMIN_SECRET_FILE cannot be combined with inline admin secret variables")
		}
		secret, err := readPrivateSecretFile(filePath)
		return secret, false, err
	}
	if adminSecret != "" {
		return adminSecret, false, nil
	}
	if phoneSecret != "" {
		return phoneSecret, true, nil
	}
	return "", false, nil
}

func readPrivateSecretFile(path string) (string, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("stat admin secret file: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return "", fmt.Errorf("admin secret file must be a regular file, not a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open admin secret file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat admin secret file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("admin secret file must be a regular file")
	}
	if !os.SameFile(pathInfo, info) {
		return "", fmt.Errorf("admin secret file changed while opening")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("admin secret file must not be group/world accessible")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAdminSecretFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read admin secret file: %w", err)
	}
	if len(data) > maxAdminSecretFileBytes {
		return "", fmt.Errorf("admin secret file exceeds 8 KiB")
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("admin secret file is empty")
	}
	return secret, nil
}
