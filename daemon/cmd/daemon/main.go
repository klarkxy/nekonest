package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/config"
	"github.com/nekonest/daemon/internal/connection"
)

func main() {
	configPath := flag.String("config", "", "config file path (default: ~/.nekonest/config.json)")
	pairFlag := flag.String("pair", "", "generate phone pair code for already-registered device (e.g. -pair gen)")
	register := flag.Bool("register", false, "register this device with the server (needs NEKONEST_SERVER)")
	deviceName := flag.String("name", "", "device name (for registration)")
	flag.Parse()

	// Handle registration flow
	if *register {
		handleRegistration(*deviceName)
		return
	}

	// Handle pair-code generation
	if *pairFlag != "" {
		handlePairing(*pairFlag)
		return
	}

	// Load config
	var cfg *config.Config
	var err error

	if *configPath != "" {
		cfg, err = config.LoadFrom(*configPath)
	} else {
		cfg, err = config.Load()
	}

	if err != nil {
		fmt.Println("No config found. Register first:")
		fmt.Println(`  set NEKONEST_SERVER=https://your-vps`)
		fmt.Println(`  nekonest-daemon -register -name "My PC"`)
		os.Exit(1)
	}

	log.Printf("🐱 NekoNest Daemon starting")
	log.Printf("   Device: %s", cfg.DeviceID)
	log.Printf("   Server: %s", cfg.ServerURL)

	// Runtime config is published as immutable snapshots. Device credentials are
	// intentionally fixed for this daemon process; changing them requires a
	// restart so connection authentication and message identity cannot diverge.
	initialCfg := *cfg
	var runtimeCfg atomic.Pointer[config.Config]
	runtimeCfg.Store(&initialCfg)
	currentConfig := func() *config.Config {
		return runtimeCfg.Load()
	}
	deviceID := initialCfg.DeviceID
	token := initialCfg.Token
	activeConfigPath := *configPath
	if activeConfigPath == "" {
		activeConfigPath = config.DefaultConfigPath()
	}
	activeConfigPath, err = filepath.Abs(activeConfigPath)
	if err != nil {
		log.Fatalf("[daemon] resolve config path: %v", err)
	}
	activeConfigPath, err = filepath.EvalSymlinks(activeConfigPath)
	if err != nil {
		log.Fatalf("[daemon] canonicalize config path for instance lock: %v", err)
	}
	journalPath := promptJournalPath(activeConfigPath, deviceID)
	instanceLock, lockErr := acquireDaemonInstanceLock(activeConfigPath + ".daemon.lock")
	if lockErr != nil {
		log.Fatalf("[daemon] refusing to start a second instance: %v", lockErr)
	}
	defer func() {
		if err := instanceLock.Close(); err != nil {
			log.Printf("[daemon] release instance lock: %v", err)
		}
	}()
	commandJournal, journalErr := loadPromptJournal(
		journalPath,
		deviceID,
		maxAcceptedPromptIDs,
	)
	if journalErr != nil {
		// Failing open could replay a prompt that already reached an agent.
		log.Fatalf("[daemon] cannot safely load prompt journal: %v", journalErr)
	}
	log.Printf("   Prompt journal: %s", commandJournal.path)

	// Initialize the built-in adapter registry.
	adapterRegistry, err := adapters.NewDefaultRegistry()
	if err != nil {
		log.Fatalf("[daemon] initialize adapters: %v", err)
	}
	adapterList := adapterRegistry.All()

	// Log available agents
	for _, a := range adapterList {
		log.Printf("   Adapter: %s (available: %v)", a.Name(), a.IsAvailable())
	}

	// Create root context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create server connection
	client := connection.NewClient(ctx, initialCfg.ServerURL, deviceID, token)

	// Wire every adapter through one normalized output stream.
	adapterRegistry.SetOutputSink(func(event adapters.OutputEvent) {
		sendSessionMessage(
			client,
			deviceID,
			event.SessionID,
			string(event.AgentType),
			event.Type,
			event.Content,
			event.MessageID,
		)
	})

	// Session discovery state
	var (
		sessionMu    sync.Mutex
		lastSessions = make(map[string]*adapters.SessionInfo)
	)
	sessionSends := newSessionLockMap()
	acceptedPrompts := newPromptAcceptanceCache(maxAcceptedPromptIDs)

	// Handle incoming messages from server
	client.OnMessage(func(data []byte) {
		// Panic recovery for message handling
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[daemon] PANIC in message handler: %v", r)
			}
		}()

		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[daemon] unmarshal error: %v", err)
			return
		}

		msgType, _ := msg["type"].(string)
		sessionID, _ := msg["session_id"].(string)
		payload, _ := msg["payload"].(map[string]interface{})

		log.Printf("[daemon] received: type=%s session=%s", msgType, sessionID)

		// Find the right adapter for this session
		var targetAdapter adapters.Adapter
		sessionMu.Lock()
		if info, ok := lastSessions[sessionID]; ok {
			if adapter, exists := adapterRegistry.Get(string(info.AgentType)); exists {
				targetAdapter = adapter
			}
		}
		sessionMu.Unlock()

		switch msgType {
		case "send_prompt":
			// Serialize sends per session (prevents double codex/kilo process).
			unlock := sessionSends.lock(sessionID)
			defer unlock()

			clientMsgID, _ := payload["client_msg_id"].(string)
			if clientMsgID == "" {
				clientMsgID, _ = payload["message_id"].(string)
			}
			if clientMsgID == "" {
				sendPromptFailed(client, deviceID, sessionID, "", "client_msg_id required for safe dispatch")
				return
			}
			if recorded, seen := commandJournal.state(sessionID, clientMsgID); seen {
				switch recorded.Status {
				case promptJournalAccepted, promptJournalCommitted:
					accepted := acceptedPrompt{prompt: recorded.PromptEcho}
					if cached, ok := acceptedPrompts.get(sessionID, clientMsgID); ok {
						accepted = cached
					}
					log.Printf("[daemon] re-acknowledging durable accepted prompt session=%s client_msg_id=%s", sessionID, clientMsgID)
					sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				default:
					log.Printf("[daemon] refusing replay of unresolved prompt session=%s client_msg_id=%s state=%s", sessionID, clientMsgID, recorded.Status)
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate)
				}
				return
			}
			if accepted, duplicate := acceptedPrompts.get(sessionID, clientMsgID); duplicate {
				log.Printf("[daemon] re-acknowledging accepted prompt session=%s client_msg_id=%s", sessionID, clientMsgID)
				sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				return
			}
			prompt, _ := payload["prompt"].(string)
			originalPrompt := prompt
			// Drop stale attachment blocks if client re-sent an already-augmented draft.
			prompt = stripNekoAttachSuffix(prompt)
			refs := parseAttachmentRefs(payload["attachments"])
			if prompt == "" && len(refs) == 0 {
				log.Printf("[daemon] empty prompt")
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "empty prompt")
				return
			}

			if len(refs) > 0 {
				attDir, _, suffix, aerr := attach.Materialize(currentConfig().ServerURL, sessionID, refs)
				if aerr != nil {
					log.Printf("[daemon] attach materialize: %v", aerr)
					sendPromptFailed(client, deviceID, sessionID, clientMsgID, "attachment download failed: "+aerr.Error())
					return
				}
				// Agent may read files during the run; remove after a few hours.
				if attDir != "" {
					go func(dir string) {
						time.Sleep(2 * time.Hour)
						_ = os.RemoveAll(dir)
					}(attDir)
				}
				if prompt == "" {
					prompt = "(user sent attachments)"
				}
				prompt = prompt + suffix
				log.Printf("[daemon] attached %d file(s) into prompt for %s", len(refs), sessionID)
			}

			if targetAdapter == nil {
				log.Printf("[daemon] no cached adapter for session %s, probing local agent stores", sessionID)
				// Prefer single best guess — never blast all agents (that multi-sends).
				targetAdapter = pickAdapterForSession(sessionID, adapterList)
			}
			if targetAdapter == nil {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "session not found on this PC")
				return
			}
			// Persist before crossing the Agent boundary. If the daemon dies
			// after this write, startup converts dispatching to indeterminate
			// and will never automatically execute the command again.
			if err := commandJournal.markDispatching(sessionID, clientMsgID, originalPrompt); err != nil {
				log.Printf("[daemon] prompt journal refused dispatch session=%s client_msg_id=%s: %v", sessionID, clientMsgID, err)
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "prompt was not executed because its durable dispatch record could not be written: "+err.Error())
				return
			}
			if err := targetAdapter.SendPrompt(sessionID, prompt); err != nil {
				log.Printf("[daemon] send_prompt error via %s: %v", targetAdapter.Name(), err)
				// The adapter API cannot prove whether a failing Start/Send
				// crossed into the agent. Preserve the ambiguity instead of
				// enabling an unsafe automatic retry.
				if journalErr := commandJournal.markIndeterminate(sessionID, clientMsgID); journalErr != nil {
					log.Printf("[daemon] mark prompt indeterminate failed: %v", journalErr)
				}
				sendPromptIndeterminate(
					client,
					deviceID,
					sessionID,
					clientMsgID,
					promptOutcomeIndeterminate+"; agent returned: "+err.Error(),
				)
			} else {
				if err := commandJournal.markAccepted(sessionID, clientMsgID); err != nil {
					log.Printf("[daemon] persist prompt acceptance failed session=%s client_msg_id=%s: %v", sessionID, clientMsgID, err)
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate+"; acceptance journal update failed")
					return
				}
				accepted := acceptedPrompt{prompt: boundedPromptEcho(originalPrompt)}
				acceptedPrompts.add(sessionID, clientMsgID, accepted)
				sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				log.Printf("[daemon] prompt sent via %s", targetAdapter.Name())
			}

		case "prompt_status_query":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			clientMsgID, _ := payload["client_msg_id"].(string)
			if clientMsgID == "" {
				sendPromptFailed(client, deviceID, sessionID, "", "client_msg_id required")
				return
			}
			if recorded, seen := commandJournal.state(sessionID, clientMsgID); seen {
				if recorded.Status == promptJournalAccepted || recorded.Status == promptJournalCommitted {
					accepted := acceptedPrompt{prompt: recorded.PromptEcho}
					if cached, ok := acceptedPrompts.get(sessionID, clientMsgID); ok {
						accepted = cached
					}
					log.Printf("[daemon] durable prompt status accepted session=%s client_msg_id=%s", sessionID, clientMsgID)
					sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				} else {
					log.Printf("[daemon] durable prompt status unresolved session=%s client_msg_id=%s state=%s", sessionID, clientMsgID, recorded.Status)
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate)
				}
				return
			}
			log.Printf("[daemon] prompt status absent from durable journal session=%s client_msg_id=%s", sessionID, clientMsgID)
			sendPromptNotSeen(client, deviceID, sessionID, clientMsgID)

		case "prompt_committed":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			clientMsgID, _ := payload["client_msg_id"].(string)
			if clientMsgID == "" {
				log.Printf("[daemon] prompt_committed without client_msg_id session=%s", sessionID)
				return
			}
			recorded, seen := commandJournal.state(sessionID, clientMsgID)
			if !seen {
				// A duplicate commit may arrive after an old committed record
				// was evicted; there is nothing left to transition.
				log.Printf("[daemon] prompt_committed for absent/evicted record session=%s client_msg_id=%s", sessionID, clientMsgID)
				return
			}
			if recorded.Status == promptJournalCommitted {
				return
			}
			if recorded.Status != promptJournalAccepted {
				log.Printf("[daemon] refusing prompt_committed for state=%s session=%s client_msg_id=%s", recorded.Status, sessionID, clientMsgID)
				return
			}
			if err := commandJournal.markCommitted(sessionID, clientMsgID); err != nil {
				log.Printf("[daemon] persist prompt_committed failed session=%s client_msg_id=%s: %v", sessionID, clientMsgID, err)
				return
			}
			log.Printf("[daemon] prompt committed by server session=%s client_msg_id=%s", sessionID, clientMsgID)

		case "approve":
			approvalID, _ := payload["approval_id"].(string)
			if targetAdapter == nil {
				sendDaemonError(client, deviceID, sessionID, "no adapter for session (is agent process still running?)")
				return
			}
			if err := targetAdapter.Approve(sessionID, approvalID); err != nil {
				log.Printf("[daemon] approve error: %v", err)
				sendDaemonError(client, deviceID, sessionID, "approve failed: "+err.Error())
			} else {
				log.Printf("[daemon] approved: %s", approvalID)
			}

		case "deny":
			approvalID, _ := payload["approval_id"].(string)
			if targetAdapter == nil {
				sendDaemonError(client, deviceID, sessionID, "no adapter for session")
				return
			}
			if err := targetAdapter.Deny(sessionID, approvalID); err != nil {
				log.Printf("[daemon] deny error: %v", err)
				sendDaemonError(client, deviceID, sessionID, "deny failed: "+err.Error())
			} else {
				log.Printf("[daemon] denied: %s", approvalID)
			}

		case "interrupt":
			if targetAdapter == nil {
				sendDaemonError(client, deviceID, sessionID, "no adapter for session")
				return
			}
			if err := targetAdapter.Interrupt(sessionID); err != nil {
				log.Printf("[daemon] interrupt error: %v", err)
				sendDaemonError(client, deviceID, sessionID, "interrupt failed: "+err.Error())
			} else {
				log.Printf("[daemon] interrupted session %s", sessionID)
			}

		case "fetch_history":
			limit := 40
			if payload != nil {
				if n, ok := payload["limit"].(float64); ok && int(n) > 0 {
					limit = int(n)
				}
			}
			if limit > 40 {
				limit = 40
			}
			hist, source, err := fetchHistoryForSession(
				sessionID,
				limit,
				targetAdapter,
				adapterList,
			)
			if hist == nil {
				msg := "no history found for session"
				if err != nil {
					msg = err.Error()
				}
				log.Printf("[daemon] fetch_history: %s", msg)
				// Still reply empty so phone stops waiting
				hist = []*adapters.HistoryMessage{}
				if source == "" {
					source = "unknown"
				}
			}
			msgs := make([]map[string]interface{}, 0, len(hist))
			for _, m := range hist {
				if m == nil {
					continue
				}
				msgs = append(msgs, map[string]interface{}{
					"id":        m.ID,
					"role":      m.Role,
					"content":   m.Content,
					"type":      m.Type,
					"timestamp": m.Timestamp,
					"metadata": map[string]interface{}{
						"imported":   true,
						"agent_type": source,
					},
				})
			}
			out := map[string]interface{}{
				"type":       "session_history",
				"device_id":  deviceID,
				"session_id": sessionID,
				"timestamp":  time.Now().Unix(),
				"payload": map[string]interface{}{
					"source":    source,
					"truncated": len(msgs) >= limit,
					"limit":     limit,
					"messages":  msgs,
				},
			}
			if e := client.Send(out); e != nil {
				log.Printf("[daemon] send session_history: %v", e)
			} else {
				log.Printf("[daemon] session_history %s source=%s n=%d", sessionID, source, len(msgs))
			}

		case "heartbeat":
			// Server heartbeat, keep-alive handled by connection layer

		default:
			log.Printf("[daemon] unknown message type: %s", msgType)
		}
	})

	// Session force-report after reconnect (server cache starts empty).
	forceReport := make(chan struct{}, 1)
	requestForceReport := func() {
		select {
		case forceReport <- struct{}{}:
		default:
		}
	}
	client.OnConnect(func() {
		requestForceReport()
		pending := commandJournal.uncommittedAccepted()
		if len(pending) > 0 {
			log.Printf("[daemon] re-acknowledging %d accepted prompt(s) awaiting server commit", len(pending))
		}
		for _, record := range pending {
			accepted := acceptedPrompt{prompt: record.PromptEcho}
			if cached, ok := acceptedPrompts.get(record.SessionID, record.ClientMsgID); ok {
				accepted = cached
			}
			if err := sendPromptAccepted(
				client,
				deviceID,
				record.SessionID,
				record.ClientMsgID,
				accepted,
			); err != nil {
				break
			}
		}
	})

	// Connect to server
	if err := client.Connect(); err != nil {
		log.Fatalf("failed to connect: %v", err)
	}

	// Start heartbeat in context
	go client.StartHeartbeat(ctx)

	// Session discovery loop with context
	go func() {
		// Initial scan after 2 seconds
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		discoverAndReport := func(force bool) {
			var allSessions []*adapters.SessionInfo
			for _, adapter := range adapterList {
				sessions, err := adapter.Discover()
				if err != nil {
					log.Printf("[daemon] %s discover error: %v", adapter.Name(), err)
					continue
				}
				allSessions = append(allSessions, sessions...)
			}

			// Check for changes
			changed := force
			sessionMu.Lock()

			// Build new session map
			newSessions := make(map[string]*adapters.SessionInfo)
			for _, s := range allSessions {
				newSessions[s.ID] = s
			}

			// Compare with last state
			if !changed {
				if len(newSessions) != len(lastSessions) {
					changed = true
				} else {
					for id, newS := range newSessions {
						oldS, ok := lastSessions[id]
						if !ok || oldS.Status != newS.Status || oldS.Summary != newS.Summary || oldS.ProjectDir != newS.ProjectDir {
							changed = true
							break
						}
					}
				}
			}

			lastSessions = newSessions
			sessionMu.Unlock()

			// Always report on force (reconnect) or when local list changed
			if changed {
				report := map[string]interface{}{
					"type":      "session_list",
					"device_id": deviceID,
					"timestamp": time.Now().Unix(),
					"payload": map[string]interface{}{
						// Convert at the wire boundary: unix last_activity + device_id
						"sessions": sessionsToWire(deviceID, allSessions),
					},
				}
				if err := client.Send(report); err != nil {
					log.Printf("[daemon] report error: %v", err)
					// Retry after short backoff (reconnect race / transient drop)
					if force {
						go func() {
							select {
							case <-time.After(2 * time.Second):
								requestForceReport()
							case <-ctx.Done():
							}
						}()
					}
				} else {
					log.Printf("[daemon] reported %d sessions (force=%v)", len(allSessions), force)
				}
			}
		}

		// First discovery
		discoverAndReport(true)

		// Periodic discovery + reconnect-driven force report
		for {
			select {
			case <-ticker.C:
				discoverAndReport(false)
			case <-forceReport:
				discoverAndReport(true)
			case <-ctx.Done():
				log.Printf("[daemon] discovery loop stopped")
				return
			}
		}
	}()

	// Start config hot-reload watcher. Snapshots are immutable after Store.
	watchPath := activeConfigPath
	go config.WatchPath(ctx, watchPath, func(newCfg *config.Config) {
		oldCfg := currentConfig()
		nextCfg := *newCfg
		credChanged := nextCfg.DeviceID != deviceID || nextCfg.Token != token
		if credChanged {
			log.Printf("[daemon] config credentials changed but were NOT applied; restart daemon to use the new device_id/token")
			nextCfg.DeviceID = deviceID
			nextCfg.Token = token
		}
		urlChanged := nextCfg.ServerURL != oldCfg.ServerURL
		if urlChanged {
			log.Printf("[daemon] server URL changed, reconnecting...")
		}
		// Endpoint change and snapshot publication share the same dispatch
		// linearization point. Existing callbacks finish against oldCfg; no
		// callback from the new endpoint can start before nextCfg is visible.
		client.SetServerURLAndPublish(nextCfg.ServerURL, func() {
			runtimeCfg.Store(&nextCfg)
		})
		log.Printf("[daemon] config snapshot applied, server=%s work_dir=%s", nextCfg.ServerURL, nextCfg.WorkDir)
	})

	// Start read loop (blocking, handles reconnection)
	go client.StartReadLoop(ctx)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("[daemon] received signal: %v", sig)
	case <-ctx.Done():
		log.Printf("[daemon] context cancelled")
	}

	// Graceful shutdown
	log.Println("[daemon] shutting down...")

	// 1. Cancel context to stop all goroutines
	cancel()

	// 2. Close WebSocket connection
	client.Close()

	// 3. Cleanup all adapter resources (watchers, running processes)
	if err := adapterRegistry.Close(); err != nil {
		log.Printf("[daemon] error closing adapters: %v", err)
	}

	// 4. Wait a bit for goroutines to finish
	time.Sleep(500 * time.Millisecond)
	log.Println("[daemon] goodbye 🐱")
}

