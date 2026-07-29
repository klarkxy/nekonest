package agentexec

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nekonest/daemon/internal/attach"
)

func attachmentPaths(files []attach.LocalFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file.Path) != "" {
			paths = append(paths, file.Path)
		}
	}
	return paths
}

func attachmentDirs(files []attach.LocalFile) []string {
	dirs := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if strings.TrimSpace(file.Path) == "" {
			continue
		}
		dir := filepath.Clean(filepath.Dir(file.Path))
		key := dir
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}

func codexImagePaths(files []attach.LocalFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if file.Path == "" || !isCodexImage(file) {
			continue
		}
		paths = append(paths, file.Path)
	}
	return paths
}

func isCodexImage(file attach.LocalFile) bool {
	switch strings.ToLower(strings.TrimSpace(file.MIME)) {
	case "image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif":
		return true
	}
	switch strings.ToLower(filepath.Ext(file.Path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	}
	return false
}

func completePrompt(callback func()) {
	if callback != nil {
		callback()
	}
}
