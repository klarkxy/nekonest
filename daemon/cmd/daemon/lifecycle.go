package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nekonest/daemon/internal/config"
	"github.com/nekonest/daemon/internal/hostsvc"
)

func runHostServiceCLI(cmd string, args []string) int {
	cfgPath, err := parseHostServiceFlags(cmd, hostsvc.ArgsWithoutCommand(args, cmd))
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return runHostServiceCommand(cmd, cfgPath)
}

func parseHostServiceFlags(cmd string, args []string) (string, error) {
	fs := flag.NewFlagSet("nekonest-daemon "+cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := fs.String("config", "", "config file path (default: ~/.nekonest/config.json)")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() > 0 {
		return "", fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return *cfg, nil
}

func runHostServiceCommand(cmd, configFlag string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve daemon executable:", err)
		return 1
	}
	cfgPath := config.DefaultConfigPath()
	if strings.TrimSpace(configFlag) != "" {
		cfgPath = configFlag
	}
	mgr := hostsvc.New(hostsvc.Spec{Executable: exe, ConfigPath: cfgPath})
	switch cmd {
	case "install":
		if hostsvc.IsEphemeralPath(exe) {
			fmt.Fprintln(os.Stderr, "warning: executable path looks temporary; keep the daemon in a stable directory before install")
		}
		if err := mgr.Install(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitHostService(err)
		}
		fmt.Printf("Installed current-user autostart (%s).\n", mgr.UnitName())
		fmt.Println("The OS starts the daemon at logon. Start it now with:")
		fmt.Println("  nekonest-daemon start")
		return 0
	case "uninstall":
		if err := mgr.Uninstall(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitHostService(err)
		}
		fmt.Printf("Removed current-user autostart (%s).\n", mgr.UnitName())
		return 0
	case "start":
		if err := mgr.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitHostService(err)
		}
		fmt.Printf("Started %s.\n", mgr.UnitName())
		return 0
	case "stop":
		if err := mgr.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitHostService(err)
		}
		fmt.Printf("Stopped %s.\n", mgr.UnitName())
		return 0
	case "status":
		st, err := mgr.Status()
		held, lockErr := instanceLockHeld(mgr.ConfigPath())
		fmt.Print(formatHostServiceStatus(st, held, lockErr))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitHostService(err)
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown host service command %q\n", cmd)
		return 2
	}
}

func exitHostService(err error) int {
	if errors.Is(err, hostsvc.ErrUnsupported) {
		return 2
	}
	return 1
}

func instanceLockHeld(configPath string) (bool, error) {
	path := configPath + ".daemon.lock"
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	lock, err := acquireDaemonInstanceLock(path)
	if err != nil {
		if errors.Is(err, errDaemonInstanceLockHeld) {
			return true, nil
		}
		return false, err
	}
	return false, lock.Close()
}

func formatHostServiceStatus(st hostsvc.Status, lockHeld bool, lockErr error) string {
	var b strings.Builder
	if !st.Supported {
		b.WriteString(hostsvc.ErrUnsupported.Error() + "\n")
	}
	fmt.Fprintf(&b, "NekoNest host service\n")
	fmt.Fprintf(&b, "  platform: %s\n", valueOrDash(st.Platform))
	fmt.Fprintf(&b, "  supervisor: %s\n", valueOrDash(st.Supervisor))
	fmt.Fprintf(&b, "  unit: %s\n", valueOrDash(st.UnitName))
	fmt.Fprintf(&b, "  installed: %s\n", yesNo(st.Installed))
	fmt.Fprintf(&b, "  enabled: %s\n", yesNo(st.Enabled))
	fmt.Fprintf(&b, "  supervisor_state: %s\n", valueOrDash(st.SupervisorState))
	fmt.Fprintf(&b, "  executable: %s\n", valueOrDash(st.Executable))
	fmt.Fprintf(&b, "  config: %s\n", valueOrDash(st.ConfigPath))
	if st.ConfiguredExec != "" {
		fmt.Fprintf(&b, "  supervisor_exec: %s\n", st.ConfiguredExec)
	}
	if st.ConfiguredArgs != "" {
		fmt.Fprintf(&b, "  supervisor_args: %s\n", st.ConfiguredArgs)
	}
	switch {
	case lockErr != nil:
		fmt.Fprintf(&b, "  process_lock: unknown (%v)\n", lockErr)
	case lockHeld:
		b.WriteString("  process_lock: held\n")
	default:
		b.WriteString("  process_lock: free\n")
	}
	if st.Detail != "" {
		fmt.Fprintf(&b, "  detail: %s\n", st.Detail)
	}
	return b.String()
}

func hostDaemonUsage() {
	out := flag.CommandLine.Output()
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(out, "Usage of %s:\n", name)
	fmt.Fprintf(out, "  %s [flags]                 run the daemon in the foreground\n", name)
	fmt.Fprintf(out, "  %s install [-config path]  register current-user autostart\n", name)
	fmt.Fprintf(out, "  %s uninstall [-config path] remove current-user autostart\n", name)
	fmt.Fprintf(out, "  %s start [-config path]    start the installed supervisor\n", name)
	fmt.Fprintf(out, "  %s stop [-config path]     stop the installed supervisor\n", name)
	fmt.Fprintf(out, "  %s status [-config path]   show supervisor and process-lock state\n", name)
	fmt.Fprintf(out, "\nFlags:\n")
	flag.PrintDefaults()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func valueOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