// projectLabel shortens a full path to the leaf folder name for UI.
func projectLabel(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	// Normalize slashes
	dir = strings.ReplaceAll(dir, "\\", "/")
	dir = strings.TrimRight(dir, "/")
	if i := strings.LastIndex(dir, "/"); i >= 0 && i+1 < len(dir) {
		return dir[i+1:]
	}
	return dir
}

// sessionsToWire converts internal discovery models into the public protocol shape
// expected by the server/PWA (unix last_activity, always-set device_id).
func sessionsToWire(deviceID string, sessions []*adapters.SessionInfo) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		item := map[string]interface{}{
			"id":            s.ID,
			"device_id":     deviceID,
			"agent_type":    string(s.AgentType),
			"status":        string(s.Status),
			"summary":       s.Summary,
			"last_activity": s.LastActivity.Unix(),
		}
		if s.ProjectDir != "" {
			item["project_dir"] = s.ProjectDir
			item["project"] = projectLabel(s.ProjectDir)
		}
		if s.PendingApproval != nil {
			item["pending_approval"] = map[string]interface{}{
				"id":          s.PendingApproval.ID,
				"tool_name":   s.PendingApproval.ToolName,
				"description": s.PendingApproval.Description,
			}
		}
		out = append(out, item)
	}
	return out
}

