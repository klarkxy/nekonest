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
	"sync"
	"syscall"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
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

	// Initialize adapters
	ccAdapter := adapters.NewClaudeCodeAdapter()
	codexAdapter := adapters.NewCodexAdapter()
	adapterList := []adapters.Adapter{ccAdapter, codexAdapter}

	// Log available agents
	for _, a := range adapterList {
		log.Printf("   Adapter: %s (available: %v)", a.Name(), a.IsAvailable())
	}

	// Create root context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create server connection
	client := connection.NewClient(ctx, cfg.ServerURL, cfg.DeviceID, cfg.Token)

	// Wire up agent output callbacks — send session_message back to server
	setupOutputCallbacks(ccAdapter, codexAdapter, client, cfg.DeviceID)

	// Session discovery state
	var (
		sessionMu    sync.Mutex
		lastSessions = make(map[string]*adapters.SessionInfo)
	)

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
			for _, a := range adapterList {
				if string(info.AgentType) == a.Name() {
					targetAdapter = a
					break
				}
			}
		}
		sessionMu.Unlock()

		switch msgType {
		case "send_prompt":
			prompt, _ := payload["prompt"].(string)
			if prompt == "" {
				log.Printf("[daemon] empty prompt")
				sendDaemonError(client, cfg.DeviceID, sessionID, "empty prompt")
				return
			}
			if targetAdapter == nil {
				log.Printf("[daemon] no adapter for session %s, trying all", sessionID)
				var lastErr error
				for _, a := range adapterList {
					if err := a.SendPrompt(sessionID, prompt); err == nil {
						log.Printf("[daemon] prompt sent via %s", a.Name())
						return
					} else {
						lastErr = err
					}
				}
				msg := "failed to send prompt: session not found or agent CLI error"
				if lastErr != nil {
					msg = lastErr.Error()
				}
				log.Printf("[daemon] %s", msg)
				sendDaemonError(client, cfg.DeviceID, sessionID, msg)
				return
			}
			if err := targetAdapter.SendPrompt(sessionID, prompt); err != nil {
				log.Printf("[daemon] send_prompt error: %v", err)
				sendDaemonError(client, cfg.DeviceID, sessionID, err.Error())
			} else {
				log.Printf("[daemon] prompt sent successfully")
			}

		case "approve":
			approvalID, _ := payload["approval_id"].(string)
			if targetAdapter == nil {
				sendDaemonError(client, cfg.DeviceID, sessionID, "no adapter for session (is agent process still running?)")
				return
			}
			if err := targetAdapter.Approve(sessionID, approvalID); err != nil {
				log.Printf("[daemon] approve error: %v", err)
				sendDaemonError(client, cfg.DeviceID, sessionID, "approve failed: "+err.Error())
			} else {
				log.Printf("[daemon] approved: %s", approvalID)
			}

		case "deny":
			approvalID, _ := payload["approval_id"].(string)
			if targetAdapter == nil {
				sendDaemonError(client, cfg.DeviceID, sessionID, "no adapter for session")
				return
			}
			if err := targetAdapter.Deny(sessionID, approvalID); err != nil {
				log.Printf("[daemon] deny error: %v", err)
				sendDaemonError(client, cfg.DeviceID, sessionID, "deny failed: "+err.Error())
			} else {
				log.Printf("[daemon] denied: %s", approvalID)
			}

		case "interrupt":
			if targetAdapter == nil {
				sendDaemonError(client, cfg.DeviceID, sessionID, "no adapter for session")
				return
			}
			if err := targetAdapter.Interrupt(sessionID); err != nil {
				log.Printf("[daemon] interrupt error: %v", err)
				sendDaemonError(client, cfg.DeviceID, sessionID, "interrupt failed: "+err.Error())
			} else {
				log.Printf("[daemon] interrupted session %s", sessionID)
			}

		case "create_session":
			// Experimental: prefer discovering existing sessions on the PC.
			agentType, _ := payload["agent_type"].(string)
			prompt, _ := payload["prompt"].(string)
			workDir, _ := payload["work_dir"].(string)

			log.Printf("[daemon] create_session (experimental): type=%s workDir=%s", agentType, workDir)

			go func() {
				newSessionID, err := startNewSession(agentType, prompt, workDir, ccAdapter, codexAdapter, cfg)
				if err != nil {
					log.Printf("[daemon] create_session error: %v", err)
					sendDaemonError(client, cfg.DeviceID, "", "create_session failed: "+err.Error()+
						" — prefer opening a session on the PC first, then use Discover/resume")
					return
				}

				confirmMsg := map[string]interface{}{
					"type":       "session_created",
					"device_id":  cfg.DeviceID,
					"session_id": newSessionID,
					"timestamp":  time.Now().Unix(),
					"payload": map[string]interface{}{
						"agent_type": agentType,
						"status":     "running",
					},
				}
				client.Send(confirmMsg)
				log.Printf("[daemon] session created: %s", newSessionID)
			}()

		case "heartbeat":
			// Server heartbeat, keep-alive handled by connection layer

		default:
			log.Printf("[daemon] unknown message type: %s", msgType)
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

		discoverAndReport := func() {
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
			changed := false
			sessionMu.Lock()

			// Build new session map
			newSessions := make(map[string]*adapters.SessionInfo)
			for _, s := range allSessions {
				newSessions[s.ID] = s
			}

			// Compare with last state
			if len(newSessions) != len(lastSessions) {
				changed = true
			} else {
				for id, newS := range newSessions {
					oldS, ok := lastSessions[id]
					if !ok || oldS.Status != newS.Status || oldS.Summary != newS.Summary {
						changed = true
						break
					}
				}
			}

			lastSessions = newSessions
			sessionMu.Unlock()

			// Always report on first run or when changed
			if changed {
				report := map[string]interface{}{
					"type":      "session_list",
					"device_id": cfg.DeviceID,
					"timestamp": time.Now().Unix(),
					"payload": map[string]interface{}{
						// Convert at the wire boundary: unix last_activity + device_id
						"sessions": sessionsToWire(cfg.DeviceID, allSessions),
					},
				}
				if err := client.Send(report); err != nil {
					log.Printf("[daemon] report error: %v", err)
				} else {
					log.Printf("[daemon] reported %d sessions", len(allSessions))
				}
			}
		}

		// First discovery
		discoverAndReport()

		// Periodic discovery
		for {
			select {
			case <-ticker.C:
				discoverAndReport()
			case <-ctx.Done():
				log.Printf("[daemon] discovery loop stopped")
				return
			}
		}
	}()

	// Start config hot-reload watcher
	go config.WatchForChanges(ctx, func(newCfg *config.Config) {
		log.Printf("[daemon] config reloaded, server=%s", newCfg.ServerURL)
		// If server URL changed, we need to reconnect
		if newCfg.ServerURL != cfg.ServerURL {
			log.Printf("[daemon] server URL changed, reconnecting...")
			client.SetServerURL(newCfg.ServerURL)
		}
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
	for _, adapter := range adapterList {
		if closer, ok := adapter.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				log.Printf("[daemon] error closing adapter %s: %v", adapter.Name(), err)
			}
		}
	}

	// 4. Wait a bit for goroutines to finish
	time.Sleep(500 * time.Millisecond)
	log.Println("[daemon] goodbye 🐱")
}

