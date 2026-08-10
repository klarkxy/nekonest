package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
	"github.com/nekonest/daemon/internal/agentexec"
	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/buildinfo"
	"github.com/nekonest/daemon/internal/config"
	"github.com/nekonest/daemon/internal/connection"
	"github.com/nekonest/daemon/internal/identity"
	"github.com/nekonest/daemon/internal/opslog"
	"github.com/nekonest/daemon/internal/sealed"
	"github.com/nekonest/daemon/internal/sealedkeys"
	"github.com/nekonest/daemon/internal/startjournal"
)

var (
	daemonSealedKeys *sealedkeys.Manager
	daemonTransport  = "open"
	// forceDiscoverCh triggers an immediate session discovery/report.
	forceDiscoverCh = make(chan struct{}, 1)
)

const (
	maxPromptAttachments     = 5
	sessionDiscoveryInterval = 30 * time.Second
)

func requestForceDiscover() {
	select {
	case forceDiscoverCh <- struct{}{}:
	default:
	}
}

func main() {
	configPath := flag.String("config", "", "config file path (default: ~/.nekonest/config.json)")
	pairFlag := flag.String("pair", "", "generate phone pair code for already-registered device (e.g. -pair gen)")
	register := flag.Bool("register", false, "register this device with the server (needs NEKONEST_SERVER)")
	deviceName := flag.String("name", "", "device name (for registration)")
	doctor := flag.Bool("doctor", false, "run non-interactive diagnostics and exit")
	showVersion := flag.Bool("version", false, "print the NekoNest daemon version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}
	if _, err := opslog.Configure(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	opslog.RedirectStandard("daemon.legacy")

	// Handle registration flow
	if *register {
		if code := handleRegistration(*deviceName); code != 0 {
			os.Exit(code)
		}
		return
	}

	// Handle pair-code generation
	if *pairFlag != "" {
		if code := handlePairing(*pairFlag); code != 0 {
			os.Exit(code)
		}
		return
	}

	if *doctor {
		cfgPath := *configPath
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}
		os.Exit(runDoctor(cfgPath))
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

	opslog.Info("daemon.main", "starting", "daemon starting", "version", buildinfo.Version)
	// The mode belongs to the registered nest, not to an ad-hoc process
	// environment. NEKONEST_TRANSPORT_MODE can only assert the value already
	// persisted in config; accepting a different value would silently downgrade
	// (or strand) a sealed nest.
	daemonTransport = cfg.TransportMode
	if requested := strings.TrimSpace(os.Getenv("NEKONEST_TRANSPORT_MODE")); requested != "" {
		mode, modeErr := config.NormalizeTransportMode(requested)
		if modeErr != nil {
			opslog.Error("daemon.main", "transport_mode_invalid", "invalid configured transport mode", modeErr)
			os.Exit(1)
		}
		if mode != daemonTransport {
			opslog.Error("daemon.main", "transport_mode_mismatch", "configured transport mode does not match environment assertion", nil)
			os.Exit(1)
		}
	}
	opslog.Info("daemon.main", "transport_mode_loaded", "persistent transport mode loaded", "transport_mode", daemonTransport)
	if sk, err := sealedkeys.LoadOrCreate(sealedkeys.DefaultPath()); err != nil {
		opslog.Error("daemon.main", "sealed_keys_unavailable", "sealed keys unavailable", err)
	} else {
		daemonSealedKeys = sk
		opslog.Info("daemon.main", "sealed_keys_loaded", "sealed keys loaded")
	}

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
		opslog.Error("daemon.main", "config_path_resolve_failed", "config path resolution failed", err)
		os.Exit(1)
	}
	activeConfigPath, err = filepath.EvalSymlinks(activeConfigPath)
	if err != nil {
		opslog.Error("daemon.main", "config_path_canonicalize_failed", "config path canonicalization failed", err)
		os.Exit(1)
	}
	journalPath := promptJournalPath(activeConfigPath, deviceID)
	instanceLock, lockErr := acquireDaemonInstanceLock(activeConfigPath + ".daemon.lock")
	if lockErr != nil {
		opslog.Error("daemon.main", "instance_lock_failed", "daemon instance lock acquisition failed", lockErr)
		os.Exit(1)
	}
	defer func() {
		if err := instanceLock.Close(); err != nil {
			opslog.Error("daemon.main", "instance_lock_release_failed", "daemon instance lock release failed", err)
		}
	}()
	commandJournal, journalErr := loadPromptJournal(
		journalPath,
		deviceID,
		maxAcceptedPromptIDs,
	)
	if journalErr != nil {
		// Failing open could replay a prompt that already reached an agent.
		opslog.Error("daemon.main", "prompt_journal_load_failed", "prompt journal load failed", journalErr)
		os.Exit(1)
	}
	opslog.Info("daemon.main", "prompt_journal_loaded", "prompt journal loaded")
	queuePath := promptQueuePath(activeConfigPath, deviceID)
	durableQueue, queueErr := loadPromptQueue(queuePath)
	queueAvailable := queueErr == nil
	if queueErr != nil {
		// Queueing is an optional capability. Unlike the command journal, a
		// damaged queue must not permit a fail-open replay, so keep it disabled.
		opslog.Error("daemon.main", "prompt_queue_unavailable", "prompt queue unavailable", queueErr)
	} else {
		opslog.Info("daemon.main", "prompt_queue_loaded", "prompt queue loaded")
	}
	threadJournalPath := startjournal.Path(activeConfigPath, deviceID)
	threadJournal, threadJournalErr := startjournal.Load(threadJournalPath, deviceID)
	if threadJournalErr != nil {
		// A missing/ambiguous start record can cause a second native thread.
		opslog.Error("daemon.main", "thread_start_journal_load_failed", "thread start journal load failed", threadJournalErr)
		os.Exit(1)
	}
	opslog.Info("daemon.main", "thread_start_journal_loaded", "thread start journal loaded")

	// Initialize the built-in adapter registry.
	adapterRegistry, err := adapters.NewDefaultRegistry()
	if err != nil {
		opslog.Error("daemon.main", "adapter_initialize_failed", "adapter initialization failed", err)
		os.Exit(1)
	}
	adapterList := adapterRegistry.All()

	// Log available agents
	for _, a := range adapterList {
		opslog.Info("daemon.main", "adapter_initialized", "adapter initialized", "agent_type", a.Name(), "available", a.IsAvailable())
	}

	// Create root context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create server connection
	client := connection.NewClient(ctx, initialCfg.ServerURL, deviceID, token, daemonTransport)
	acceptedPrompts := newPromptAcceptanceCache(maxAcceptedPromptIDs)
	sessionSends := newSessionLockMap()
	activeTurns := newActiveTurnRegistry()

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
	var turnGenerations atomic.Uint64
	var dispatchQueued func(string)
	var handleControlEvent func(adapters.ControlEvent)
	handleControlEvent = func(event adapters.ControlEvent) {
		lifecycle := event.Lifecycle
		if lifecycle == "" {
			switch event.Class {
			case "completed":
				lifecycle = adapters.TurnTerminalSuccess
			case "failed":
				lifecycle = adapters.TurnTerminalFailure
			case "interrupted", "cancelled", "canceled":
				lifecycle = adapters.TurnInterrupted
			}
		}
		event.Lifecycle = lifecycle
		terminal := lifecycle == adapters.TurnTerminalSuccess || lifecycle == adapters.TurnTerminalFailure ||
			lifecycle == adapters.TurnInterrupted || lifecycle == adapters.TurnIndeterminate
		if terminal {
			binding, ready, matched := activeTurns.terminalForEvent(event)
			if !matched {
				opslog.Warn("daemon.main", "terminal_control_stale", "stale terminal control event ignored", "session_id", event.SessionID, "client_msg_id", event.ClientMsgID, "generation", event.Generation)
				return
			}
			if !ready {
				return
			}
			event.Generation = binding.Generation
			event.ClientMsgID = binding.ClientMsgID
			if event.NativeRequestID == "" {
				event.NativeRequestID = binding.NativeRequestID
			}
			if activeTurns.clearMatching(event.SessionID, binding.Generation, binding.ClientMsgID, binding.NativeRequestID) {
				event.ClearActiveTurn = true
			}
		}
		if sessionUpdate, attention := controlEventWirePlan(event); sessionUpdate || attention {
			sendControlEvent(client, deviceID, event)
		}
		if queueAvailable && event.SessionID != "" {
			if running, ok := durableQueue.running(event.SessionID); ok && (event.ClientMsgID == "" || event.ClientMsgID == running.ClientMsgID) {
				switch lifecycle {
				case adapters.TurnTerminalSuccess:
					if err := durableQueue.complete(event.SessionID, running.ClientMsgID); err != nil {
						opslog.Error("daemon.main", "queued_prompt_complete_failed", "queued prompt completion failed", err, "session_id", event.SessionID, "client_msg_id", running.ClientMsgID)
					}
				case adapters.TurnTerminalFailure:
					if err := durableQueue.block(event.SessionID, running.ClientMsgID, promptQueueBlockedFailed); err != nil {
						opslog.Error("daemon.main", "queued_prompt_block_failed", "failed queued prompt could not be blocked", err, "session_id", event.SessionID, "client_msg_id", running.ClientMsgID, "status", promptQueueBlockedFailed)
					}
				case adapters.TurnInterrupted:
					if err := durableQueue.block(event.SessionID, running.ClientMsgID, promptQueueBlockedInterrupted); err != nil {
						opslog.Error("daemon.main", "queued_prompt_block_failed", "interrupted queued prompt could not be blocked", err, "session_id", event.SessionID, "client_msg_id", running.ClientMsgID, "status", promptQueueBlockedInterrupted)
					}
				case adapters.TurnIndeterminate:
					if err := durableQueue.block(event.SessionID, running.ClientMsgID, promptQueueBlockedIndeterminate); err != nil {
						opslog.Error("daemon.main", "queued_prompt_block_failed", "indeterminate queued prompt could not be blocked", err, "session_id", event.SessionID, "client_msg_id", running.ClientMsgID, "status", promptQueueBlockedIndeterminate)
					}
				}
				sendQueueUpdate(client, deviceID, event.SessionID, durableQueue)
				if lifecycle == adapters.TurnTerminalSuccess && dispatchQueued != nil {
					go dispatchQueued(event.SessionID)
				}
			}
		}
		requestForceDiscover()
	}
	adapterRegistry.SetControlSink(handleControlEvent)
	if adapter, ok := adapterRegistry.Get("codex"); ok {
		if codex, ok := adapter.(*adapters.CodexAdapter); ok {
			codex.SetRecoverySink(requestForceDiscover)
		}
	}
	dispatchQueued = func(sessionID string) {
		unlock := sessionSends.lock(sessionID)
		defer unlock()
		if !queueAvailable {
			return
		}
		target := pickAdapterForSession(sessionID, adapterList)
		if target == nil {
			return
		}
		if codex, ok := target.(*adapters.CodexAdapter); ok && (!codex.AppServerHealthy() || codex.HasActiveTurn(sessionID)) {
			return
		}
		item, ok, claimErr := durableQueue.claimNext(sessionID)
		if claimErr != nil || !ok {
			if claimErr != nil {
				opslog.Error("daemon.main", "queued_prompt_claim_failed", "queued prompt claim failed", claimErr, "session_id", sessionID)
			}
			return
		}
		sendQueueUpdate(client, deviceID, sessionID, durableQueue)
		if target.Name() != item.AgentType {
			_ = durableQueue.block(sessionID, item.ClientMsgID, promptQueueBlockedIndeterminate)
			sendQueueUpdate(client, deviceID, sessionID, durableQueue)
			return
		}
		prompt, refs, dispatchErr := queuedPromptPayload(deviceID, sessionID, item)
		var files []attach.LocalFile
		attDir := ""
		if dispatchErr == nil && len(refs) > 0 {
			var suffix string
			attDir, files, suffix, dispatchErr = attach.Materialize(currentConfig().ServerURL, sessionID, refs)
			if dispatchErr == nil {
				if prompt == "" {
					prompt = "(user sent attachments)"
				}
				prompt += suffix
			}
		}
		crossedNativeBoundary := false
		if dispatchErr == nil {
			crossedNativeBoundary, dispatchErr = dispatchQueuedPrompt(target, commandJournal, acceptedPrompts, client, deviceID, item, prompt, files, attDir, turnGenerations.Add(1), activeTurns, handleControlEvent)
		}
		if dispatchErr == nil {
			return
		}
		if attDir != "" && !crossedNativeBoundary {
			_ = os.RemoveAll(attDir)
		}
		opslog.Error("daemon.main", "queued_prompt_dispatch_failed", "queued prompt dispatch failed", dispatchErr, "session_id", sessionID, "client_msg_id", item.ClientMsgID, "agent_type", item.AgentType)
		if errors.Is(dispatchErr, agentexec.ErrSessionBusy) {
			if err := durableQueue.releaseClaim(sessionID, item.ClientMsgID); err != nil {
				opslog.Error("daemon.main", "queued_prompt_claim_release_failed", "busy queued prompt claim release failed", err, "session_id", sessionID, "client_msg_id", item.ClientMsgID)
			}
			sendQueueUpdate(client, deviceID, sessionID, durableQueue)
			return
		}
		_ = durableQueue.block(sessionID, item.ClientMsgID, promptQueueBlockedIndeterminate)
		sendQueueUpdate(client, deviceID, sessionID, durableQueue)
		sendPromptIndeterminate(client, deviceID, sessionID, item.ClientMsgID, "queued prompt became indeterminate before a safe native acceptance: "+dispatchErr.Error())
	}

	// Session discovery state
	var (
		sessionMu             sync.Mutex
		lastSessions          = make(map[string]*adapters.SessionInfo)
		lastStartCapabilities []map[string]interface{}
	)
	startCapabilityCache := newAgentStartCapabilityCache(30 * time.Second)
	threadStarts := &threadStartCoordinator{
		journal:       threadJournal,
		lookupAdapter: adapterRegistry.Get,
		snapshotProjectDirs: func() []string {
			return snapshotProjectDirs(&sessionMu, lastSessions)
		},
		materializeAttachments: func(operationID string, refs []attach.Ref) (string, []attach.LocalFile, string, error) {
			return attach.Materialize(currentConfig().ServerURL, operationID, refs)
		},
	}

	// Handle incoming messages from server
	client.OnMessage(func(data []byte) {
		// Panic recovery for message handling
		defer func() {
			if r := recover(); r != nil {
				opslog.Error("daemon.main", "message_handler_panic", "message handler panicked", nil)
			}
		}()

		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			opslog.Error("daemon.main", "message_decode_failed", "incoming message decoding failed", err)
			return
		}

		msgType, _ := msg["type"].(string)
		sessionID, _ := msg["session_id"].(string)
		payload, _ := msg["payload"].(map[string]interface{})
		var sealedPromptWire []byte
		if msgType == "send_prompt" {
			if _, sealed := msg["sealed_payload"]; sealed {
				// A queued retry must preserve the exact original command envelope.
				sealedPromptWire = bytes.Clone(data)
			}
		}
		if daemonInboundApplicationType(msgType) {
			decoded, err := decodeInboundApplicationCommand(deviceID, sessionID, msg, msgType)
			if err != nil {
				opslog.Warn("daemon.main", "transport_envelope_rejected", "incoming transport envelope rejected", "message_type", safeInboundMessageTypeForLog(msgType))
				sendInboundDecodeFailure(client, deviceID, sessionID, msgType, msg, err)
				return
			}
			payload = decoded
		} else if err := validateInboundRoutingFrame(msg); err != nil {
			opslog.Warn("daemon.main", "routing_frame_rejected", "incoming routing frame rejected", "message_type", safeInboundMessageTypeForLog(msgType))
			return
		}

		opslog.Debug("daemon.main", "message_received", "incoming message received", "message_type", safeInboundMessageTypeForLog(msgType))

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
		case "refresh_sessions":
			// An authenticated phone subscription asked for a fresh native-store
			// catalog. Coalescing keeps repeated browser reloads from causing a
			// discovery storm while still bypassing the normal polling interval.
			requestForceDiscover()

		case "pair_ready":
			// Phone completed pairing; wrap catalog key for that phone.
			go publishCatalogKeyForPhone(currentConfig(), payload)
			return
		case "send_prompt":
			// Serialize sends per session (prevents duplicate native processes).
			unlock := sessionSends.lock(sessionID)
			defer unlock()

			if payload == nil {
				payload = map[string]interface{}{}
			}

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
					opslog.Info("daemon.main", "prompt_acceptance_reacknowledged", "durable prompt acceptance reacknowledged", "session_id", sessionID, "client_msg_id", clientMsgID, "status", recorded.Status)
					sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				default:
					opslog.Warn("daemon.main", "prompt_replay_refused", "unresolved prompt replay refused", "session_id", sessionID, "client_msg_id", clientMsgID, "status", recorded.Status)
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate)
				}
				return
			}
			if accepted, duplicate := acceptedPrompts.get(sessionID, clientMsgID); duplicate {
				opslog.Info("daemon.main", "prompt_acceptance_reacknowledged", "cached prompt acceptance reacknowledged", "session_id", sessionID, "client_msg_id", clientMsgID, "status", promptJournalAccepted)
				sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				return
			}
			if queueAvailable {
				if queued, exists := durableQueue.item(sessionID, clientMsgID); exists {
					if queued.Status == promptQueueCancelled {
						sendPromptCancelled(client, deviceID, sessionID, clientMsgID)
					} else {
						sendPromptQueued(client, deviceID, sessionID, clientMsgID, durableQueue.position(sessionID, clientMsgID))
						sendQueueUpdate(client, deviceID, sessionID, durableQueue)
					}
					return
				}
			}
			prompt, _ := payload["prompt"].(string)
			originalPrompt := prompt
			// Drop stale attachment blocks if client re-sent an already-augmented draft.
			prompt = stripNekoAttachSuffix(prompt)
			refs := parseAttachmentRefs(payload["attachments"])
			if len(refs) > maxPromptAttachments {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, fmt.Sprintf("too many attachments (limit %d)", maxPromptAttachments))
				return
			}
			if prompt == "" && len(refs) == 0 {
				logEmptyPromptRejected(sessionID, clientMsgID)
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "empty prompt")
				return
			}
			if targetAdapter == nil {
				opslog.Debug("daemon.main", "adapter_cache_miss", "session adapter cache missed; probing native stores", "session_id", sessionID)
				// Prefer single best guess — never blast all agents (that multi-sends).
				targetAdapter = pickAdapterForSession(sessionID, adapterList)
			}
			if targetAdapter == nil {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "session not found on this PC")
				return
			}
			queueCapable := queueAvailable
			if codex, ok := targetAdapter.(*adapters.CodexAdapter); ok {
				queueCapable = queueAvailable && codex.AppServerHealthy()
			}
			if queueCapable {
				queued := promptQueueItem{
					SessionID: sessionID, ClientMsgID: clientMsgID, AgentType: targetAdapter.Name(),
				}
				if len(sealedPromptWire) > 0 {
					queued.SealedEnvelope = sealedPromptWire
				} else {
					queued.Prompt = originalPrompt
					if rawAttachments, err := json.Marshal(payload["attachments"]); err == nil && string(rawAttachments) != "null" {
						queued.Attachments = rawAttachments
					}
				}
				stored, _, err := durableQueue.enqueue(queued)
				if err != nil {
					sendPromptFailed(client, deviceID, sessionID, clientMsgID, "prompt could not be durably queued: "+err.Error())
					return
				}
				sendPromptQueued(client, deviceID, sessionID, clientMsgID, durableQueue.position(sessionID, stored.ClientMsgID))
				sendQueueUpdate(client, deviceID, sessionID, durableQueue)
				if dispatchQueued != nil {
					go dispatchQueued(sessionID)
				}
				return
			}

			var (
				localAttachments []attach.LocalFile
				releaseFiles     func()
				runOwnsFiles     bool
			)
			defer func() {
				if releaseFiles != nil && !runOwnsFiles {
					releaseFiles()
				}
			}()

			if len(refs) > 0 {
				attDir, files, suffix, aerr := attach.Materialize(
					currentConfig().ServerURL,
					sessionID,
					refs,
				)
				if aerr != nil {
					opslog.Error("daemon.main", "attachment_materialize_failed", "prompt attachments could not be materialized", aerr, "session_id", sessionID, "client_msg_id", clientMsgID, "count", len(refs))
					sendPromptFailed(client, deviceID, sessionID, clientMsgID, "attachment download failed: "+aerr.Error())
					return
				}
				localAttachments = files
				var cleanupOnce sync.Once
				releaseFiles = func() {
					cleanupOnce.Do(func() {
						if err := os.RemoveAll(attDir); err != nil {
							opslog.Error("daemon.main", "attachment_cleanup_failed", "prompt attachment cleanup failed", err, "session_id", sessionID, "client_msg_id", clientMsgID)
						}
					})
				}
				if prompt == "" {
					prompt = "(user sent attachments)"
				}
				prompt = prompt + suffix
				opslog.Info("daemon.main", "attachments_materialized", "prompt attachments materialized", "session_id", sessionID, "client_msg_id", clientMsgID, "count", len(localAttachments))
			}

			// Persist before crossing the Agent boundary. If the daemon dies
			// after this write, startup converts dispatching to indeterminate
			// and will never automatically execute the command again.
			if err := commandJournal.markDispatching(sessionID, clientMsgID, originalPrompt); err != nil {
				opslog.Error("daemon.main", "prompt_dispatch_journal_failed", "prompt dispatch journal write failed", err, "session_id", sessionID, "client_msg_id", clientMsgID)
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "prompt was not executed because its durable dispatch record could not be written: "+err.Error())
				return
			}
			generation := turnGenerations.Add(1)
			if !activeTurns.bind(sessionID, generation, clientMsgID) {
				if journalErr := commandJournal.rollbackDispatching(sessionID, clientMsgID); journalErr != nil {
					opslog.Error("daemon.main", "prompt_dispatch_rollback_failed", "active-turn conflict rollback failed", journalErr, "session_id", sessionID, "client_msg_id", clientMsgID, "generation", generation)
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate+"; active turn conflict")
				} else {
					sendPromptFailed(client, deviceID, sessionID, clientMsgID, "this session already has a controllable turn")
				}
				return
			}
			var completeOnce sync.Once
			onComplete := func() {
				completeOnce.Do(func() {
					if releaseFiles != nil {
						releaseFiles()
					}
					if activeTurns.completeMatching(sessionID, generation, clientMsgID) {
						sendControlEvent(client, deviceID, adapters.ControlEvent{
							SessionID: sessionID, AgentType: adapters.AgentType(targetAdapter.Name()), ClearActiveTurn: true,
						})
					}
				})
			}
			request := adapters.PromptRequest{
				Prompt:      prompt,
				Attachments: localAttachments,
				OnComplete:  onComplete,
				Generation:  generation,
				ClientMsgID: clientMsgID,
				OnNativeBound: func(nativeRequestID string) {
					activeTurns.setNativeRequestID(sessionID, generation, clientMsgID, nativeRequestID)
				},
			}
			if err := targetAdapter.SendPrompt(sessionID, request); err != nil {
				opslog.Error("daemon.main", "prompt_dispatch_failed", "prompt dispatch through adapter failed", err, "session_id", sessionID, "client_msg_id", clientMsgID, "agent_type", targetAdapter.Name(), "generation", generation)
				activeTurns.clearMatching(sessionID, generation, clientMsgID, "")
				if errors.Is(err, adapters.ErrPromptBoundaryIndeterminate) {
					// A timed-out or malformed native RPC may still be using the
					// materialized files. Retain them rather than breaking that turn.
					runOwnsFiles = true
				}
				if errors.Is(err, agentexec.ErrSessionBusy) {
					// Busy is checked before an adapter starts a new process,
					// so this rejection is known not to have crossed the agent
					// boundary. Roll back the dispatch marker and allow an
					// explicit retry after the active run finishes.
					if journalErr := commandJournal.rollbackDispatching(sessionID, clientMsgID); journalErr == nil {
						sendPromptFailed(
							client,
							deviceID,
							sessionID,
							clientMsgID,
							"this session is still running; wait for the current turn to finish, then retry",
						)
						return
					} else {
						opslog.Error("daemon.main", "prompt_dispatch_rollback_failed", "busy prompt dispatch rollback failed", journalErr, "session_id", sessionID, "client_msg_id", clientMsgID, "generation", generation)
					}
				}
				// The adapter API cannot prove whether a failing Start/Send
				// crossed into the agent. Preserve the ambiguity instead of
				// enabling an unsafe automatic retry.
				if journalErr := commandJournal.markIndeterminate(sessionID, clientMsgID); journalErr != nil {
					opslog.Error("daemon.main", "prompt_indeterminate_persist_failed", "indeterminate prompt state could not be persisted", journalErr, "session_id", sessionID, "client_msg_id", clientMsgID, "generation", generation)
				}
				sendPromptIndeterminate(
					client,
					deviceID,
					sessionID,
					clientMsgID,
					promptOutcomeIndeterminate+"; agent returned: "+err.Error(),
				)
			} else {
				// The resumed process now owns the temporary files and invokes
				// OnComplete only after its output readers and process exit.
				runOwnsFiles = true
				if binding, ok := activeTurns.current(sessionID); ok {
					sendControlEvent(client, deviceID, adapters.ControlEvent{
						SessionID: sessionID, AgentType: adapters.AgentType(targetAdapter.Name()),
						Status: adapters.StatusRunning, ActiveTurn: binding,
					})
				}
				if err := commandJournal.markAccepted(sessionID, clientMsgID); err != nil {
					opslog.Error("daemon.main", "prompt_acceptance_persist_failed", "prompt acceptance could not be persisted", err, "session_id", sessionID, "client_msg_id", clientMsgID, "agent_type", targetAdapter.Name(), "generation", generation)
					if activeTurns.abandonAcceptance(sessionID, generation, clientMsgID) {
						sendControlEvent(client, deviceID, adapters.ControlEvent{
							SessionID: sessionID, AgentType: adapters.AgentType(targetAdapter.Name()), ClearActiveTurn: true,
						})
					}
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate+"; acceptance journal update failed")
					return
				}
				pendingTerminal, completed := activeTurns.accept(sessionID, generation, clientMsgID)
				if completed {
					sendControlEvent(client, deviceID, adapters.ControlEvent{
						SessionID: sessionID, AgentType: adapters.AgentType(targetAdapter.Name()), ClearActiveTurn: true,
					})
				}
				if pendingTerminal != nil {
					handleControlEvent(*pendingTerminal)
				}
				accepted := acceptedPrompt{prompt: boundedPromptEcho(originalPrompt)}
				acceptedPrompts.add(sessionID, clientMsgID, accepted)
				sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				opslog.Info("daemon.main", "prompt_accepted", "prompt accepted by native agent", "session_id", sessionID, "client_msg_id", clientMsgID, "agent_type", targetAdapter.Name(), "generation", generation)
			}

		case "prompt_status_query":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			clientMsgID, _ := payload["client_msg_id"].(string)
			if clientMsgID == "" {
				clientMsgID, _ = msg["client_msg_id"].(string)
			}
			if clientMsgID == "" {
				sendPromptFailed(client, deviceID, sessionID, "", "client_msg_id required")
				return
			}
			if queueAvailable {
				if queued, exists := durableQueue.item(sessionID, clientMsgID); exists {
					if queued.Status == promptQueueCancelled {
						sendPromptCancelled(client, deviceID, sessionID, clientMsgID)
					} else {
						sendPromptQueued(client, deviceID, sessionID, clientMsgID, durableQueue.position(sessionID, clientMsgID))
						sendQueueUpdate(client, deviceID, sessionID, durableQueue)
					}
					return
				}
			}
			if recorded, seen := commandJournal.state(sessionID, clientMsgID); seen {
				if recorded.Status == promptJournalAccepted || recorded.Status == promptJournalCommitted {
					accepted := acceptedPrompt{prompt: recorded.PromptEcho}
					if cached, ok := acceptedPrompts.get(sessionID, clientMsgID); ok {
						accepted = cached
					}
					opslog.Info("daemon.main", "prompt_status_accepted", "durable prompt status is accepted", "session_id", sessionID, "client_msg_id", clientMsgID, "status", recorded.Status)
					sendPromptAccepted(client, deviceID, sessionID, clientMsgID, accepted)
				} else {
					opslog.Warn("daemon.main", "prompt_status_unresolved", "durable prompt status is unresolved", "session_id", sessionID, "client_msg_id", clientMsgID, "status", recorded.Status)
					sendPromptIndeterminate(client, deviceID, sessionID, clientMsgID, promptOutcomeIndeterminate)
				}
				return
			}
			opslog.Info("daemon.main", "prompt_status_not_seen", "prompt status absent from durable journal", "session_id", sessionID, "client_msg_id", clientMsgID, "status", "not_seen")
			sendPromptNotSeen(client, deviceID, sessionID, clientMsgID)

		case "prompt_committed":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			clientMsgID, _ := payload["client_msg_id"].(string)
			if clientMsgID == "" {
				clientMsgID, _ = msg["client_msg_id"].(string)
			}
			if clientMsgID == "" {
				opslog.Warn("daemon.main", "prompt_commit_rejected", "prompt commit without client message id rejected", "session_id", sessionID)
				return
			}
			recorded, seen := commandJournal.state(sessionID, clientMsgID)
			if !seen {
				// A duplicate commit may arrive after an old committed record
				// was evicted; there is nothing left to transition.
				opslog.Warn("daemon.main", "prompt_commit_record_absent", "prompt commit record is absent or evicted", "session_id", sessionID, "client_msg_id", clientMsgID)
				return
			}
			if recorded.Status == promptJournalCommitted {
				return
			}
			if recorded.Status != promptJournalAccepted {
				opslog.Warn("daemon.main", "prompt_commit_refused", "prompt commit refused for current state", "session_id", sessionID, "client_msg_id", clientMsgID, "status", recorded.Status)
				return
			}
			if err := commandJournal.markCommitted(sessionID, clientMsgID); err != nil {
				opslog.Error("daemon.main", "prompt_commit_persist_failed", "prompt commit could not be persisted", err, "session_id", sessionID, "client_msg_id", clientMsgID)
				return
			}
			opslog.Info("daemon.main", "prompt_committed", "prompt committed by server", "session_id", sessionID, "client_msg_id", clientMsgID, "status", promptJournalCommitted)

		case "cancel_prompt":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			clientMsgID, _ := payload["client_msg_id"].(string)
			if clientMsgID == "" {
				sendPromptFailed(client, deviceID, sessionID, "", "client_msg_id required")
				return
			}
			if !queueAvailable {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "prompt queue unavailable")
				return
			}
			_, exists := durableQueue.item(sessionID, clientMsgID)
			if !exists {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "queued prompt not found")
				return
			}
			if err := durableQueue.cancel(sessionID, clientMsgID); err != nil {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, err.Error())
				return
			}
			sendPromptCancelled(client, deviceID, sessionID, clientMsgID)
			sendQueueUpdate(client, deviceID, sessionID, durableQueue)

		case "resume_prompt_queue":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			if !queueAvailable {
				sendPromptFailed(client, deviceID, sessionID, "", "prompt queue unavailable")
				return
			}
			if err := durableQueue.resumeSession(sessionID); err != nil {
				sendPromptFailed(client, deviceID, sessionID, "", "prompt queue could not be resumed: "+err.Error())
				return
			}
			sendQueueUpdate(client, deviceID, sessionID, durableQueue)
			if dispatchQueued != nil {
				go dispatchQueued(sessionID)
			}

		case "skip_prompt_queue_item":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			clientMsgID, _ := payload["client_msg_id"].(string)
			if !queueAvailable || strings.TrimSpace(clientMsgID) == "" {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, "indeterminate queue blocker required")
				return
			}
			if err := durableQueue.skipIndeterminate(sessionID, clientMsgID); err != nil {
				sendPromptFailed(client, deviceID, sessionID, clientMsgID, err.Error())
				return
			}
			sendQueueUpdate(client, deviceID, sessionID, durableQueue)
			if dispatchQueued != nil {
				go dispatchQueued(sessionID)
			}

		case "approve":
			approvalID, _ := payload["approval_id"].(string)
			if targetAdapter == nil {
				sendDaemonError(client, deviceID, sessionID, "no adapter for session (is agent process still running?)")
				return
			}
			if err := targetAdapter.Approve(sessionID, approvalID); err != nil {
				opslog.Error("daemon.main", "approval_failed", "approval response failed", err, "session_id", sessionID)
				sendDaemonError(client, deviceID, sessionID, "approve failed: "+err.Error())
			} else {
				opslog.Info("daemon.main", "approval_completed", "approval response completed", "session_id", sessionID, "status", "approved")
			}

		case "deny":
			approvalID, _ := payload["approval_id"].(string)
			if targetAdapter == nil {
				sendDaemonError(client, deviceID, sessionID, "no adapter for session")
				return
			}
			if err := targetAdapter.Deny(sessionID, approvalID); err != nil {
				opslog.Error("daemon.main", "denial_failed", "denial response failed", err, "session_id", sessionID)
				sendDaemonError(client, deviceID, sessionID, "deny failed: "+err.Error())
			} else {
				opslog.Info("daemon.main", "denial_completed", "denial response completed", "session_id", sessionID, "status", "denied")
			}

		case "respond_user_input":
			requestID, _ := payload["request_id"].(string)
			answers := parseUserInputAnswers(payload["answers"])
			codex, _ := targetAdapter.(*adapters.CodexAdapter)
			if codex == nil {
				if adapter, exists := adapterRegistry.Get("codex"); exists {
					codex, _ = adapter.(*adapters.CodexAdapter)
				}
			}
			if codex == nil || strings.TrimSpace(requestID) == "" {
				sendUserInputResult(client, deviceID, sessionID, requestID, "stale", "pending user input not found")
				return
			}
			status, respondErr := codex.RespondUserInput(requestID, answers)
			message := ""
			if respondErr != nil {
				message = respondErr.Error()
			}
			sendUserInputResult(client, deviceID, sessionID, requestID, status, message)
			requestForceDiscover()

		case "interrupt":
			unlock := sessionSends.lock(sessionID)
			defer unlock()
			generation, clientMsgID, bindingErr := parseActiveTurnCommand(payload)
			if bindingErr != nil || !activeTurns.matches(sessionID, generation, clientMsgID) {
				sendDaemonError(client, deviceID, sessionID, "interrupt rejected: active turn changed or is no longer controllable")
				return
			}
			if targetAdapter == nil {
				sendDaemonError(client, deviceID, sessionID, "no adapter for session")
				return
			}
			if err := targetAdapter.Interrupt(sessionID); err != nil {
				opslog.Error("daemon.main", "interrupt_failed", "session interrupt failed", err, "session_id", sessionID, "client_msg_id", clientMsgID, "generation", generation)
				sendDaemonError(client, deviceID, sessionID, "interrupt failed: "+err.Error())
			} else {
				opslog.Info("daemon.main", "interrupt_completed", "session interrupt completed", "session_id", sessionID, "client_msg_id", clientMsgID, "generation", generation)
				if queueAvailable {
					if running, ok := durableQueue.running(sessionID); ok {
						if err := durableQueue.block(sessionID, running.ClientMsgID, promptQueueBlockedInterrupted); err != nil {
							opslog.Error("daemon.main", "queued_prompt_block_failed", "running queue item could not be blocked after interrupt", err, "session_id", sessionID, "client_msg_id", running.ClientMsgID, "status", promptQueueBlockedInterrupted)
						}
					}
					sendQueueUpdate(client, deviceID, sessionID, durableQueue)
				}
			}

		case "steer":
			text, _ := payload["text"].(string)
			if text == "" {
				text, _ = payload["prompt"].(string)
			}
			if text == "" {
				sendDaemonError(client, deviceID, sessionID, "steer text required")
				return
			}
			codex, ok := targetAdapter.(*adapters.CodexAdapter)
			if !ok || codex == nil {
				// try lookup codex adapter by name
				if a, exists := adapterRegistry.Get("codex"); exists {
					codex, _ = a.(*adapters.CodexAdapter)
				}
			}
			if codex == nil {
				sendDaemonError(client, deviceID, sessionID, "steer only supported for codex")
				return
			}
			if err := codex.Steer(sessionID, text); err != nil {
				opslog.Error("daemon.main", "steer_failed", "session steer failed", err, "session_id", sessionID, "agent_type", "codex")
				sendDaemonError(client, deviceID, sessionID, "steer failed: "+err.Error())
			}

		case "start_thread":
			if payload == nil {
				payload = map[string]interface{}{}
			}
			var agentName string
			if rawAgent, present := payload["agent_type"]; present {
				var validType bool
				agentName, validType = rawAgent.(string)
				if !validType {
					opID, _ := payload["operation_id"].(string)
					if opID == "" {
						opID, _ = msg["client_msg_id"].(string)
					}
					sendThreadResult(client, deviceID, "", opID, "thread_failed", "", false, "invalid agent_type")
					return
				}
			}
			agentType, validAgent := parseStartAgentType(agentName)
			if !validAgent {
				opID, _ := payload["operation_id"].(string)
				if opID == "" {
					opID, _ = msg["client_msg_id"].(string)
				}
				sendThreadResult(client, deviceID, agentName, opID, "thread_failed", "", false, "invalid agent_type")
				return
			}
			projectDir, _ := payload["project_dir"].(string)
			legacyCWD, _ := payload["cwd"].(string)
			projectDir, projectDirErr := coalesceStartProjectDir(projectDir, legacyCWD)
			if projectDirErr != nil {
				opID, _ := payload["operation_id"].(string)
				if opID == "" {
					opID, _ = msg["client_msg_id"].(string)
				}
				sendThreadResult(client, deviceID, string(agentType), opID, "thread_failed", "", false, projectDirErr.Error())
				return
			}
			first, _ := payload["prompt"].(string)
			if first == "" {
				first, _ = payload["initial_prompt"].(string)
			}
			opID, _ := payload["operation_id"].(string)
			if opID == "" {
				opID, _ = msg["client_msg_id"].(string)
			}
			refs := parseAttachmentRefs(payload["attachments"])
			if len(refs) > maxPromptAttachments {
				sendThreadResult(client, deviceID, string(agentType), opID, "thread_failed", "", false, fmt.Sprintf("too many attachments (limit %d)", maxPromptAttachments))
				return
			}
			// A malformed non-empty attachment list must not be silently dropped
			// before native thread/start. The phone retains its local draft after
			// this definitive pre-boundary failure.
			if raw, present := payload["attachments"]; present && len(refs) == 0 {
				if list, ok := raw.([]interface{}); !ok || len(list) > 0 {
					sendThreadResult(client, deviceID, string(agentType), opID, "thread_failed", "", false, "invalid thread-start attachments")
					return
				}
			}
			command := threadStartCommand{
				OperationID: opID,
				AgentType:   agentType,
				ProjectDir:  projectDir,
				Prompt:      first,
				Attachments: refs,
			}
			// Native startup can include an ACP handshake plus an ownership wait.
			// Keep it off the single WebSocket read loop so other sessions and
			// interrupt messages remain responsive.
			go func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						opslog.Error("daemon.main", "thread_start_panic", "thread start coordinator panicked", nil, "operation_id", command.OperationID, "agent_type", command.AgentType)
						message := "thread start coordinator panicked; automatic retry is disabled"
						if record, ok := threadStarts.journal.FailClosed(command.OperationID, message); ok {
							sendThreadResult(client, deviceID, record.AgentType, record.OperationID, string(record.Status), record.SessionID, record.PromptAccepted, record.Message)
						} else {
							sendThreadResult(client, deviceID, string(command.AgentType), command.OperationID, "thread_indeterminate", "", false, message)
						}
					}
				}()
				threadStarts.Handle(ctx, command, func(event threadStartEvent) {
					sendThreadResult(
						client,
						deviceID,
						event.AgentType,
						event.OperationID,
						string(event.State),
						event.SessionID,
						event.PromptAccepted,
						event.Message,
					)
					if event.State == startjournal.StatusOwned || event.State == startjournal.StatusIndeterminate {
						requestForceDiscover()
					}
				})
			}()

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
				if err != nil {
					opslog.Error("daemon.main", "history_fetch_failed", "native history fetch failed", err, "session_id", sessionID, "agent_type", source)
				} else {
					opslog.Warn("daemon.main", "history_unavailable", "native history unavailable", "session_id", sessionID, "agent_type", source)
				}
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
			sendDaemonApplication(client, deviceID, sessionID, "", "session_history", map[string]interface{}{
				"source": source, "truncated": len(msgs) >= limit, "limit": limit, "messages": msgs,
			}, false)
			opslog.Debug("daemon.main", "history_sent", "native history sent", "session_id", sessionID, "agent_type", source, "count", len(msgs))

		case "heartbeat":
			// Server heartbeat, keep-alive handled by connection layer

		default:
			opslog.Warn("daemon.main", "message_type_unknown", "unknown incoming message type ignored", "message_type", safeInboundMessageTypeForLog(msgType))
		}
	})

	// Session force-report after reconnect (server cache starts empty).
	forceReport := forceDiscoverCh
	requestForceReport := requestForceDiscover
	client.OnConnect(func() {
		requestForceReport()
		pending := commandJournal.uncommittedAccepted()
		if len(pending) > 0 {
			opslog.Info("daemon.main", "prompt_acceptances_replayed", "accepted prompts awaiting server commit reacknowledged", "device_id", deviceID, "count", len(pending))
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
		opslog.Error("daemon.main", "connect_failed", "initial server connection failed", err, "device_id", deviceID)
		os.Exit(1)
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

		discoverAndReport := func(force bool) {
			var allSessions []*adapters.SessionInfo
			for _, adapter := range adapterList {
				sessions, err := adapter.Discover()
				if err != nil {
					opslog.Error("daemon.main", "adapter_discovery_failed", "native session discovery failed", err, "agent_type", adapter.Name())
					continue
				}
				allSessions = append(allSessions, sessions...)
			}

			// Stamp capabilities (Codex full-control when app-server healthy).
			var codexApp *adapters.CodexAdapter
			var codexAppHealthy bool
			if a, ok := adapterRegistry.Get("codex"); ok {
				if ca, ok := a.(*adapters.CodexAdapter); ok {
					codexApp = ca
					codexAppHealthy = ca.AppServerHealthy()
				}
			}
			for _, s := range allSessions {
				if s == nil {
					continue
				}
				if s.Capabilities == nil {
					s.Capabilities = adapters.DefaultCapabilities(s.AgentType)
				}
				owner, ownerExists := adapterRegistry.Get(string(s.AgentType))
				available := ownerExists && owner != nil && owner.IsAvailable()
				if !available {
					s.Capabilities.Send = false
					s.Capabilities.Interrupt = false
					s.Capabilities.Queue = false
					s.Capabilities.UnavailableReasons = mergeCapabilityReasons(
						s.Capabilities.UnavailableReasons,
						map[string]string{"send": "cli_missing", "interrupt": "cli_missing", "queue": "cli_missing"},
					)
				} else if s.AgentType != adapters.AgentCodex {
					s.Capabilities.Send = true
					s.Capabilities.Interrupt = true
					s.Capabilities.ControlPath = "cli"
					s.Capabilities.UnavailableReasons = removeCapabilityReasons(
						s.Capabilities.UnavailableReasons, "send", "interrupt",
					)
					s.Capabilities.Queue = queueAvailable
					if !queueAvailable {
						s.Capabilities.UnavailableReasons = mergeCapabilityReasons(s.Capabilities.UnavailableReasons, map[string]string{"queue": "queue_journal_unavailable"})
					}
				}
				if s.AgentType == adapters.AgentCodex {
					if codexApp != nil {
						codexApp.ApplyAppServerOverlay(s)
					}
					if codexAppHealthy {
						s.Capabilities = &adapters.SessionCapabilities{
							ControlMode:    adapters.ControlAppServer,
							Send:           true,
							Approve:        true,
							Deny:           true,
							Interrupt:      true,
							Steer:          true,
							Queue:          queueAvailable,
							Spawn:          true,
							UserInput:      true,
							AttachmentMode: adapters.AttachNativeImageAndFile,
							ControlPath:    "app_server",
						}
					}
				}
			}
			// Check for changes
			startCapabilities := startCapabilityCache.Get(ctx, adapterRegistry)
			changed := force
			sessionMu.Lock()

			// Build new session map
			newSessions := make(map[string]*adapters.SessionInfo)
			overlayActiveTurns(allSessions, activeTurns)
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
						if !ok || oldS.Status != newS.Status || oldS.Summary != newS.Summary || oldS.ProjectDir != newS.ProjectDir || pendingApprovalChanged(oldS, newS) || pendingUserInputChanged(oldS, newS) || sessionCapabilitiesChanged(oldS, newS) || !reflect.DeepEqual(oldS.ActiveTurn, newS.ActiveTurn) {
							changed = true
							break
						}
					}
				}
			}

			lastSessions = newSessions
			sessionMu.Unlock()
			if !changed && !reflect.DeepEqual(lastStartCapabilities, startCapabilities) {
				changed = true
			}
			lastStartCapabilities = startCapabilities

			// Always report on force (reconnect) or when local list changed
			if changed {
				report, buildErr := buildDaemonApplicationMessage(client.ProtocolVersion(), deviceID, "", "", "session_list", map[string]interface{}{
					// Convert at the wire boundary: unix last_activity + device_id
					"sessions":           sessionsToWire(deviceID, allSessions),
					"start_capabilities": startCapabilities,
				}, true)
				if buildErr != nil {
					opslog.Error("daemon.main", "session_list_build_failed", "session list message build failed", buildErr, "device_id", deviceID, "count", len(allSessions))
					return
				}
				if err := client.Send(report); err != nil {
					opslog.Error("daemon.main", "session_list_send_failed", "session list report failed", err, "device_id", deviceID, "count", len(allSessions))
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
					opslog.Info("daemon.main", "session_list_sent", "session list reported", "device_id", deviceID, "count", len(allSessions), "forced", force)
				}
			}
		}

		runSessionDiscoveryLoop(ctx, sessionDiscoveryInterval, forceReport, discoverAndReport)
		opslog.Info("daemon.main", "discovery_stopped", "session discovery loop stopped", "device_id", deviceID)
	}()

	// Start config hot-reload watcher. Snapshots are immutable after Store.
	watchPath := activeConfigPath
	go config.WatchPath(ctx, watchPath, func(newCfg *config.Config) {
		oldCfg := currentConfig()
		nextCfg := *newCfg
		if nextCfg.TransportMode != daemonTransport {
			opslog.Warn("daemon.main", "config_reload_transport_refused", "transport mode hot reload refused", "device_id", deviceID, "transport_mode", nextCfg.TransportMode)
			return
		}
		credChanged := nextCfg.DeviceID != deviceID || nextCfg.Token != token
		if credChanged {
			opslog.Warn("daemon.main", "config_reload_credentials_refused", "credential hot reload refused", "device_id", deviceID)
			nextCfg.DeviceID = deviceID
			nextCfg.Token = token
		}
		urlChanged := nextCfg.ServerURL != oldCfg.ServerURL
		if urlChanged {
			opslog.Info("daemon.main", "config_reload_endpoint_changed", "server endpoint changed; reconnect requested", "device_id", deviceID)
		}
		// Endpoint change and snapshot publication share the same dispatch
		// linearization point. Existing callbacks finish against oldCfg; no
		// callback from the new endpoint can start before nextCfg is visible.
		client.SetServerURLAndPublish(nextCfg.ServerURL, func() {
			runtimeCfg.Store(&nextCfg)
		})
		opslog.Info("daemon.main", "config_reload_applied", "runtime config snapshot applied", "device_id", deviceID, "endpoint_changed", urlChanged, "credentials_changed", credChanged)
	})

	// Start read loop (blocking, handles reconnection)
	go client.StartReadLoop(ctx)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		opslog.Info("daemon.main", "shutdown_signal_received", "shutdown signal received", "signal", sig.String())
	case <-ctx.Done():
		opslog.Info("daemon.main", "shutdown_context_cancelled", "daemon context cancelled")
	}

	// Graceful shutdown
	opslog.Info("daemon.main", "shutdown_started", "daemon shutdown started", "device_id", deviceID)

	// 1. Cancel context to stop all goroutines
	cancel()

	// 2. Close WebSocket connection
	client.Close()

	// 3. Cleanup all adapter resources (watchers, running processes)
	if err := adapterRegistry.Close(); err != nil {
		opslog.Error("daemon.main", "adapter_shutdown_failed", "adapter shutdown failed", err)
	}

	// 4. Wait a bit for goroutines to finish
	time.Sleep(500 * time.Millisecond)
	opslog.Info("daemon.main", "shutdown_completed", "daemon shutdown completed", "device_id", deviceID)
}