// sendSessionMessage creates a session_message and sends it to the server.
// msgID, when non-empty, is a stable id so the phone can patch streaming content.
func sendSessionMessage(client *connection.Client, deviceID, sessionID, agentType, msgType, content, msgID string) {
	// Determine role from msgType
	role := "assistant"
	switch msgType {
	case "system":
		role = "system"
	case "user":
		role = "user"
	case "tool_call":
		role = "assistant"
	case "tool_result":
		role = "tool"
	case "error":
		role = "system"
	}
	if msgID == "" {
		msgID = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}

	msg := map[string]interface{}{
		"type":       "session_message",
		"device_id":  deviceID,
		"session_id": sessionID,
		"timestamp":  time.Now().Unix(),
		"payload": map[string]interface{}{
			"message": map[string]interface{}{
				"id":        msgID,
				"role":      role,
				"content":   content,
				"type":      msgType,
				"timestamp": time.Now().Unix(),
				"metadata": map[string]interface{}{
					"agent_type": agentType,
					"stream":     true,
				},
			},
		},
	}

	if err := client.Send(msg); err != nil {
		log.Printf("[daemon] send session_message error: %v", err)
	}
}

// handleRegistration registers this device with the server via REST and saves config.
func handleRegistration(deviceName string) {
	fmt.Println("🐱 NekoNest Device Registration")
	fmt.Println("================================")

	if deviceName == "" {
		hostname, _ := os.Hostname()
		deviceName = hostname
	}

	// Already registered?
	if existing, err := config.Load(); err == nil && existing.Token != "" && existing.DeviceID != "" {
		fmt.Printf("Already registered.\n")
		fmt.Printf("  Device: %s\n", existing.DeviceID)
		fmt.Printf("  Server: %s\n", existing.ServerURL)
		fmt.Printf("Config:  %s\n", config.DefaultConfigPath())
		fmt.Println("To re-register, delete the config file first.")
		// Still mint a fresh pair code for the phone
		if code, exp, err := requestPairCode(existing); err == nil {
			fmt.Printf("\n📱 Phone pair code: %s (expires ~%s)\n", code, exp.Local().Format("15:04:05"))
			fmt.Println("Enter this code in the PWA 「配对电脑」 page.")
		}
		return
	}

	serverURL := os.Getenv("NEKONEST_SERVER")
	if serverURL == "" {
		fmt.Println("Set NEKONEST_SERVER first, e.g.:")
		fmt.Println(`  set NEKONEST_SERVER=https://nekonest.example.com`)
		fmt.Println(`  $env:NEKONEST_SERVER="https://nekonest.example.com"  # PowerShell`)
		os.Exit(1)
	}

	httpBase := config.HTTPBaseURL(serverURL)
	wsURL := config.NormalizeServerURL(serverURL)

	body, _ := json.Marshal(map[string]string{"name": deviceName})
	req, err := http.NewRequest(http.MethodPost, httpBase+"/api/devices/register", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("register request build failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bootstrap := os.Getenv("NEKONEST_BOOTSTRAP_TOKEN"); bootstrap != "" {
		req.Header.Set("X-Neko-Bootstrap", bootstrap)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("register request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("register failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		DeviceID string `json:"device_id"`
		Token    string `json:"token"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Fatalf("parse register response: %v", err)
	}
	if result.DeviceID == "" || result.Token == "" {
		log.Fatalf("register response missing device_id/token: %s", string(respBody))
	}

	cfg := &config.Config{
		ServerURL: wsURL,
		DeviceID:  result.DeviceID,
		Token:     result.Token,
	}
	if err := cfg.Save(); err != nil {
		log.Fatalf("Failed to save config: %v", err)
	}

	fmt.Printf("✅ Registered as %s (%s)\n", result.Name, result.DeviceID)
	fmt.Printf("   Server: %s\n", wsURL)
	fmt.Printf("   Config: %s\n", config.DefaultConfigPath())

	if code, exp, err := requestPairCode(cfg); err != nil {
		fmt.Printf("\n⚠️  Could not generate pair code: %v\n", err)
		fmt.Println("You can still list the device on phone if you use the phone secret.")
	} else {
		fmt.Printf("\n📱 Phone pair code: %s (expires ~%s)\n", code, exp.Local().Format("15:04:05"))
		fmt.Println("1. Open PWA → 配对电脑 → enter this code")
		fmt.Println("2. Start daemon: nekonest-daemon.exe")
	}
}

// handlePairing generates a short-lived pair code for an already-registered device.
// Usage: nekonest-daemon -pair gen   (or any non-empty value; code is minted server-side)
func handlePairing(code string) {
	fmt.Println("🐱 NekoNest Pair Code")
	fmt.Println("=====================")

	cfg, err := config.Load()
	if err != nil {
		fmt.Println("Device not registered yet. Run with -register first.")
		os.Exit(1)
	}

	// Historical flag took a code string; we now always mint a server code for the phone.
	_ = code
	pairCode, exp, err := requestPairCode(cfg)
	if err != nil {
		log.Fatalf("generate pair code: %v", err)
	}
	fmt.Printf("Device: %s\n", cfg.DeviceID)
	fmt.Printf("📱 Phone pair code: %s\n", pairCode)
	fmt.Printf("Expires: %s\n", exp.Local().Format(time.RFC3339))
	fmt.Println("Enter this code in the PWA 「配对电脑」 page.")
}

// requestPairCode calls POST /api/pair/generate for the registered device.
func requestPairCode(cfg *config.Config) (string, time.Time, error) {
	httpBase := config.HTTPBaseURL(cfg.ServerURL)
	body, _ := json.Marshal(map[string]string{
		"device_id": cfg.DeviceID,
		"token":     cfg.Token,
	})
	resp, err := http.Post(httpBase+"/api/pair/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Code      string `json:"code"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", time.Time{}, err
	}
	return result.Code, time.Unix(result.ExpiresAt, 0), nil
}

func stripNekoAttachSuffix(prompt string) string {
	// Only strip our exact injected block, not arbitrary user text containing the phrase.
	const mark = "\n\n[NekoNest attachments — local files on this PC]\n"
	if i := strings.Index(prompt, mark); i >= 0 {
		return strings.TrimSpace(prompt[:i])
	}
	return prompt
}

// pickAdapterForSession chooses one adapter when Discover cache missed.
func pickAdapterForSession(sessionID string, list []adapters.Adapter) adapters.Adapter {
	// IDs can overlap across stores. Return an adapter only when exactly one
	// authoritative local store positively claims the session. In particular,
	// do not use FetchHistory as an existence probe: empty history is valid.
	var match adapters.Adapter
	for _, adapter := range list {
		if !adapter.OwnsSession(sessionID) {
			continue
		}
		if match != nil {
			return nil
		}
		match = adapter
	}
	return match
}

// fetchHistoryForSession keeps a successful empty history associated with its
// owning adapter. A nil slice with a nil error is a valid empty transcript,
// not a signal to probe unrelated agent stores.
func fetchHistoryForSession(
	sessionID string,
	limit int,
	preferred adapters.Adapter,
	list []adapters.Adapter,
) ([]*adapters.HistoryMessage, string, error) {
	var firstErr error
	read := func(adapter adapters.Adapter) ([]*adapters.HistoryMessage, string, bool) {
		if adapter == nil {
			return nil, "", false
		}
		history, err := adapter.FetchHistory(sessionID, limit)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil, "", false
		}
		if history == nil {
			history = []*adapters.HistoryMessage{}
		}
		return history, adapter.Name(), true
	}

	if history, source, ok := read(preferred); ok {
		return history, source, nil
	}
	owner := pickAdapterForSession(sessionID, list)
	if owner == nil ||
		(preferred != nil && owner.Name() == preferred.Name()) {
		return nil, "", firstErr
	}
	if history, source, ok := read(owner); ok {
		return history, source, nil
	}
	return nil, "", firstErr
}

func parseAttachmentRefs(raw interface{}) []attach.Ref {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var out []attach.Ref
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		u, _ := m["url"].(string)
		if u == "" {
			continue
		}
		name, _ := m["name"].(string)
		mime, _ := m["mime"].(string)
		id, _ := m["id"].(string)
		out = append(out, attach.Ref{URL: u, Name: name, MIME: mime, ID: id})
	}
	return out
}

