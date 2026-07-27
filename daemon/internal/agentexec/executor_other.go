//go:build !windows

package agentexec

func wrapWindowsShellScript(command string, args []string) (exe string, argv []string, cmdLine string) {
	return command, args, ""
}