// runSessionDiscoveryLoop waits the full interval after each completed scan.
// A slow native-store scan therefore cannot build ticker debt and catch up in
// a tight loop. Force requests remain buffered and run immediately afterward.
func runSessionDiscoveryLoop(
	ctx context.Context,
	interval time.Duration,
	force <-chan struct{},
	discover func(bool),
) {
	discover(true)
	for {
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			discover(false)
		case <-force:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			discover(true)
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
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

func pendingApprovalChanged(oldS, newS *adapters.SessionInfo) bool {
	if oldS == nil || newS == nil {
		return oldS != newS
	}
	o, n := oldS.PendingApproval, newS.PendingApproval
	if o == nil && n == nil {
		return false
	}
	if o == nil || n == nil {
		return true
	}
	return o.ID != n.ID || o.ToolName != n.ToolName || o.Description != n.Description
}

func pendingUserInputChanged(oldS, newS *adapters.SessionInfo) bool {
	if oldS == nil || newS == nil {
		return oldS != newS
	}
	oldInput, newInput := oldS.PendingUserInput, newS.PendingUserInput
	if oldInput == nil || newInput == nil {
		return oldInput != newInput
	}
	return oldInput.RequestID != newInput.RequestID || oldInput.ExpiresAt != newInput.ExpiresAt ||
		!reflect.DeepEqual(oldInput.Questions, newInput.Questions)
}

func sessionCapabilitiesChanged(oldS, newS *adapters.SessionInfo) bool {
	if oldS == nil || newS == nil {
		return oldS != newS
	}
	oldCaps, newCaps := oldS.Capabilities, newS.Capabilities
	if oldCaps == nil || newCaps == nil {
		return oldCaps != newCaps
	}
	return oldCaps.ControlMode != newCaps.ControlMode ||
		oldCaps.Send != newCaps.Send ||
		oldCaps.Approve != newCaps.Approve ||
		oldCaps.Deny != newCaps.Deny ||
		oldCaps.Interrupt != newCaps.Interrupt ||
		oldCaps.Steer != newCaps.Steer ||
		oldCaps.Queue != newCaps.Queue ||
		oldCaps.Spawn != newCaps.Spawn ||
		oldCaps.UserInput != newCaps.UserInput ||
		oldCaps.AttachmentMode != newCaps.AttachmentMode ||
		oldCaps.ControlPath != newCaps.ControlPath ||
		oldCaps.ControlVersion != newCaps.ControlVersion ||
		!reflect.DeepEqual(oldCaps.UnavailableReasons, newCaps.UnavailableReasons)
}

func mergeCapabilityReasons(current, additions map[string]string) map[string]string {
	merged := make(map[string]string, len(current)+len(additions))
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range additions {
		merged[key] = value
	}
	return merged
}

func removeCapabilityReasons(current map[string]string, keys ...string) map[string]string {
	clean := mergeCapabilityReasons(current, nil)
	for _, key := range keys {
		delete(clean, key)
	}
	return clean
}

func completeCapabilityReasons(caps *adapters.SessionCapabilities) map[string]string {
	reasons := mergeCapabilityReasons(caps.UnavailableReasons, nil)
	set := func(key, fallback string, enabled bool) {
		if enabled {
			delete(reasons, key)
			return
		}
		if strings.TrimSpace(reasons[key]) == "" {
			reasons[key] = fallback
		}
	}
	set("send", "unsupported_by_control_path", caps.Send)
	set("approve", "unsupported_by_control_path", caps.Approve)
	set("deny", "unsupported_by_control_path", caps.Deny)
	set("interrupt", "unsupported_by_control_path", caps.Interrupt)
	set("steer", "unsupported_by_agent", caps.Steer)
	set("queue", "queue_not_available", caps.Queue)
	set("spawn", "start_not_available", caps.Spawn)
	set("user_input", "user_input_not_available", caps.UserInput)
	set("attachment", "attachment_not_available", caps.AttachmentMode != adapters.AttachUnsupported)
	return reasons
}

// sessionsToWire converts internal discovery models into the public protocol shape
// expected by the server/PWA (unix last_activity, always-set device_id).
func sessionsToWire(deviceID string, sessions []*adapters.SessionInfo) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		// Normalize status: unknown values collapse to idle (forward-compatible).
		status := string(s.Status)
		switch s.Status {
		case adapters.StatusRunning, adapters.StatusIdle, adapters.StatusWaitingUser,
			adapters.StatusWaitingApproval, adapters.StatusError:
		default:
			if status == "" {
				status = string(adapters.StatusIdle)
			}
		}
		caps := s.Capabilities
		if caps == nil {
			caps = adapters.DefaultCapabilities(s.AgentType)
		}
		reasons := completeCapabilityReasons(caps)
		item := map[string]interface{}{
			"id":            s.ID,
			"device_id":     deviceID,
			"agent_type":    string(s.AgentType),
			"status":        status,
			"summary":       s.Summary,
			"last_activity": s.LastActivity.Unix(),
			"capabilities": map[string]interface{}{
				"control_mode":        string(caps.ControlMode),
				"send":                caps.Send,
				"approve":             caps.Approve,
				"deny":                caps.Deny,
				"interrupt":           caps.Interrupt,
				"steer":               caps.Steer,
				"queue":               caps.Queue,
				"spawn":               caps.Spawn,
				"user_input":          caps.UserInput,
				"attachment_mode":     string(caps.AttachmentMode),
				"control_path":        caps.ControlPath,
				"control_version":     caps.ControlVersion,
				"unavailable_reasons": reasons,
			},
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
		if s.PendingUserInput != nil {
			item["pending_user_input"] = s.PendingUserInput
		}
		if s.ActiveTurn != nil {
			item["active_turn"] = s.ActiveTurn
		}
		out = append(out, item)
	}
	return out
}

// sendSessionMessage creates a session_message and sends it to the server.
// msgID, when non-empty, is a stable id so the phone can patch streaming content.
// In sealed mode the message body is encrypted under the session content key.
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

	ts := time.Now().Unix()
	protocolVersion := client.ProtocolVersion()
	inner := map[string]interface{}{
		"message": map[string]interface{}{
			"id":        msgID,
			"role":      role,
			"content":   content,
			"type":      msgType,
			"timestamp": ts,
			"metadata": map[string]interface{}{
				"agent_type": agentType,
				"stream":     true,
			},
		},
	}

	msg := map[string]interface{}{
		"protocol_version": protocolVersion,
		"transport_mode":   daemonTransport,
		"type":             "session_message",
		"device_id":        deviceID,
		"session_id":       sessionID,
		"timestamp":        ts,
	}

	if daemonTransport == "sealed" && daemonSealedKeys != nil {
		plain, err := json.Marshal(inner)
		if err != nil {
			opslog.Error("daemon.main", "session_message_marshal_failed", "session message payload marshal failed", err, "session_id", sessionID, "agent_type", agentType)
			return
		}
		aad := sealed.AADFields{
			ProtocolVersion: protocolVersion,
			TransportMode:   "sealed",
			Type:            "session_message",
			DeviceID:        deviceID,
			SessionID:       sessionID,
			Timestamp:       ts,
		}
		wire, err := daemonSealedKeys.SealSession(sessionID, deviceID, "phones", aad, plain)
		if err != nil {
			opslog.Error("daemon.main", "session_message_seal_failed", "session message sealing failed", err, "session_id", sessionID, "agent_type", agentType)
			return
		}
		msg["sealed_payload"] = wire
	} else {
		msg["payload"] = inner
	}

	if err := client.Send(msg); err != nil {
		opslog.Error("daemon.main", "session_message_send_failed", "session message send failed", err, "session_id", sessionID, "agent_type", agentType)
	}
}