func sendPromptAccepted(
	client *connection.Client,
	deviceID, sessionID, clientMsgID string,
	accepted acceptedPrompt,
) error {
	payload := map[string]interface{}{
		"client_msg_id": clientMsgID,
		"prompt":        accepted.prompt,
	}
	msg := map[string]interface{}{
		"type":       "prompt_accepted",
		"device_id":  deviceID,
		"session_id": sessionID,
		"timestamp":  time.Now().Unix(),
		"payload":    payload,
	}
	if err := client.Send(msg); err != nil {
		// The accepted ID remains cached. A server retransmission will only
		// re-send this acknowledgement and will not execute the prompt again.
		log.Printf("[daemon] send prompt_accepted failed client_msg_id=%s: %v", clientMsgID, err)
		return err
	}
	return nil
}

func sendPromptFailed(client *connection.Client, deviceID, sessionID, clientMsgID, message string) {
	sendPromptFailure(client, deviceID, sessionID, clientMsgID, message, "rejected", true)
}

func sendPromptIndeterminate(client *connection.Client, deviceID, sessionID, clientMsgID, message string) {
	sendPromptFailure(client, deviceID, sessionID, clientMsgID, message, "indeterminate", false)
}

func sendPromptNotSeen(client *connection.Client, deviceID, sessionID, clientMsgID string) {
	msg := map[string]interface{}{
		"type":       "prompt_not_seen",
		"device_id":  deviceID,
		"session_id": sessionID,
		"timestamp":  time.Now().Unix(),
		"payload": map[string]interface{}{
			"client_msg_id": clientMsgID,
		},
	}
	if err := client.Send(msg); err != nil {
		log.Printf("[daemon] send prompt_not_seen failed client_msg_id=%s: %v", clientMsgID, err)
	}
}

func sendPromptFailure(
	client *connection.Client,
	deviceID, sessionID, clientMsgID, message, outcome string,
	retryAllowed bool,
) {
	msg := map[string]interface{}{
		"type":       "prompt_failed",
		"device_id":  deviceID,
		"session_id": sessionID,
		"timestamp":  time.Now().Unix(),
		"payload": map[string]interface{}{
			"client_msg_id": clientMsgID,
			"error":         message,
			"message":       message,
			"outcome":       outcome,
			"retry_allowed": retryAllowed,
		},
	}
	if err := client.Send(msg); err != nil {
		log.Printf("[daemon] send prompt_failed failed client_msg_id=%s: %v", clientMsgID, err)
	}
}

// sendDaemonError reports an error back to the server (forwarded to phones).
func sendDaemonError(client *connection.Client, deviceID, sessionID, message string) {
	msg := map[string]interface{}{
		"type":       "error",
		"device_id":  deviceID,
		"session_id": sessionID,
		"timestamp":  time.Now().Unix(),
		"payload":    map[string]interface{}{"message": message},
	}
	if err := client.Send(msg); err != nil {
		log.Printf("[daemon] send error message failed: %v", err)
	}
}
