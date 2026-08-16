package agentexec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

func isNodeScript(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".cjs", ".mjs":
		return true
	default:
		return false
	}
}

func findNodeExecutable() string {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("node.exe"); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	return ""
}

func nodeScriptRunnable(path string) bool {
	if !isNodeScript(path) {
		return false
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return findNodeExecutable() != ""
}

func wrapNodeScript(command string, args []string) (string, []string, bool, error) {
	if !isNodeScript(command) {
		return "", nil, false, nil
	}
	node := findNodeExecutable()
	if node == "" {
		return "", nil, true, fmt.Errorf("node is required to launch %s", command)
	}
	return node, append([]string{command}, args...), true, nil
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func isElectronGUIBinary(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	dir := filepath.Dir(path)
	switch base {
	case "cursor.exe":
		return true
	case "zcode.exe", "zcode":
		if _, err := os.Stat(filepath.Join(dir, "resources", "app.asar")); err == nil {
			return true
		}
	}
	return false
}

func cliLookPath(names ...string) string {
	for _, name := range names {
		p, err := exec.LookPath(name)
		if err != nil || isElectronGUIBinary(p) {
			continue
		}
		return p
	}
	return ""
}

func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" || isElectronGUIBinary(path) {
			continue
		}
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func cursorAgentBaseName(path string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".cmd")
	return base
}

var cursorAgentVersionDir = regexp.MustCompile(`^\d{4}\.\d{1,2}\.\d{1,2}(-\d{2}-\d{2}-\d{2})?-[a-f0-9]+$`)

func looksLikeCursorAgentCLI(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || isElectronGUIBinary(path) {
		return false
	}
	name := cursorAgentBaseName(path)
	switch name {
	case "cursor-agent":
		return true
	case "agent":
		dir := strings.ToLower(filepath.ToSlash(filepath.Dir(filepath.Clean(path))))
		return strings.Contains(dir, "cursor")
	default:
		return strings.HasPrefix(name, "cursor-agent")
	}
}

func resolveCursorAgentLaunch(command string, args []string) (string, []string, bool, error) {
	if !looksLikeCursorAgentCLI(command) {
		return "", nil, false, nil
	}
	switch strings.ToLower(filepath.Ext(command)) {
	case ".cmd", ".ps1", ".bat":
	default:
		return "", nil, false, nil
	}
	node, script, err := findCursorAgentNodeEntry(filepath.Dir(command))
	if err != nil {
		return "", nil, true, err
	}
	return node, append([]string{script}, args...), true, nil
}

func findCursorAgentNodeEntry(root string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", fmt.Errorf("cursor-agent directory is empty")
	}
	directNode := filepath.Join(root, "node.exe")
	if runtime.GOOS != "windows" {
		directNode = filepath.Join(root, "node")
	}
	directScript := filepath.Join(root, "index.js")
	if fileExists(directNode) && fileExists(directScript) {
		return directNode, directScript, nil
	}
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		return "", "", fmt.Errorf("cursor-agent versions: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && cursorAgentVersionDir.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", "", fmt.Errorf("no cursor-agent version directory in %s", root)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] > names[j] })
	node := filepath.Join(root, "versions", names[0], "node.exe")
	if runtime.GOOS != "windows" {
		node = filepath.Join(root, "versions", names[0], "node")
	}
	script := filepath.Join(root, "versions", names[0], "index.js")
	if !fileExists(node) || !fileExists(script) {
		return "", "", fmt.Errorf("cursor-agent %s is missing node or index.js", names[0])
	}
	return node, script, nil
}