func parseUserInputAnswers(raw interface{}) map[string][]string {
	out := make(map[string][]string)
	answers, ok := raw.(map[string]interface{})
	if !ok {
		return out
	}
	for questionID, rawAnswer := range answers {
		questionID = strings.TrimSpace(questionID)
		if questionID == "" {
			continue
		}
		var values []interface{}
		switch typed := rawAnswer.(type) {
		case []interface{}:
			values = typed
		case map[string]interface{}:
			values, _ = typed["answers"].([]interface{})
		}
		for _, value := range values {
			if text, ok := value.(string); ok {
				out[questionID] = append(out[questionID], text)
			}
		}
		if out[questionID] == nil {
			out[questionID] = []string{}
		}
	}
	return out
}

// sendControlEvent publishes details immediately while attention_event remains
// routing-only and safe for generic Web Push.
func sendControlEvent(client *connection.Client, deviceID string, event adapters.ControlEvent) {
	if client == nil || event.SessionID == "" {
		return
	}
	sendSessionUpdate, sendAttention := controlEventWirePlan(event)
	if !sendSessionUpdate && !sendAttention {
		return
	}
	// session_update is intentionally partial and the relay cannot merge sealed
	// payloads. Force an authoritative catalog snapshot after every state update
	// so a reconnect observes the exact active-turn generation (or its clear).
	if sendSessionUpdate {
		defer requestForceDiscover()
	}
	ts := time.Now().Unix()
	protocolVersion := client.ProtocolVersion()
	agentType := string(event.AgentType)
	if agentType == "" {
		agentType = "codex"
	}
	if sendSessionUpdate {
		session := map[string]interface{}{
			"id": event.SessionID, "device_id": deviceID, "agent_type": agentType,
			"last_activity": ts,
		}
		if event.Status != "" {
			session["status"] = string(event.Status)
		}
		if event.PendingApproval != nil {
			session["pending_approval"] = event.PendingApproval
		}
		if event.PendingUserInput != nil {
			session["pending_user_input"] = event.PendingUserInput
		}
		if event.Capabilities != nil {
			session["capabilities"] = event.Capabilities
		}
		if event.ActiveTurn != nil {
			session["active_turn"] = event.ActiveTurn
		} else if event.ClearActiveTurn {
			session["active_turn"] = nil
		}
		inner := map[string]interface{}{"session": session}
		msg := map[string]interface{}{
			"protocol_version": protocolVersion, "transport_mode": daemonTransport,
			"type": "session_update", "device_id": deviceID,
			"session_id": event.SessionID, "timestamp": ts,
		}
		if daemonTransport == "sealed" {
			if daemonSealedKeys == nil {
				opslog.Error("daemon.main", "session_update_sealed_keys_unavailable", "session update refused because sealed keys are unavailable", nil, "session_id", event.SessionID, "agent_type", agentType)
				return
			}
			plain, err := json.Marshal(inner)
			if err != nil {
				return
			}
			aad := sealed.AADFields{
				ProtocolVersion: protocolVersion, TransportMode: "sealed", Type: "session_update",
				DeviceID: deviceID, SessionID: event.SessionID, Timestamp: ts,
			}
			sealedPayload, err := daemonSealedKeys.SealSession(event.SessionID, deviceID, "phones", aad, plain)
			if err != nil {
				opslog.Error("daemon.main", "session_update_seal_failed", "session update sealing failed", err, "session_id", event.SessionID, "agent_type", agentType)
				return
			}
			msg["sealed_payload"] = sealedPayload
		} else {
			msg["payload"] = inner
		}
		if err := client.Send(msg); err != nil {
			opslog.Error("daemon.main", "session_update_send_failed", "session update send failed", err, "session_id", event.SessionID, "agent_type", agentType)
		}
	}
	if !sendAttention {
		return
	}
	attention := map[string]interface{}{
		"protocol_version": protocolVersion, "transport_mode": daemonTransport,
		"type": "attention_event", "device_id": deviceID,
		"session_id": event.SessionID, "timestamp": ts,
		"payload": map[string]interface{}{
			"event_id": event.EventID, "class": event.Class, "occurred_at": ts,
		},
	}
	if err := client.Send(attention); err != nil {
		opslog.Error("daemon.main", "attention_event_send_failed", "attention event send failed", err, "session_id", event.SessionID, "agent_type", agentType)
	}
}