// setupOutputCallbacks wires the agent output from commanders through to the server connection.
// When an agent produces output, it gets sent as a session_message back to the server.
func setupOutputCallbacks(
	ccAdapter *adapters.ClaudeCodeAdapter,
	codexAdapter *adapters.CodexAdapter,
	client *connection.Client,
	deviceID string,
) {
	// Claude Code output callback
	ccCommander := ccAdapter.GetCommander()
	ccCommander.OnAgentOutput = func(sessionID, msgType, content string) {
		sendSessionMessage(client, deviceID, sessionID, "claude_code", msgType, content)
	}

	// Codex output callback
	codexCommander := codexAdapter.GetCommander()
	codexCommander.OnAgentOutput = func(sessionID, msgType, content string) {
		sendSessionMessage(client, deviceID, sessionID, "codex", msgType, content)
	}
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
func sendSessionMessage(client *connection.Client, deviceID, sessionID, agentType, msgType, content string) {
	// Determine role from msgType
	role := "assistant"
	switch msgType {
	case "system":
		role = "system"
	case "tool_call":
		role = "assistant"
	case "tool_result":
		role = "tool"
	case "error":
		role = "system"
	}

	msg := map[string]interface{}{
		"type":      "session_message",
		"device_id": deviceID,
		"session_id": sessionID,
		"timestamp": time.Now().Unix(),
		"payload": map[string]interface{}{
			"message": map[string]interface{}{
				"id":        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
				"role":      role,
				"content":   content,
				"type":      msgType,
				"timestamp": time.Now().Unix(),
				"metadata": map[string]interface{}{
					"agent_type": agentType,
				},
			},
		},
	}

	if err := client.Send(msg); err != nil {
		log.Printf("[daemon] send session_message error: %v", err)
	}
}

// startNewSession is experimental. Preferred path: open a session on the PC,
// let Discover report the real session id, then send_prompt with --resume.
func startNewSession(agentType, prompt, workDir string, ccAdapter *adapters.ClaudeCodeAdapter, codexAdapter *adapters.CodexAdapter, cfg *config.Config) (string, error) {
	_ = workDir
	_ = cfg
	_ = ccAdapter
	_ = codexAdapter
	_ = prompt
	return "", fmt.Errorf(
		"create_session is not reliable yet for %s; open Claude Code/Codex on the PC first, wait for the session to appear in the phone list, then send prompts there",
		agentType,
	)
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
