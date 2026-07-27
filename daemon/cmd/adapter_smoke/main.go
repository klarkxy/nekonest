package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "send" {
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: adapter_smoke send <claude_code|codex|kilo> <sessionID> <prompt>")
			os.Exit(2)
		}
		runSend(os.Args[2], os.Args[3], strings.Join(os.Args[4:], " "))
		return
	}
	runDiscover()
}

func runDiscover() {
	type named struct {
		name string
		a    adapters.Adapter
	}
	list := []named{
		{"claude_code", adapters.NewClaudeCodeAdapter()},
		{"codex", adapters.NewCodexAdapter()},
		{"kilo", adapters.NewKiloAdapter()},
	}
	out := map[string]any{}
	for _, n := range list {
		entry := map[string]any{"available": n.a.IsAvailable()}
		if k, ok := n.a.(*adapters.KiloAdapter); ok {
			entry["cli"] = k.GetCommander().CLIPath()
		}
		sessions, err := n.a.Discover()
		if err != nil {
			entry["error"] = err.Error()
		} else {
			entry["count"] = len(sessions)
			max := 5
			if len(sessions) < max {
				max = len(sessions)
			}
			samples := make([]map[string]any, 0, max)
			for i := 0; i < max; i++ {
				s := sessions[i]
				samples = append(samples, map[string]any{
					"id":      s.ID,
					"status":  s.Status,
					"summary": s.Summary,
					"path":    s.SessionPath,
					"agent":   s.AgentType,
					"age_s":   int(time.Since(s.LastActivity).Seconds()),
				})
			}
			entry["samples"] = samples
		}
		out[n.name] = entry
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func runSend(agent, sessionID, prompt string) {
	var a adapters.Adapter
	switch agent {
	case "claude_code":
		ad := adapters.NewClaudeCodeAdapter()
		ad.GetCommander().OnAgentOutput = logOut3
		a = ad
	case "codex":
		ad := adapters.NewCodexAdapter()
		ad.GetCommander().OnAgentOutput = logOut3
		a = ad
	case "kilo":
		ad := adapters.NewKiloAdapter()
		ad.GetCommander().OnAgentOutput = logOut4
		// warm dir cache via Discover
		_, _ = ad.Discover()
		a = ad
	default:
		fmt.Fprintf(os.Stderr, "unknown agent %s\n", agent)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "send %s %s available=%v\n", agent, sessionID, a.IsAvailable())
	err := a.SendPrompt(sessionID, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SendPrompt error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "SendPrompt ok; waiting 45s for output...")
	time.Sleep(45 * time.Second)
	fmt.Fprintln(os.Stderr, "done")
}

var outMu sync.Mutex

func logOut3(sessionID, msgType, content string) {
	logOut4(sessionID, msgType, content, "")
}

func logOut4(sessionID, msgType, content, msgID string) {
	outMu.Lock()
	defer outMu.Unlock()
	c := content
	if len(c) > 200 {
		c = c[:200] + "..."
	}
	fmt.Fprintf(os.Stderr, "[out] sid=%s type=%s id=%s content=%q\n", sessionID, msgType, msgID, c)
}