func controlEventWirePlan(event adapters.ControlEvent) (sessionUpdate, attention bool) {
	sessionUpdate = event.Status != "" || event.PendingApproval != nil || event.PendingUserInput != nil || event.Capabilities != nil || event.ActiveTurn != nil || event.ClearActiveTurn
	attention = event.EventID != "" && event.Class != ""
	return sessionUpdate, attention
}

func sendUserInputResult(client *connection.Client, deviceID, sessionID, requestID, status, message string) {
	payload := map[string]interface{}{"request_id": requestID, "status": status}
	if message != "" {
		payload["message"] = message
	}
	sendDaemonApplication(client, deviceID, sessionID, "", "user_input_result", payload, false)
}

func buildDaemonApplicationMessage(
	protocolVersion string,
	deviceID, sessionID, clientMsgID, msgType string,
	payload map[string]interface{},
	useCatalog bool,
) (map[string]interface{}, error) {
	ts := time.Now().Unix()
	msg := map[string]interface{}{
		"protocol_version": protocolVersion, "transport_mode": daemonTransport,
		"type": msgType, "device_id": deviceID, "timestamp": ts,
	}
	if sessionID != "" {
		msg["session_id"] = sessionID
	}
	if clientMsgID != "" {
		msg["client_msg_id"] = clientMsgID
	}
	if daemonTransport == "open" {
		msg["payload"] = payload
		return msg, nil
	}
	if daemonSealedKeys == nil {
		return nil, errors.New("sealed keys unavailable")
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	aad := sealed.AADFields{
		ProtocolVersion: protocolVersion, TransportMode: "sealed", Type: msgType,
		DeviceID: deviceID, SessionID: sessionID, ClientMsgID: clientMsgID, Timestamp: ts,
	}
	var wire *sealed.WireSealed
	if useCatalog {
		wire, err = daemonSealedKeys.SealCatalog(deviceID, "phones", aad, plain)
	} else {
		wire, err = daemonSealedKeys.SealSession(sessionID, deviceID, "phones", aad, plain)
	}
	if err != nil {
		return nil, err
	}
	msg["sealed_payload"] = wire
	return msg, nil
}

func sendDaemonApplication(
	client *connection.Client,
	deviceID, sessionID, clientMsgID, msgType string,
	payload map[string]interface{},
	useCatalog bool,
) {
	msg, err := buildDaemonApplicationMessage(client.ProtocolVersion(), deviceID, sessionID, clientMsgID, msgType, payload, useCatalog)
	if err != nil {
		opslog.Error("daemon.main", "application_message_build_failed", "application message build failed", err, "device_id", deviceID, "session_id", sessionID, "client_msg_id", clientMsgID, "message_type", msgType)
		return
	}
	if err := client.Send(msg); err != nil {
		opslog.Error("daemon.main", "application_message_send_failed", "application message send failed", err, "device_id", deviceID, "session_id", sessionID, "client_msg_id", clientMsgID, "message_type", msgType)
	}
}

func commandFailure(event, message string, err error, attrs ...any) int {
	opslog.Error("daemon.main", event, message, err, attrs...)
	return 1
}

// handleRegistration registers this device with the server via REST and saves config.
func handleRegistration(deviceName string) int {
	fmt.Println("🐱 NekoNest Device Registration")
	fmt.Println("================================")

	if deviceName == "" {
		hostname, _ := os.Hostname()
		deviceName = hostname
	}

	// Already registered?
	if existing, err := config.Load(); err == nil && existing.Token != "" && existing.DeviceID != "" {
		if _, modeErr := registrationTransportMode(existing.TransportMode, os.Getenv("NEKONEST_TRANSPORT_MODE")); modeErr != nil {
			return commandFailure("registration_transport_mode_invalid", "registered transport mode is invalid", modeErr, "device_id", existing.DeviceID)
		}
		fmt.Printf("Already registered.\n")
		fmt.Printf("  Device: %s\n", existing.DeviceID)
		fmt.Printf("  Server: %s\n", existing.ServerURL)
		fmt.Printf("Config:  %s\n", config.DefaultConfigPath())
		fmt.Println("To re-register, delete the config file first.")
		// Still mint a fresh pair code for the phone
		_, st, _ := identity.LoadOrCreate(identity.Path())
		if code, exp, err := requestPairCode(existing, st); err == nil {
			fmt.Printf("\n📱 Phone pair code: %s (expires ~%s)\n", code, exp.Local().Format("15:04:05"))
			if st != nil {
				fmt.Printf("Fingerprint: %s\n", st.Fingerprint)
			}
			fmt.Println("Enter this code in the PWA 「配对电脑」 page.")
		}
		return 0
	}

	serverURL := os.Getenv("NEKONEST_SERVER")
	if serverURL == "" {
		fmt.Println("Set NEKONEST_SERVER first, e.g.:")
		fmt.Println(`  set NEKONEST_SERVER=https://nekonest.example.com`)
		fmt.Println(`  $env:NEKONEST_SERVER="https://nekonest.example.com"  # PowerShell`)
		return commandFailure("registration_server_missing", "registration server environment is missing", nil)
	}

	httpBase := config.HTTPBaseURL(serverURL)
	wsURL := config.NormalizeServerURL(serverURL)

	_, st, err := identity.LoadOrCreate(identity.Path())
	if err != nil {
		return commandFailure("registration_identity_failed", "registration identity load failed", err)
	}

	body, _ := json.Marshal(map[string]string{
		"name":                 deviceName,
		"os":                   runtime.GOOS,
		"ed25519_public":       st.Ed25519Public,
		"x25519_public":        st.X25519Public,
		"identity_fingerprint": st.Fingerprint,
		"transport_mode":       strings.TrimSpace(os.Getenv("NEKONEST_TRANSPORT_MODE")),
	})
	req, err := http.NewRequest(http.MethodPost, httpBase+"/api/devices/register", bytes.NewReader(body))
	if err != nil {
		return commandFailure("registration_request_build_failed", "registration request build failed", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bootstrap := os.Getenv("NEKONEST_BOOTSTRAP_TOKEN"); bootstrap != "" {
		req.Header.Set("X-Neko-Bootstrap", bootstrap)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return commandFailure("registration_request_failed", "registration request failed", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return commandFailure("registration_rejected", "registration was rejected by server", nil, "status", resp.StatusCode)
	}

	var result struct {
		DeviceID      string `json:"device_id"`
		Token         string `json:"token"`
		Name          string `json:"name"`
		TransportMode string `json:"transport_mode"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return commandFailure("registration_response_invalid", "registration response could not be parsed", err, "status", resp.StatusCode)
	}
	mode, modeErr := registrationTransportMode(result.TransportMode, os.Getenv("NEKONEST_TRANSPORT_MODE"))
	if modeErr != nil {
		return commandFailure("registration_response_transport_invalid", "registration response transport mode is invalid", modeErr, "status", resp.StatusCode)
	}
	if result.DeviceID == "" || result.Token == "" {
		return commandFailure("registration_response_credentials_missing", "registration response is missing credentials", nil, "status", resp.StatusCode)
	}

	cfg := &config.Config{
		ServerURL:     wsURL,
		DeviceID:      result.DeviceID,
		Token:         result.Token,
		TransportMode: mode,
	}
	if err := cfg.Save(); err != nil {
		return commandFailure("registration_config_save_failed", "registered configuration could not be saved", err, "device_id", result.DeviceID)
	}

	fmt.Printf("✅ Registered as %s (%s)\n", result.Name, result.DeviceID)
	fmt.Printf("   Server: %s\n", wsURL)
	fmt.Printf("   Config: %s\n", config.DefaultConfigPath())
	fmt.Printf("   Fingerprint: %s\n", st.Fingerprint)

	if code, exp, err := requestPairCode(cfg, st); err != nil {
		opslog.Error("daemon.main", "registration_pair_code_failed", "registration succeeded but pair code generation failed", err, "device_id", result.DeviceID)
		fmt.Println("\n⚠️  Could not generate pair code; check the daemon operational log.")
		fmt.Println("You can still list the device on phone if you use the phone secret.")
	} else {
		fmt.Printf("\n📱 Phone pair code: %s (expires ~%s)\n", code, exp.Local().Format("15:04:05"))
		qr := identity.BuildPairQR(httpBase, result.DeviceID, result.Name, code, exp.Unix(), st, mode)
		qrJSON, _ := json.Marshal(qr)
		fmt.Println(string(qrJSON))
		fmt.Println("1. Open PWA → 配对电脑 → enter code / paste QR payload")
		fmt.Println("2. Start daemon: nekonest-daemon.exe")
	}
	return 0
}

// registrationTransportMode rejects incomplete registration responses before
// credentials are persisted. The server selects a nest-wide immutable mode;
// an optional environment request is only a first-registration assertion.
func registrationTransportMode(serverMode, requested string) (string, error) {
	if strings.TrimSpace(serverMode) == "" {
		return "", errors.New("missing transport_mode")
	}
	mode, err := config.NormalizeTransportMode(serverMode)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(requested) == "" {
		return mode, nil
	}
	want, err := config.NormalizeTransportMode(requested)
	if err != nil {
		return "", err
	}
	if want != mode {
		return "", fmt.Errorf("requested %s, server returned %s", want, mode)
	}
	return mode, nil
}

// handlePairing generates a short-lived pair code for an already-registered device.
// Usage: nekonest-daemon -pair gen   (or any non-empty value; code is minted server-side)
func handlePairing(code string) int {
	fmt.Println("🐱 NekoNest Pair Code")
	fmt.Println("=====================")

	cfg, err := config.Load()
	if err != nil {
		fmt.Println("Device not registered yet. Run with -register first.")
		return commandFailure("pairing_config_load_failed", "pairing configuration could not be loaded", err)
	}

	// Historical flag took a code string; we now always mint a server code for the phone.
	_ = code
	_, st, err := identity.LoadOrCreate(identity.Path())
	if err != nil {
		return commandFailure("pairing_identity_failed", "pairing identity load failed", err, "device_id", cfg.DeviceID)
	}
	pairCode, exp, err := requestPairCode(cfg, st)
	if err != nil {
		return commandFailure("pairing_code_failed", "pair code generation failed", err, "device_id", cfg.DeviceID)
	}
	httpBase := config.HTTPBaseURL(cfg.ServerURL)
	qr := identity.BuildPairQR(httpBase, cfg.DeviceID, "", pairCode, exp.Unix(), st, cfg.TransportMode)
	qrJSON, _ := json.Marshal(qr)
	fmt.Printf("Device: %s\n", cfg.DeviceID)
	fmt.Printf("Fingerprint: %s\n", st.Fingerprint)
	fmt.Printf("📱 Phone pair code: %s\n", pairCode)
	fmt.Printf("Expires: %s\n", exp.Local().Format(time.RFC3339))
	fmt.Println()
	fmt.Println("QR payload (scan or paste into PWA):")
	fmt.Println(string(qrJSON))
	fmt.Println()
	fmt.Println("Enter the 6-char code in the PWA, and verify the fingerprint matches the PC screen.")
	return 0
}

// requestPairCode calls POST /api/pair/generate for the registered device.
func requestPairCode(cfg *config.Config, st *identity.Stored) (string, time.Time, error) {
	httpBase := config.HTTPBaseURL(cfg.ServerURL)
	bodyMap := map[string]string{
		"device_id": cfg.DeviceID,
		"token":     cfg.Token,
	}
	if st != nil {
		bodyMap["ed25519_public"] = st.Ed25519Public
		bodyMap["x25519_public"] = st.X25519Public
		bodyMap["identity_fingerprint"] = st.Fingerprint
	}
	body, _ := json.Marshal(bodyMap)
	resp, err := http.Post(httpBase+"/api/pair/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("pair code request returned status %d", resp.StatusCode)
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

func queuedPromptPayload(deviceID, sessionID string, item promptQueueItem) (string, []attach.Ref, error) {
	if len(item.SealedEnvelope) > 0 {
		var wire map[string]interface{}
		if err := json.Unmarshal(item.SealedEnvelope, &wire); err != nil {
			return "", nil, fmt.Errorf("decode saved sealed prompt: %w", err)
		}
		sealedObj, ok := wire["sealed_payload"].(map[string]interface{})
		if !ok || daemonSealedKeys == nil {
			return "", nil, errors.New("saved sealed prompt cannot be opened")
		}
		payload, err := openSealedPrompt(deviceID, sessionID, wire, sealedObj)
		if err != nil {
			return "", nil, err
		}
		return queuedPromptPayloadFromMap(payload)
	}
	var raw interface{}
	if len(item.Attachments) > 0 && json.Unmarshal(item.Attachments, &raw) != nil {
		return "", nil, errors.New("saved prompt attachments are invalid")
	}
	refs := parseAttachmentRefs(raw)
	if len(refs) > maxPromptAttachments {
		return "", nil, fmt.Errorf("too many attachments (limit %d)", maxPromptAttachments)
	}
	return stripNekoAttachSuffix(item.Prompt), refs, nil
}

func queuedPromptPayloadFromMap(payload map[string]interface{}) (string, []attach.Ref, error) {
	if payload == nil {
		return "", nil, errors.New("saved prompt payload is empty")
	}
	prompt, _ := payload["prompt"].(string)
	refs := parseAttachmentRefs(payload["attachments"])
	if len(refs) > maxPromptAttachments {
		return "", nil, fmt.Errorf("too many attachments (limit %d)", maxPromptAttachments)
	}
	if prompt == "" && len(refs) == 0 {
		return "", nil, errors.New("saved prompt is empty")
	}
	return stripNekoAttachSuffix(prompt), refs, nil
}

// dispatchQueuedPrompt is called only after the queue persisted running. It
// records dispatching immediately before the native RPC and accepts only after
// that RPC positively returns; no exec-resume fallback can cross this boundary.
func dispatchQueuedPrompt(
	target adapters.Adapter,
	journal *promptJournal,
	accepted *promptAcceptanceCache,
	client *connection.Client,
	deviceID string,
	item promptQueueItem,
	prompt string,
	files []attach.LocalFile,
	attachmentDir string,
	generation uint64,
	activeTurns *activeTurnRegistry,
	handleControlEvent func(adapters.ControlEvent),
) (bool, error) {
	cleanup := func() {
		if attachmentDir != "" {
			if err := os.RemoveAll(attachmentDir); err != nil {
				opslog.Error("daemon.main", "queued_attachment_cleanup_failed", "queued prompt attachment cleanup failed", err, "session_id", item.SessionID, "client_msg_id", item.ClientMsgID)
			}
		}
	}
	if err := journal.markDispatching(item.SessionID, item.ClientMsgID, item.Prompt); err != nil {
		cleanup()
		return false, fmt.Errorf("persist queued dispatch: %w", err)
	}
	if activeTurns == nil || !activeTurns.bind(item.SessionID, generation, item.ClientMsgID) {
		cleanup()
		if journalErr := journal.rollbackDispatching(item.SessionID, item.ClientMsgID); journalErr != nil {
			return false, fmt.Errorf("active turn conflict and journal rollback failed: %w", journalErr)
		}
		return false, agentexec.ErrSessionBusy
	}
	var completeOnce sync.Once
	onComplete := func() {
		completeOnce.Do(func() {
			cleanup()
			if activeTurns.completeMatching(item.SessionID, generation, item.ClientMsgID) {
				sendControlEvent(client, deviceID, adapters.ControlEvent{
					SessionID: item.SessionID, AgentType: adapters.AgentType(item.AgentType), ClearActiveTurn: true,
				})
			}
		})
	}
	request := adapters.PromptRequest{
		Prompt: prompt, Attachments: files, OnComplete: onComplete,
		Generation: generation, ClientMsgID: item.ClientMsgID, DeferAcceptance: true,
		OnNativeBound: func(nativeRequestID string) {
			activeTurns.setNativeRequestID(item.SessionID, generation, item.ClientMsgID, nativeRequestID)
		},
	}
	var sendErr error
	if codex, ok := target.(*adapters.CodexAdapter); ok {
		request.DeferAcceptance = false // Codex app-server owns its proven event ordering.
		sendErr = codex.SendQueuedPrompt(item.SessionID, request)
	} else {
		sendErr = target.SendPrompt(item.SessionID, request)
	}
	if sendErr != nil {
		activeTurns.clearMatching(item.SessionID, generation, item.ClientMsgID, "")
		if errors.Is(sendErr, agentexec.ErrSessionBusy) {
			cleanup()
			if journalErr := journal.rollbackDispatching(item.SessionID, item.ClientMsgID); journalErr != nil {
				return false, fmt.Errorf("pre-boundary busy but journal rollback failed: %w", journalErr)
			}
			return false, sendErr
		}
		if journalErr := journal.markIndeterminate(item.SessionID, item.ClientMsgID); journalErr != nil {
			opslog.Error("daemon.main", "queued_prompt_indeterminate_persist_failed", "queued prompt indeterminate state could not be persisted", journalErr, "session_id", item.SessionID, "client_msg_id", item.ClientMsgID, "agent_type", item.AgentType, "generation", generation)
		}
		// A non-busy controller error cannot prove that the native boundary was
		// not crossed. Retain attachments and fail closed.
		return true, sendErr
	}
	if binding, ok := activeTurns.current(item.SessionID); ok {
		sendControlEvent(client, deviceID, adapters.ControlEvent{
			SessionID: item.SessionID, AgentType: adapters.AgentType(item.AgentType),
			Status: adapters.StatusRunning, ActiveTurn: binding,
		})
	}
	if err := journal.markAccepted(item.SessionID, item.ClientMsgID); err != nil {
		if acker, ok := target.(adapters.PromptAcceptanceAcker); ok {
			acker.AbandonPrompt(item.SessionID, generation)
		}
		if activeTurns.abandonAcceptance(item.SessionID, generation, item.ClientMsgID) {
			sendControlEvent(client, deviceID, adapters.ControlEvent{
				SessionID: item.SessionID, AgentType: adapters.AgentType(item.AgentType), ClearActiveTurn: true,
			})
		}
		return true, fmt.Errorf("persist queued acceptance: %w", err)
	}
	pendingTerminal, completed := activeTurns.accept(item.SessionID, generation, item.ClientMsgID)
	if completed {
		sendControlEvent(client, deviceID, adapters.ControlEvent{
			SessionID: item.SessionID, AgentType: adapters.AgentType(item.AgentType), ClearActiveTurn: true,
		})
	}
	if pendingTerminal != nil && handleControlEvent != nil {
		handleControlEvent(*pendingTerminal)
	}
	if acker, ok := target.(adapters.PromptAcceptanceAcker); ok {
		acker.AcknowledgePrompt(item.SessionID, generation)
	}
	acceptedPrompt := acceptedPrompt{prompt: boundedPromptEcho(item.Prompt)}
	accepted.add(item.SessionID, item.ClientMsgID, acceptedPrompt)
	sendPromptAccepted(client, deviceID, item.SessionID, item.ClientMsgID, acceptedPrompt)
	return true, nil
}

func sendPromptQueued(client *connection.Client, deviceID, sessionID, clientMsgID string, position int) {
	sendDaemonApplication(client, deviceID, sessionID, clientMsgID, "prompt_queued", map[string]interface{}{
		"client_msg_id": clientMsgID, "queued": true, "queue_position": position,
	}, false)
}

func sendPromptCancelled(client *connection.Client, deviceID, sessionID, clientMsgID string) {
	sendDaemonApplication(client, deviceID, sessionID, clientMsgID, "prompt_cancelled", map[string]interface{}{
		"client_msg_id": clientMsgID,
	}, false)
}

func sendQueueUpdate(client *connection.Client, deviceID, sessionID string, queue *promptQueue) {
	if client == nil || queue == nil {
		return
	}
	items := queue.list(sessionID)
	wireItems := make([]map[string]interface{}, 0, len(items))
	paused := false
	for _, item := range items {
		if isPromptQueueTerminal(item.Status) {
			continue
		}
		if isPromptQueueBlocked(item.Status) {
			paused = true
		}
		status := string(item.Status)
		wireItems = append(wireItems, map[string]interface{}{
			"client_msg_id": item.ClientMsgID, "position": queue.position(sessionID, item.ClientMsgID), "status": status,
		})
	}
	sendDaemonApplication(client, deviceID, sessionID, "", "queue_update", map[string]interface{}{
		"session_id": sessionID, "paused": paused, "items": wireItems,
	}, false)
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
	msg, err := buildDaemonApplicationMessage(client.ProtocolVersion(), deviceID, sessionID, clientMsgID, "prompt_accepted", payload, false)
	if err != nil {
		return err
	}
	if err := client.Send(msg); err != nil {
		// The accepted ID remains cached. A server retransmission will only
		// re-send this acknowledgement and will not execute the prompt again.
		opslog.Error("daemon.main", "prompt_acceptance_send_failed", "prompt acceptance acknowledgement send failed", err, "device_id", deviceID, "session_id", sessionID, "client_msg_id", clientMsgID)
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
		"protocol_version": client.ProtocolVersion(),
		"transport_mode":   daemonTransport,
		"type":             "prompt_not_seen",
		"device_id":        deviceID,
		"session_id":       sessionID,
		"client_msg_id":    clientMsgID,
		"timestamp":        time.Now().Unix(),
		"payload": map[string]interface{}{
			"client_msg_id": clientMsgID,
		},
	}
	if err := client.Send(msg); err != nil {
		opslog.Error("daemon.main", "prompt_not_seen_send_failed", "prompt not-seen acknowledgement send failed", err, "device_id", deviceID, "session_id", sessionID, "client_msg_id", clientMsgID)
	}
}

func sendPromptFailure(
	client *connection.Client,
	deviceID, sessionID, clientMsgID, message, outcome string,
	retryAllowed bool,
) {
	msg, err := buildDaemonApplicationMessage(client.ProtocolVersion(), deviceID, sessionID, clientMsgID, "prompt_failed", map[string]interface{}{
		"client_msg_id": clientMsgID,
		"error":         message,
		"message":       message,
		"outcome":       outcome,
		"retry_allowed": retryAllowed,
	}, false)
	if err != nil {
		opslog.Error("daemon.main", "prompt_failure_build_failed", "prompt failure acknowledgement build failed", err, "device_id", deviceID, "session_id", sessionID, "client_msg_id", clientMsgID, "status", outcome)
		return
	}
	// Delivery state is relay-visible event metadata; application error text
	// remains inside sealed_payload.
	msg["outcome"] = outcome
	msg["retry_allowed"] = retryAllowed
	if err := client.Send(msg); err != nil {
		opslog.Error("daemon.main", "prompt_failure_send_failed", "prompt failure acknowledgement send failed", err, "device_id", deviceID, "session_id", sessionID, "client_msg_id", clientMsgID, "status", outcome)
	}
}

func sendThreadResult(client *connection.Client, deviceID, agentType, opID, msgType, threadID string, promptAccepted bool, errMsg string) {
	msg := buildThreadResultMessage(client.ProtocolVersion(), deviceID, agentType, opID, msgType, threadID, promptAccepted, errMsg)
	if err := client.Send(msg); err != nil {
		opslog.Error("daemon.main", "thread_result_send_failed", "thread start result send failed", err, "device_id", deviceID, "session_id", threadID, "operation_id", opID, "agent_type", agentType, "status", msgType)
	}
}

func buildThreadResultMessage(protocolVersion, deviceID, agentType, opID, msgType, threadID string, promptAccepted bool, errMsg string) map[string]interface{} {
	ts := time.Now().Unix()
	payload := map[string]interface{}{
		"agent_type":      agentType,
		"operation_id":    opID,
		"state":           msgType,
		"prompt_accepted": promptAccepted,
	}
	if threadID != "" {
		payload["session_id"] = threadID
		payload["thread_id"] = threadID
	}
	if errMsg != "" {
		payload["error"] = errMsg
		payload["message"] = errMsg
		payload["reason"] = errMsg
	}
	msg := map[string]interface{}{
		"protocol_version": protocolVersion,
		"transport_mode":   daemonTransport,
		"type":             msgType,
		"device_id":        deviceID,
		"session_id":       threadID,
		"client_msg_id":    opID,
		"timestamp":        ts,
	}
	if daemonTransport == "sealed" {
		if daemonSealedKeys == nil {
			opslog.Error("daemon.main", "thread_result_sealed_keys_unavailable", "thread start result could not be sealed", nil, "device_id", deviceID, "session_id", threadID, "operation_id", opID, "agent_type", agentType, "status", msgType)
			degradeSealedThreadResult(msg)
		} else {
			plain, err := json.Marshal(payload)
			if err != nil {
				opslog.Error("daemon.main", "thread_result_marshal_failed", "thread start result payload marshal failed", err, "device_id", deviceID, "session_id", threadID, "operation_id", opID, "agent_type", agentType, "status", msgType)
				degradeSealedThreadResult(msg)
			} else {
				aad := sealed.AADFields{
					ProtocolVersion: protocolVersion,
					TransportMode:   "sealed",
					Type:            msgType,
					DeviceID:        deviceID,
					SessionID:       threadID,
					ClientMsgID:     opID,
					Timestamp:       ts,
				}
				wire, err := daemonSealedKeys.SealCatalog(deviceID, "phones", aad, plain)
				if err != nil {
					opslog.Error("daemon.main", "thread_result_seal_failed", "thread start result sealing failed", err, "device_id", deviceID, "session_id", threadID, "operation_id", opID, "agent_type", agentType, "status", msgType)
					degradeSealedThreadResult(msg)
				} else {
					msg["sealed_payload"] = wire
				}
			}
		}
	} else {
		msg["payload"] = payload
	}
	return msg
}

func degradeSealedThreadResult(msg map[string]interface{}) {
	// A visible outer state is routing metadata, not an authenticated business
	// result. If sealing fails, expose only the operation correlation and force
	// the phone down the fail-closed recovery path.
	msg["type"] = "thread_indeterminate"
	delete(msg, "session_id")
	delete(msg, "payload")
	delete(msg, "sealed_payload")
}

func daemonInboundApplicationType(msgType string) bool {
	switch msgType {
	case "send_prompt", "approve", "deny", "respond_user_input", "interrupt", "steer",
		"cancel_prompt", "resume_prompt_queue", "skip_prompt_queue_item", "start_thread", "fetch_history":
		return true
	default:
		return false
	}
}

func safeInboundMessageTypeForLog(msgType string) string {
	if daemonInboundApplicationType(msgType) {
		return msgType
	}
	switch msgType {
	case "refresh_sessions", "pair_ready", "prompt_status_query", "prompt_committed", "heartbeat",
		"key_package", "phone_revoked", "attention_event", "error":
		return msgType
	default:
		return "unknown"
	}
}

func logEmptyPromptRejected(sessionID, clientMsgID string) {
	opslog.Warn("daemon.main", "prompt_rejected_empty", "empty prompt rejected", "session_id", sessionID, "client_msg_id", clientMsgID)
}

func validateInboundRoutingFrame(msg map[string]interface{}) error {
	if msg == nil {
		return errors.New("message required")
	}
	version, _ := msg["protocol_version"].(string)
	if !strings.HasPrefix(version, "1.") {
		return errors.New("protocol_version mismatch")
	}
	mode, _ := msg["transport_mode"].(string)
	if mode != daemonTransport {
		return fmt.Errorf("transport_mode mismatch: daemon=%s frame=%s", daemonTransport, mode)
	}
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "refresh_sessions", "pair_ready", "prompt_status_query", "prompt_committed", "heartbeat",
		"key_package", "phone_revoked", "attention_event", "error":
		return nil
	default:
		return fmt.Errorf("unexpected routing message type %q", msgType)
	}
}

func decodeInboundApplicationCommand(deviceID, sessionID string, msg map[string]interface{}, expectedType string) (map[string]interface{}, error) {
	if msg == nil {
		return nil, errors.New("message required")
	}
	version, _ := msg["protocol_version"].(string)
	if !strings.HasPrefix(version, "1.") {
		return nil, errors.New("protocol_version mismatch")
	}
	mode, _ := msg["transport_mode"].(string)
	if mode != daemonTransport {
		return nil, fmt.Errorf("transport_mode mismatch: daemon=%s frame=%s", daemonTransport, mode)
	}
	actualType, _ := msg["type"].(string)
	if actualType != expectedType {
		return nil, errors.New("message type mismatch")
	}
	plainRaw, hasPlain := msg["payload"]
	sealedRaw, hasSealed := msg["sealed_payload"]
	if hasPlain && hasSealed {
		return nil, errors.New("payload and sealed_payload are mutually exclusive")
	}
	if daemonTransport == "open" {
		if hasSealed {
			return nil, errors.New("open mode rejects sealed_payload")
		}
		if !hasPlain || plainRaw == nil {
			return map[string]interface{}{}, nil
		}
		payload, ok := plainRaw.(map[string]interface{})
		if !ok {
			return nil, errors.New("payload must be an object")
		}
		return payload, nil
	}
	if !hasSealed || hasPlain {
		return nil, errors.New("sealed application command requires sealed_payload only")
	}
	if daemonSealedKeys == nil {
		return nil, errors.New("sealed keys unavailable")
	}
	sealedObj, ok := sealedRaw.(map[string]interface{})
	if !ok {
		return nil, errors.New("sealed_payload must be an object")
	}
	payload, err := openSealedCommand(deviceID, sessionID, msg, sealedObj, expectedType)
	if err != nil {
		return nil, err
	}
	outerClientMsgID, _ := msg["client_msg_id"].(string)
	if innerClientMsgID, _ := payload["client_msg_id"].(string); innerClientMsgID != "" && innerClientMsgID != outerClientMsgID {
		return nil, errors.New("inner client_msg_id does not match envelope")
	}
	if expectedType == "start_thread" {
		if operationID, _ := payload["operation_id"].(string); operationID != "" && operationID != outerClientMsgID {
			return nil, errors.New("operation_id does not match envelope")
		}
	}
	return payload, nil
}

func sendInboundDecodeFailure(client *connection.Client, deviceID, sessionID, msgType string, msg map[string]interface{}, cause error) {
	clientMsgID, _ := msg["client_msg_id"].(string)
	switch msgType {
	case "send_prompt", "cancel_prompt", "resume_prompt_queue", "skip_prompt_queue_item":
		sendPromptFailed(client, deviceID, sessionID, clientMsgID, "authenticated command could not be decoded")
	case "respond_user_input":
		sendUserInputResult(client, deviceID, sessionID, "", "indeterminate", "authenticated command could not be decoded")
	case "start_thread":
		sendThreadResult(client, deviceID, "", clientMsgID, "thread_failed", "", false, "authenticated command could not be decoded")
	default:
		sendDaemonError(client, deviceID, sessionID, "authenticated command could not be decoded")
	}
	_ = cause
}

// openSealedPrompt decrypts a sealed send_prompt body into a payload map.
func openSealedPrompt(deviceID, sessionID string, msg map[string]interface{}, sealedObj map[string]interface{}) (map[string]interface{}, error) {
	return openSealedCommand(deviceID, sessionID, msg, sealedObj, "send_prompt")
}

func openSealedCommand(deviceID, sessionID string, msg map[string]interface{}, sealedObj map[string]interface{}, messageType string) (map[string]interface{}, error) {
	raw, err := json.Marshal(sealedObj)
	if err != nil {
		return nil, err
	}
	var sealedFrame sealed.WireSealed
	if err := json.Unmarshal(raw, &sealedFrame); err != nil {
		return nil, err
	}
	ts, _ := msg["timestamp"].(float64)
	clientMsgID, _ := msg["client_msg_id"].(string)
	if clientMsgID == "" {
		if p, ok := msg["payload"].(map[string]interface{}); ok {
			clientMsgID, _ = p["client_msg_id"].(string)
		}
	}
	protocolVersion, _ := msg["protocol_version"].(string)
	if protocolVersion == "" {
		return nil, errors.New("protocol_version required for sealed AAD")
	}
	aad := sealed.AADFields{
		ProtocolVersion: protocolVersion,
		TransportMode:   "sealed",
		Type:            messageType,
		DeviceID:        deviceID,
		SessionID:       sessionID,
		ClientMsgID:     clientMsgID,
		KeyScope:        sealedFrame.KeyScope,
		KeyEpoch:        sealedFrame.Epoch,
		SenderID:        sealedFrame.SenderID,
		Sequence:        sealedFrame.Sequence,
		Timestamp:       int64(ts),
	}
	var pt []byte
	if sealedFrame.KeyScope == "session" {
		pt, err = daemonSealedKeys.OpenSession(sessionID, &sealedFrame, aad)
	} else {
		pt, err = daemonSealedKeys.OpenCatalog(&sealedFrame, aad)
	}
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(pt, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// publishCatalogKeyForPhone wraps the device catalog key for a newly paired phone.
func publishCatalogKeyForPhone(cfg *config.Config, payload map[string]interface{}) {
	if cfg == nil || daemonSealedKeys == nil {
		return
	}
	phoneID, _ := payload["phone_id"].(string)
	phoneX, _ := payload["phone_x25519_public"].(string)
	phoneEd, _ := payload["phone_ed25519_public"].(string)
	code, _ := payload["pair_code"].(string)
	if phoneID == "" || phoneX == "" {
		// Fallback: pull grants from server.
		publishCatalogKeysForAllGrants(cfg)
		return
	}
	_, st, err := identity.LoadOrCreate(identity.Path())
	if err != nil || st == nil {
		opslog.Error("daemon.main", "pair_catalog_identity_failed", "paired phone catalog identity load failed", err, "device_id", cfg.DeviceID)
		return
	}
	id, err := identityLoadSealed(st)
	if err != nil {
		opslog.Error("daemon.main", "pair_catalog_sealed_identity_failed", "paired phone sealed identity load failed", err, "device_id", cfg.DeviceID)
		return
	}
	phonePub, err := sealed.ParseX25519Public(phoneX)
	if err != nil {
		opslog.Warn("daemon.main", "pair_catalog_phone_key_rejected", "paired phone public key rejected", "device_id", cfg.DeviceID)
		return
	}
	shared, err := sealed.SharedSecret(id.X25519Private, phonePub)
	if err != nil {
		opslog.Error("daemon.main", "pair_catalog_shared_secret_failed", "paired phone shared secret derivation failed", err, "device_id", cfg.DeviceID)
		return
	}
	transcript := []byte(strings.Join([]string{
		"nekonest-pair-v1",
		code,
		cfg.DeviceID,
		st.Ed25519Public,
		st.X25519Public,
		phoneEd,
		phoneX,
	}, "|"))
	wrap, err := sealed.DerivePairWrappingKey(shared, transcript)
	if err != nil {
		opslog.Error("daemon.main", "pair_catalog_wrap_key_failed", "paired phone wrapping key derivation failed", err, "device_id", cfg.DeviceID)
		return
	}
	epoch, nonce, ct, err := daemonSealedKeys.WrapCatalogForPhone(wrap)
	if err != nil {
		opslog.Error("daemon.main", "pair_catalog_wrap_failed", "paired phone catalog key wrapping failed", err, "device_id", cfg.DeviceID)
		return
	}
	if err := uploadKeyPackage(cfg, phoneID, "device_catalog", "", epoch, nonce, ct); err != nil {
		opslog.Error("daemon.main", "pair_catalog_upload_failed", "paired phone catalog key upload failed", err, "device_id", cfg.DeviceID, "generation", epoch)
		return
	}
	opslog.Info("daemon.main", "pair_catalog_published", "paired phone catalog key package published", "device_id", cfg.DeviceID, "generation", epoch)
}

func identityLoadSealed(st *identity.Stored) (*sealed.Identity, error) {
	// Reuse LoadOrCreate path by reading file — Stored already has pubs.
	edPriv, err := sealed.B64Decode(st.Ed25519Private)
	if err != nil {
		return nil, err
	}
	edPub, err := sealed.ParseEd25519Public(st.Ed25519Public)
	if err != nil {
		return nil, err
	}
	xPrivRaw, err := sealed.B64Decode(st.X25519Private)
	if err != nil {
		return nil, err
	}
	xPub, err := sealed.ParseX25519Public(st.X25519Public)
	if err != nil {
		return nil, err
	}
	var xPriv [32]byte
	copy(xPriv[:], xPrivRaw)
	return &sealed.Identity{
		Ed25519Public: edPub, Ed25519Private: edPriv,
		X25519Public: xPub, X25519Private: xPriv,
	}, nil
}

func uploadKeyPackage(cfg *config.Config, phoneID, scope, sessionID string, epoch uint64, nonce, wrapped string) error {
	httpBase := config.HTTPBaseURL(cfg.ServerURL)
	body, _ := json.Marshal(map[string]any{
		"device_id":   cfg.DeviceID,
		"token":       cfg.Token,
		"phone_id":    phoneID,
		"scope":       scope,
		"session_id":  sessionID,
		"epoch":       epoch,
		"wrapped_key": wrapped,
		"nonce":       nonce,
	})
	resp, err := http.Post(httpBase+"/api/keys/upload", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func publishCatalogKeysForAllGrants(cfg *config.Config) {
	if cfg == nil || daemonSealedKeys == nil {
		return
	}
	httpBase := config.HTTPBaseURL(cfg.ServerURL)
	req, err := http.NewRequest(http.MethodGet, httpBase+"/api/devices/grants?device_id="+cfg.DeviceID, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-Neko-Device-Token", cfg.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Grants []struct {
			PhoneID       string `json:"phone_id"`
			X25519Public  string `json:"x25519_public"`
			Ed25519Public string `json:"ed25519_public"`
		} `json:"grants"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return
	}
	for _, g := range body.Grants {
		if g.X25519Public == "" {
			continue
		}
		publishCatalogKeyForPhone(cfg, map[string]interface{}{
			"phone_id":             g.PhoneID,
			"phone_x25519_public":  g.X25519Public,
			"phone_ed25519_public": g.Ed25519Public,
			"pair_code":            "", // transcript without code won't match phone wrap — skip
		})
	}
}

// sendDaemonError reports an error back to the server (forwarded to phones).
func sendDaemonError(client *connection.Client, deviceID, sessionID, message string) {
	if daemonTransport == "sealed" {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"id":        fmt.Sprintf("error-%d", time.Now().UnixNano()),
				"role":      "assistant",
				"content":   message,
				"type":      "error",
				"timestamp": time.Now().Unix(),
			},
		}
		sendDaemonApplication(client, deviceID, sessionID, "", "session_message", msg, false)
		return
	}
	msg := map[string]interface{}{
		"protocol_version": client.ProtocolVersion(),
		"transport_mode":   daemonTransport,
		"type":             "error",
		"device_id":        deviceID,
		"session_id":       sessionID,
		"timestamp":        time.Now().Unix(),
		"payload":          map[string]interface{}{"message": message},
	}
	if err := client.Send(msg); err != nil {
		opslog.Error("daemon.main", "error_message_send_failed", "daemon error message send failed", err, "device_id", deviceID, "session_id", sessionID)
	}
}

// runDoctor prints non-interactive diagnostics. Exit 0 if config + identity +
// at least one adapter is available and the nest is reachable; otherwise 1.
func runDoctor(configPath string) int {
	fmt.Println("🐱 NekoNest Doctor")
	fmt.Println("==================")
	ok := true
	check := func(name string, pass bool, detail string) {
		mark := "OK"
		if !pass {
			mark = "FAIL"
			ok = false
		}
		fmt.Printf("[%s] %s: %s\n", mark, name, detail)
	}

	check("daemon_version", true, buildinfo.Version)
	check("os", true, runtime.GOOS+"/"+runtime.GOARCH)
	cfg, err := config.LoadFrom(configPath)
	if err != nil {
		check("config", false, err.Error())
	} else {
		check("config", cfg.DeviceID != "" && cfg.Token != "" && cfg.ServerURL != "",
			fmt.Sprintf("device=%s server=%s path=%s", cfg.DeviceID, cfg.ServerURL, configPath))
		transportOK := true
		if requested := strings.TrimSpace(os.Getenv("NEKONEST_TRANSPORT_MODE")); requested != "" {
			mode, modeErr := config.NormalizeTransportMode(requested)
			transportOK = modeErr == nil && mode == cfg.TransportMode
		}
		check("transport_mode", transportOK, cfg.TransportMode)
	}

	idPath := identity.PathBesideConfig(configPath)
	if _, err := os.Stat(idPath); err != nil {
		idPath = identity.Path()
	}
	_, st, err := identity.LoadOrCreate(idPath)
	if err != nil {
		check("identity", false, err.Error())
	} else {
		fp := ""
		if st != nil {
			fp = st.Fingerprint
			if len(fp) > 16 {
				fp = fp[:16] + "…"
			}
		}
		check("identity", st != nil && st.Fingerprint != "", "fingerprint="+fp)
	}

	reg, err := adapters.NewDefaultRegistry()
	if err != nil {
		check("adapters", false, err.Error())
	} else {
		anyAvail := false
		for _, a := range reg.All() {
			avail := a.IsAvailable()
			if avail {
				anyAvail = true
			}
			mark := "missing"
			if avail {
				mark = "available"
			}
			fmt.Printf("  - %s: %s\n", a.Name(), mark)
		}
		check("adapters", anyAvail, "at least one CLI required")
		_ = reg.Close()
	}

	if cfg != nil {
		httpBase := config.HTTPBaseURL(cfg.ServerURL)
		client := &http.Client{Timeout: 8 * time.Second}
		resp, err := client.Get(httpBase + "/health")
		if err != nil {
			check("server_health", false, err.Error())
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			check("server_health", resp.StatusCode == http.StatusOK, fmt.Sprintf("%s status=%d", httpBase, resp.StatusCode))
			if resp.StatusCode == http.StatusOK {
				var health struct {
					ServerVersion string `json:"server_version"`
					TransportMode string `json:"transport_mode"`
				}
				if readErr != nil {
					check("component_versions", false, readErr.Error())
				} else if err := json.Unmarshal(body, &health); err != nil || health.ServerVersion == "" {
					check("component_versions", false, "server did not report its application version")
				} else {
					check(
						"component_versions",
						health.ServerVersion == buildinfo.Version,
						fmt.Sprintf("daemon=%s server=%s", buildinfo.Version, health.ServerVersion),
					)
					check("server_transport_mode", health.TransportMode == cfg.TransportMode,
						fmt.Sprintf("daemon=%s server=%s", cfg.TransportMode, health.TransportMode))
				}
			}
		}
	}

	// Codex app-server probe (non-fatal if missing — exec resume still works).
	cas := agentexec.NewCodexAppServer()
	codexVersion, codexVersionOK, codexVersionErr := cas.CodexVersion()
	if codexVersionErr != nil {
		fmt.Printf("[info] codex full-control version: unavailable (%v), minimum=%s\n", codexVersionErr, agentexec.MinimumCodexFullControlVersion)
	} else {
		fmt.Printf("[info] codex full-control version: installed=%s minimum=%s compatible=%v\n",
			codexVersion, agentexec.MinimumCodexFullControlVersion, codexVersionOK)
	}
	probe := cas.ProbeMethods()
	fmt.Printf("[info] codex app-server: available=%v schema=%v initialize=%v full_control=%v\n",
		probe["available"], probe["schema"], probe["initialize"], probe["full_control"])
	for _, method := range []string{"thread/start", "turn/start", "turn/steer", "turn/interrupt", "approval", "request_user_input", "collaboration_mode"} {
		fmt.Printf("  - %s: %v\n", method, probe[method])
	}
	_ = cas.Close()

	if skPath := sealedkeys.DefaultPath(); skPath != "" {
		if _, err := os.Stat(skPath); err == nil {
			fmt.Printf("[info] sealed keys file present: %s\n", skPath)
		} else {
			fmt.Printf("[info] sealed keys file not yet created (ok until sealed mode)\n")
		}
	}

	if ok {
		fmt.Println("\nAll critical checks passed.")
		return 0
	}
	fmt.Println("\nOne or more checks failed.")
	return 1
}
