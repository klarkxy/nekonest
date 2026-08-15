//go:build windows

package agentexec

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/nekonest/daemon/internal/opslog"
)

// resolveLaunch never routes user-controlled args through cmd.exe.
// npm .cmd shims are resolved to node.exe + .js so prompts keep %, !, and newlines.
func resolveLaunch(command string, args []string) (exe string, argv []string, err error) {
	ext := strings.ToLower(filepath.Ext(command))
	if ext != ".cmd" && ext != ".bat" {
		return command, args, nil
	}
	if node, script, ok := resolveNpmCmdShim(command); ok {
		return node, append([]string{script}, args...), nil
	}
	return "", nil, fmt.Errorf("refusing to run %s via cmd.exe (prompt would be corrupted); install a .exe shim or ensure node resolves the npm package", command)
}

func releaseJobObject(job uintptr) {
	if job == 0 {
		return
	}
	closeJobHandle(syscall.Handle(job))
}

// resolveNpmCmdShim parses a typical npm global .cmd wrapper:
//
//	"%~dp0\node.exe" "%~dp0\node_modules\...\bin\cli.js" %*
func resolveNpmCmdShim(cmdPath string) (node string, script string, ok bool) {
	f, err := os.Open(cmdPath)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	dir := filepath.Dir(cmdPath)
	sc := bufio.NewScanner(f)
	var jsCand string
	// npm shims are small
	for i := 0; i < 80 && sc.Scan(); i++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "REM") || strings.HasPrefix(line, "::") {
			continue
		}
		// Modern npm shims end with:
		// endLocal & ... & "%_prog%" "%dp0%\node_modules\pkg\cli.js" %*
		// Older shims use "%~dp0\node.exe" directly. We do not evaluate the
		// batch program; we only extract an existing JavaScript entry point.
		lowerLine := strings.ToLower(line)
		if !strings.Contains(lowerLine, ".js") &&
			!strings.Contains(lowerLine, ".mjs") &&
			!strings.Contains(lowerLine, ".cjs") {
			continue
		}
		parts := splitQuoted(line)
		var nodeCand string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" || p == "%*" {
				continue
			}
			p = expandNpmShimDir(p, dir)
			if strings.Contains(p, "%") {
				// Do not attempt to interpret arbitrary batch variables such as
				// %_prog% or %COMSPEC%.
				continue
			}
			p = filepath.Clean(p)
			low := strings.ToLower(p)
			if strings.HasSuffix(low, "node.exe") || filepath.Base(low) == "node.exe" {
				nodeCand = p
			}
			if strings.HasSuffix(low, ".js") || strings.HasSuffix(low, ".mjs") || strings.HasSuffix(low, ".cjs") {
				jsCand = p
			}
		}
		if nodeCand != "" && jsCand != "" {
			if st, err := os.Stat(nodeCand); err == nil && !st.IsDir() {
				if st2, err := os.Stat(jsCand); err == nil && !st2.IsDir() {
					return nodeCand, jsCand, true
				}
			}
		}
	}
	if jsCand == "" {
		return "", "", false
	}
	if st, err := os.Stat(jsCand); err != nil || st.IsDir() {
		return "", "", false
	}
	// npm's %_prog% first prefers node.exe beside the shim.
	localNode := filepath.Join(dir, "node.exe")
	if st, err := os.Stat(localNode); err == nil && !st.IsDir() {
		return localNode, jsCand, true
	}
	// Otherwise npm resolves "node" through PATH.
	if n, err := exec.LookPath("node.exe"); err == nil {
		return n, jsCand, true
	}
	if n, err := exec.LookPath("node"); err == nil {
		return n, jsCand, true
	}
	return "", "", false
}

func expandNpmShimDir(value, dir string) string {
	replacement := dir + string(filepath.Separator)
	for _, token := range []string{"%~dp0", "%~DP0", "%dp0%", "%DP0%", "%DP%", "%dp%"} {
		value = strings.ReplaceAll(value, token, replacement)
	}
	return value
}

