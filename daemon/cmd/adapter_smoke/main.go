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
			fmt.Fprintln(os.Stderr, "usage: adapter_smoke send <claude_code|codex|kilo|kimi_cli|grok_build> <sessionID> <prompt>")
			os.Exit(2)
		}
		runSend(os.Args[2], os.Args[3], strings.Join(os.Args[4:], " "))
		return
	}
	runDiscover()
}

func runDiscover() {
	registry, err := adapters.NewDefaultRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize adapters: %v\n", err)
		os.Exit(1)
	}
	defer registry.Close()
	out := map[string]any{}
	for _, adapter := range registry.All() {
		entry := map[string]any{"available": adapter.IsAvailable()}
		sessions, err := adapter.Discover()
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
					"id":          s.ID,
					"status":      s.Status,
					"summary":     s.Summary,
					"path":        s.SessionPath,
					"project_dir": s.ProjectDir,
					"agent":       s.AgentType,
					"age_s":       int(time.Since(s.LastActivity).Seconds()),
				})
			}
			entry["samples"] = samples
		}
		out[adapter.Name()] = entry
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func runSend(agent, sessionID, prompt string) {
	registry, err := adapters.NewDefaultRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize adapters: %v\n", err)
		os.Exit(1)
	}
	defer registry.Close()
	adapter, ok := registry.Get(agent)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown agent %s\n", agent)
		os.Exit(2)
	}
	registry.SetOutputSink(logEvent)
	// Warm adapter path caches and reject sessions absent from the local store.
	_, _ = adapter.Discover()
	fmt.Fprintf(os.Stderr, "send %s %s available=%v\n", agent, sessionID, adapter.IsAvailable())
	err = adapter.SendPrompt(sessionID, adapters.PromptRequest{Prompt: prompt})
	if err != nil {
		fmt.Fprintf(os.Stderr, "SendPrompt error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "SendPrompt ok; waiting 45s for output...")
	time.Sleep(45 * time.Second)
	fmt.Fprintln(os.Stderr, "done")
}

var outMu sync.Mutex

func logEvent(event adapters.OutputEvent) {
	outMu.Lock()
	defer outMu.Unlock()
	c := event.Content
	if len(c) > 200 {
		c = c[:200] + "..."
	}
	fmt.Fprintf(
		os.Stderr,
		"[out] sid=%s agent=%s type=%s id=%s content=%q\n",
		event.SessionID,
		event.AgentType,
		event.Type,
		event.MessageID,
		c,
	)
}
