package agentexec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func waitPromptAck(
	ctx context.Context,
	ack <-chan struct{},
	exited <-chan struct{},
	nativeID *string,
	idMu *sync.Mutex,
	agent string,
	diag func() string,
) (string, bool, bool, error) {
	id := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		return *nativeID
	}
	select {
	case <-ack:
		return id(), true, true, nil
	case <-exited:
		native := id()
		if native == "" {
			return "", false, false, fmt.Errorf("%s initial prompt was not confirmed", agent)
		}
		return native, true, false, fmt.Errorf("%s initial prompt was not confirmed", agent)
	case <-ctx.Done():
		return id(), true, false, fmt.Errorf("%s initial prompt was not confirmed: %w", agent, ctx.Err())
	}
}

func probeCLIHelp(ctx context.Context, command string, required ...string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("CLI path is empty")
	}
	if strings.ContainsAny(command, `/\`) {
		if _, err := os.Stat(command); err != nil {
			return fmt.Errorf("CLI not found: %w", err)
		}
	} else if _, err := exec.LookPath(command); err != nil {
		return fmt.Errorf("CLI not found: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	executable, args, err := resolveLaunch(command, []string{"--help"})
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(probeCtx, executable, args...)
	cmd.WaitDelay = time.Second
	configureBackgroundProcess(cmd)
	output, runErr := cmd.CombinedOutput()
	text := strings.ToLower(string(output))
	if runErr != nil && strings.TrimSpace(text) == "" {
		return fmt.Errorf("CLI help failed: %w", runErr)
	}
	for _, want := range required {
		if !strings.Contains(text, strings.ToLower(want)) {
			return fmt.Errorf("installed CLI does not advertise %s", want)
		}
	}
	return nil
}