func splitQuoted(s string) []string {
	var out []string
	var b strings.Builder
	inQ := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQ = !inQ
		case (c == ' ' || c == '\t') && !inQ:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func interruptProcess(p *os.Process) error {
	return killProcessTree(p)
}

// startManagedProcess eliminates the child-escape window by starting the
// process suspended, assigning it to a kill-on-close Job Object, then resuming
// it. If the host does not permit Job Objects, execution continues with an
// explicit warning and taskkill /T is used as the termination fallback.
func startManagedProcess(cmd *exec.Cmd) (uintptr, error) {
	configureBackgroundProcess(cmd)
	job, err := createJobObject()
	if err != nil || job == 0 {
		opslog.Warn("daemon.agentexec", "job_object_create_failed", "Windows Job Object creation failed; taskkill tree fallback enabled", "status", "fallback")
		return 0, cmd.Start()
	}
	if err := setJobKillOnClose(job); err != nil {
		closeJobHandle(job)
		opslog.Warn("daemon.agentexec", "job_object_configure_failed", "Windows Job Object configuration failed; taskkill tree fallback enabled", "status", "fallback")
		return 0, cmd.Start()
	}

	const createSuspended = 0x00000004
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createSuspended
	if err := cmd.Start(); err != nil {
		closeJobHandle(job)
		return 0, err
	}
	processHandle, err := openProcessHandle(uint32(cmd.Process.Pid))
	if err != nil {
		closeJobHandle(job)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, fmt.Errorf("open suspended process: %w", err)
	}
	defer closeJobHandle(processHandle)
	if err := assignProcessHandleToJob(job, processHandle); err != nil {
		closeJobHandle(job)
		if resumeErr := resumeProcess(processHandle); resumeErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return 0, fmt.Errorf("assign Job Object: %v; resume fallback: %w", err, resumeErr)
		}
		opslog.Warn("daemon.agentexec", "job_object_assign_failed", "Windows Job Object assignment failed; taskkill tree fallback enabled", "status", "fallback")
		return 0, nil
	}
	if err := resumeProcess(processHandle); err != nil {
		// Closing the assigned kill-on-close job terminates the suspended
		// process before it can execute user code or create children.
		closeJobHandle(job)
		_ = cmd.Wait()
		return 0, fmt.Errorf("resume managed process: %w", err)
	}
	return uintptr(job), nil
}

func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	if _, err := os.Stat(taskkill); err != nil {
		taskkill = "taskkill.exe"
	}
	cmd := exec.Command(taskkill, "/PID", strconv.Itoa(p.Pid), "/T", "/F")
	configureBackgroundProcess(cmd)
	errTree := cmd.Run()
	errParent := p.Kill()
	if errTree == nil || errParent == nil {
		return nil
	}
	return fmt.Errorf("taskkill process tree: %v; kill parent: %w", errTree, errParent)
}

func closeJobHandle(h syscall.Handle) {
	if h != 0 {
		_ = syscall.CloseHandle(h)
	}
}

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	modntdll                     = syscall.NewLazyDLL("ntdll.dll")
	procCreateJobObjectW         = modkernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = modkernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = modkernel32.NewProc("AssignProcessToJobObject")
	procOpenProcess              = modkernel32.NewProc("OpenProcess")
	procNtResumeProcess          = modntdll.NewProc("NtResumeProcess")
)

const (
	JobObjectExtendedLimitInformation  = 9
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x2000
)

type jobobject_basic_limit_information struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type io_counters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobobject_extended_limit_information struct {
	BasicLimitInformation jobobject_basic_limit_information
	IoInfo                io_counters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

func createJobObject() (syscall.Handle, error) {
	r1, _, e := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return 0, e
	}
	return syscall.Handle(r1), nil
}

func setJobKillOnClose(job syscall.Handle) error {
	var info jobobject_extended_limit_information
	info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	r1, _, e := procSetInformationJobObject.Call(
		uintptr(job),
		uintptr(JobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r1 == 0 {
		return e
	}
	return nil
}

func assignProcessHandleToJob(job, process syscall.Handle) error {
	r1, _, e := procAssignProcessToJobObject.Call(uintptr(job), uintptr(process))
	if r1 == 0 {
		return e
	}
	return nil
}

func openProcessHandle(pid uint32) (syscall.Handle, error) {
	const (
		processSetQuota         = 0x0100
		processTerminate        = 0x0001
		processSuspendResume    = 0x0800
		processQueryInformation = 0x0400
	)
	access := uintptr(processSetQuota | processTerminate | processSuspendResume | processQueryInformation)
	handle, _, callErr := procOpenProcess.Call(access, 0, uintptr(pid))
	if handle == 0 {
		return 0, callErr
	}
	return syscall.Handle(handle), nil
}

func resumeProcess(process syscall.Handle) error {
	status, _, callErr := procNtResumeProcess.Call(uintptr(process))
	if status != 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("NtResumeProcess status 0x%x", status)
	}
	return nil
}
