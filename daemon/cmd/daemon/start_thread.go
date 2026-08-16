package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/startjournal"
)

const (
	defaultStartProbeTimeout     = 8 * time.Second
	defaultNativeStartTimeout    = 60 * time.Second
	defaultOwnershipWait         = 8 * time.Second
	defaultOwnershipPollInterval = 100 * time.Millisecond
)

var supportedStartAgents = []adapters.AgentType{
	adapters.AgentClaudeCode,
	adapters.AgentCodex,
	adapters.AgentKimiCLI,
	adapters.AgentGrokBuild,
	adapters.AgentZCode,
	adapters.AgentCursor,
}

func advertiseStartCapability(agentType adapters.AgentType, adapter adapters.Adapter) bool {
	switch agentType {
	case adapters.AgentZCode, adapters.AgentCursor:
		return adapter != nil && adapter.IsAvailable()
	default:
		return true
	}
}

type threadStartCommand struct {
	OperationID string
	AgentType   adapters.AgentType
	ProjectDir  string
	Prompt      string
	Attachments []attach.Ref
}

type threadStartEvent struct {
	OperationID    string
	AgentType      string
	State          startjournal.Status
	SessionID      string
	PromptAccepted bool
	Message        string
}

type threadStartCoordinator struct {
	journal             *startjournal.Journal
	lookupAdapter       func(string) (adapters.Adapter, bool)
	snapshotProjectDirs func() []string
	probeTimeout        time.Duration
	startTimeout        time.Duration
	ownershipWait       time.Duration
	ownershipPoll       time.Duration
	// probeStartCapability shares the daemon's cached native-starter probe with
	// session discovery. It is deliberately optional so focused coordinator
	// tests and standalone callers retain the safe direct-probe fallback.
	probeStartCapability func(context.Context, adapters.AgentType, adapters.NativeThreadStarter) (adapters.ThreadStartCapability, error)
	// materializeAttachments must download every ref before the native starter
	// is invoked. Keeping it injected makes the no-native-boundary failure
	// path directly testable.
	materializeAttachments func(sessionID string, refs []attach.Ref) (string, []attach.LocalFile, string, error)
}

func (c *threadStartCoordinator) Handle(parent context.Context, command threadStartCommand, emit func(threadStartEvent)) {
	if emit == nil {
		return
	}
	agentType := command.AgentType
	if agentType == "" {
		agentType = adapters.AgentCodex
	}
	normalizedDir, err := normalizeProjectDir(command.ProjectDir)
	if err != nil {
		emit(threadStartEvent{
			OperationID: command.OperationID,
			AgentType:   string(agentType),
			State:       startjournal.StatusFailed,
			Message:     err.Error(),
		})
		return
	}
	record, fresh, err := c.journal.Begin(command.OperationID, startjournal.Request{
		AgentType:         string(agentType),
		ProjectDir:        normalizedDir,
		PromptDigest:      startjournal.PromptDigest(command.Prompt),
		AttachmentsDigest: threadStartAttachmentsDigest(command.Attachments),
	})
	if err != nil {
		message := err.Error()
		if !startjournal.IsConflict(err) {
			message = "thread was not started because its durable start record could not be written: " + message
		}
		emit(threadStartEvent{
			OperationID: command.OperationID,
			AgentType:   string(agentType),
			State:       startjournal.StatusFailed,
			Message:     message,
		})
		return
	}
	if !fresh {
		emit(eventFromStartRecord(record))
		return
	}
	emit(eventFromStartRecord(record))

	finish := func(status startjournal.Status, sessionID, message string, promptAccepted ...bool) {
		accepted := len(promptAccepted) > 0 && promptAccepted[0]
		finished, finishErr := c.journal.FinishWithPromptOutcome(command.OperationID, status, sessionID, message, accepted)
		if finishErr == nil {
			emit(eventFromStartRecord(finished))
			return
		}
		fallback := "thread start outcome is indeterminate because its durable result could not be written; automatic retry is disabled: " + finishErr.Error()
		failedClosed, ok := c.journal.FailClosed(command.OperationID, fallback)
		if ok {
			failedClosed.SessionID = sessionID
			emit(eventFromStartRecord(failedClosed))
			return
		}
		emit(threadStartEvent{
			OperationID:    command.OperationID,
			AgentType:      string(agentType),
			State:          startjournal.StatusIndeterminate,
			SessionID:      sessionID,
			PromptAccepted: false,
			Message:        fallback,
		})
	}

	if strings.TrimSpace(command.Prompt) == "" {
		finish(startjournal.StatusFailed, "", "initial prompt is required")
		return
	}
	projectDir, err := resolveCurrentProjectDir(command.ProjectDir, c.snapshotProjectDirs())
	if err != nil {
		finish(startjournal.StatusFailed, "", err.Error())
		return
	}
	adapter, exists := c.lookupAdapter(string(agentType))
	starter, canStart := adapter.(adapters.NativeThreadStarter)
	if !exists || adapter == nil || !canStart {
		finish(startjournal.StatusFailed, "", "native thread creation is not implemented for "+string(agentType))
		return
	}

	probeTimeout := c.probeTimeout
	if probeTimeout <= 0 {
		probeTimeout = defaultStartProbeTimeout
	}
	probeCtx, cancelProbe := context.WithTimeout(parent, probeTimeout)
	capability, probePanic := c.probeThreadStart(probeCtx, agentType, starter)
	cancelProbe()
	if probePanic != nil {
		finish(startjournal.StatusFailed, "", "native thread creation probe failed: "+probePanic.Error())
		return
	}
	if !capability.Available {
		reason := strings.TrimSpace(capability.Reason)
		if reason == "" {
			reason = "native thread creation is unavailable for " + string(agentType)
		}
		finish(startjournal.StatusFailed, "", reason)
		return
	}
	if len(command.Attachments) > 0 && capability.AttachmentMode == adapters.AttachUnsupported {
		finish(startjournal.StatusFailed, "", "first-turn attachments are unavailable for the selected native control path")
		return
	}

	var (
		attachmentDir    string
		attachmentFiles  []attach.LocalFile
		attachmentSuffix string
	)
	if len(command.Attachments) > 0 {
		if c.materializeAttachments == nil {
			finish(startjournal.StatusFailed, "", "attachments are unavailable for native thread creation")
			return
		}
		attachmentDir, attachmentFiles, attachmentSuffix, err = c.materializeAttachments(command.OperationID, command.Attachments)
		if err != nil || len(attachmentFiles) != len(command.Attachments) {
			if attachmentDir != "" {
				_ = os.RemoveAll(attachmentDir)
			}
			message := "attachment download failed before native thread creation"
			if err != nil {
				message += ": " + err.Error()
			} else {
				message += ": one or more attachments could not be materialized"
			}
			finish(startjournal.StatusFailed, "", message)
			return
		}
	}
	var cleanupOnce sync.Once
	releaseFiles := func() {
		cleanupOnce.Do(func() {
			if attachmentDir != "" {
				_ = os.RemoveAll(attachmentDir)
			}
		})
	}
	// The native starter takes ownership of cleanup only after it positively
	// accepted the first turn. Every pre-boundary/failed path removes files now.
	starterOwnsFiles := false

	startTimeout := c.startTimeout
	if startTimeout <= 0 {
		startTimeout = defaultNativeStartTimeout
	}
	startCtx, cancelStart := context.WithTimeout(parent, startTimeout)
	started, startErr, startPanic := invokeNativeThreadStart(startCtx, starter, adapters.ThreadStartRequest{
		ProjectDir:  projectDir,
		Prompt:      command.Prompt + attachmentSuffix,
		Attachments: attachmentFiles,
		OnComplete:  releaseFiles,
	})
	cancelStart()
	sessionID := strings.TrimSpace(started.SessionID)
	crossedNativeBoundary := started.Created || started.PromptAccepted
	promptAccepted := started.PromptAccepted && startErr == nil && startPanic == nil
	starterOwnsFiles = crossedNativeBoundary && started.PromptResult != nil || promptAccepted
	if !starterOwnsFiles {
		releaseFiles()
	}
	var ownershipConfirmed bool
	if crossedNativeBoundary && sessionID != "" {
		ownershipResult := make(chan bool, 1)
		go func() { ownershipResult <- c.waitForOwnership(parent, adapter, sessionID) }()
		promptResult := started.PromptResult
		ownershipDone := false
		promptDone := promptAccepted || promptResult == nil
		for !ownershipDone || !promptDone {
			select {
			case ownershipConfirmed = <-ownershipResult:
				ownershipDone = true
			case promptErr, ok := <-promptResult:
				promptDone = true
				promptResult = nil
				promptAccepted = ok && promptErr == nil
				if promptErr != nil {
					startErr = promptErr
				}
			case <-parent.Done():
				finish(startjournal.StatusIndeterminate, sessionID, "daemon stopped while native ownership or initial prompt outcome was unresolved; automatic retry is disabled")
				return
			}
		}
		if started.PromptResult != nil {
			releaseFiles()
			starterOwnsFiles = false
		}
		if promptAccepted && !ownershipConfirmed {
			ownershipConfirmed = c.waitForOwnership(parent, adapter, sessionID)
		}
	}
	if ownershipConfirmed && promptAccepted {
		finish(startjournal.StatusOwned, sessionID, "", true)
		return
	}
	if ownershipConfirmed {
		reason := "native thread ownership was confirmed, but the initial prompt was not; the outcome is indeterminate and automatic retry is disabled"
		if startErr != nil {
			reason += ": " + startErr.Error()
		}
		finish(startjournal.StatusIndeterminate, sessionID, reason)
		return
	}
	if startPanic != nil || crossedNativeBoundary {
		reason := "native thread start outcome is indeterminate; ownership was not confirmed in the agent's native store and automatic retry is disabled"
		if startPanic != nil {
			reason += ": " + startPanic.Error()
		} else if startErr != nil {
			reason += ": " + startErr.Error()
		} else if sessionID == "" {
			reason += ": no native session id returned"
		}
		finish(startjournal.StatusIndeterminate, sessionID, reason)
		return
	}
	reason := "native thread was not created"
	if startErr != nil {
		reason = startErr.Error()
	}
	finish(startjournal.StatusFailed, "", reason)
}

func (c *threadStartCoordinator) probeThreadStart(ctx context.Context, agentType adapters.AgentType, starter adapters.NativeThreadStarter) (adapters.ThreadStartCapability, error) {
	if c.probeStartCapability != nil {
		return c.probeStartCapability(ctx, agentType, starter)
	}
	return probeNativeThreadStart(ctx, starter)
}

func threadStartAttachmentsDigest(refs []attach.Ref) string {
	if len(refs) == 0 {
		return ""
	}
	// attach.Ref contains only strings, so this canonical JSON form cannot fail
	// to marshal and preserves attachment order as part of the operation binding.
	data, err := json.Marshal(refs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (c *threadStartCoordinator) waitForOwnership(ctx context.Context, adapter adapters.Adapter, sessionID string) bool {
	if safeOwnsSession(adapter, sessionID) {
		return true
	}
	wait := c.ownershipWait
	if wait <= 0 {
		wait = defaultOwnershipWait
	}
	poll := c.ownershipPoll
	if poll <= 0 {
		poll = defaultOwnershipPollInterval
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return safeOwnsSession(adapter, sessionID)
		case <-ticker.C:
			if safeOwnsSession(adapter, sessionID) {
				return true
			}
		}
	}
}

func eventFromStartRecord(record startjournal.Record) threadStartEvent {
	return threadStartEvent{
		OperationID:    record.OperationID,
		AgentType:      record.AgentType,
		State:          record.Status,
		SessionID:      record.SessionID,
		PromptAccepted: record.PromptAccepted,
		Message:        record.Message,
	}
}

func probeNativeThreadStart(ctx context.Context, starter adapters.NativeThreadStarter) (capability adapters.ThreadStartCapability, panicErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = fmt.Errorf("adapter panic: %v", recovered)
		}
	}()
	return starter.ProbeThreadStart(ctx), nil
}

func invokeNativeThreadStart(ctx context.Context, starter adapters.NativeThreadStarter, request adapters.ThreadStartRequest) (result adapters.ThreadStartResult, err error, panicErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = fmt.Errorf("adapter panic: %v", recovered)
		}
	}()
	result, err = starter.StartNativeThread(ctx, request)
	return result, err, nil
}

func safeOwnsSession(adapter adapters.Adapter, sessionID string) (owned bool) {
	defer func() {
		if recover() != nil {
			owned = false
		}
	}()
	return adapter != nil && adapter.OwnsSession(sessionID)
}

func parseStartAgentType(value string) (adapters.AgentType, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return adapters.AgentCodex, true
	}
	for _, agentType := range supportedStartAgents {
		if value == string(agentType) {
			return agentType, true
		}
	}
	return "", false
}

func coalesceStartProjectDir(projectDir, legacyCWD string) (string, error) {
	projectDir = strings.TrimSpace(projectDir)
	legacyCWD = strings.TrimSpace(legacyCWD)
	if projectDir == "" {
		return legacyCWD, nil
	}
	if legacyCWD == "" {
		return projectDir, nil
	}
	projectKey, err := normalizeProjectDir(projectDir)
	if err != nil {
		return "", err
	}
	legacyKey, err := normalizeProjectDir(legacyCWD)
	if err != nil {
		return "", err
	}
	if projectKey != legacyKey {
		return "", errors.New("project_dir and cwd must identify the same discovered directory")
	}
	return projectDir, nil
}

// normalizeProjectDir performs OS-independent lexical normalization so a
// Windows path cannot bypass validation on Linux (or vice versa). Windows
// comparisons are case-insensitive; POSIX comparisons preserve case.
func normalizeProjectDir(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", errors.New("project_dir is required")
	}
	if strings.ContainsRune(raw, '\x00') {
		return "", errors.New("project_dir contains NUL")
	}
	slashed := strings.ReplaceAll(raw, `\`, "/")
	lowerSlashed := strings.ToLower(slashed)
	if strings.HasPrefix(lowerSlashed, "//./") {
		return "", errors.New("Windows device paths are not allowed")
	}
	if strings.HasPrefix(lowerSlashed, "//?/unc/") {
		slashed = "//" + slashed[len("//?/unc/"):]
		lowerSlashed = strings.ToLower(slashed)
	} else if strings.HasPrefix(lowerSlashed, "//?/") {
		slashed = slashed[len("//?/"):]
		lowerSlashed = strings.ToLower(slashed)
	}
	if isWindowsDriveAbsolute(slashed) {
		if containsParentSegment(strings.Split(slashed[3:], "/")) {
			return "", errors.New("project_dir must not contain '..'")
		}
		cleaned := pathpkg.Clean("/" + slashed[3:])
		return "windows:" + strings.ToLower(slashed[:2]+cleaned), nil
	}
	if len(slashed) >= 2 && slashed[1] == ':' {
		return "", errors.New("Windows project_dir must be drive-absolute")
	}
	if strings.HasPrefix(slashed, "//") {
		parts := strings.Split(strings.TrimPrefix(slashed, "//"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", errors.New("UNC project_dir requires server and share")
		}
		if containsParentSegment(parts) {
			return "", errors.New("project_dir must not contain '..'")
		}
		cleaned := strings.TrimPrefix(pathpkg.Clean("/"+strings.Join(parts, "/")), "/")
		return "windows:unc/" + strings.ToLower(cleaned), nil
	}
	if strings.HasPrefix(raw, "/") {
		parts := strings.Split(raw, "/")
		if containsParentSegment(parts) {
			return "", errors.New("project_dir must not contain '..'")
		}
		return "posix:" + pathpkg.Clean(raw), nil
	}
	return "", errors.New("project_dir must be an absolute Windows or POSIX path")
}

func isWindowsDriveAbsolute(value string) bool {
	if len(value) < 3 || value[1] != ':' || value[2] != '/' {
		return false
	}
	letter := value[0]
	return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
}

func containsParentSegment(parts []string) bool {
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

func canonicalNativeProjectDir(value string) (string, string, error) {
	normalized, err := normalizeProjectDir(value)
	if err != nil {
		return "", "", err
	}
	if runtime.GOOS == "windows" && !strings.HasPrefix(normalized, "windows:") {
		return "", "", errors.New("project_dir is not a native Windows path")
	}
	if runtime.GOOS != "windows" && !strings.HasPrefix(normalized, "posix:") {
		return "", "", errors.New("project_dir is not a native POSIX path")
	}
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if !filepath.IsAbs(cleaned) {
		return "", "", errors.New("project_dir must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize project_dir: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", "", fmt.Errorf("stat project_dir: %w", err)
	}
	if !info.IsDir() {
		return "", "", errors.New("project_dir is not a directory")
	}
	canonical = filepath.Clean(canonical)
	canonicalNormalized, err := normalizeProjectDir(canonical)
	if err != nil {
		return "", "", err
	}
	return canonical, canonicalNormalized, nil
}

func resolveCurrentProjectDir(requested string, discovered []string) (string, error) {
	requestedNormalized, err := normalizeProjectDir(requested)
	if err != nil {
		return "", err
	}
	for _, candidate := range discovered {
		candidateNormalized, candidateErr := normalizeProjectDir(candidate)
		if candidateErr != nil || requestedNormalized != candidateNormalized {
			continue
		}
		requestedCanonical, requestedResolved, requestedErr := canonicalNativeProjectDir(requested)
		candidateCanonical, candidateResolved, candidateErr := canonicalNativeProjectDir(candidate)
		if requestedErr == nil && candidateErr == nil && requestedResolved == candidateResolved {
			// Pass the daemon-resolved native path, never an untrusted spelling.
			if runtime.GOOS == "windows" && !strings.EqualFold(requestedCanonical, candidateCanonical) {
				continue
			}
			if runtime.GOOS != "windows" && requestedCanonical != candidateCanonical {
				continue
			}
			return candidateCanonical, nil
		}
	}
	return "", errors.New("directory not in currently discovered projects")
}

func snapshotProjectDirs(mu *sync.Mutex, sessions map[string]*adapters.SessionInfo) []string {
	mu.Lock()
	defer mu.Unlock()
	dirs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session != nil && strings.TrimSpace(session.ProjectDir) != "" {
			dirs = append(dirs, session.ProjectDir)
		}
	}
	return dirs
}

type startCapabilityActivityTier string

const (
	startCapabilityActiveTier  startCapabilityActivityTier = "active"
	startCapabilityRecentTier  startCapabilityActivityTier = "recent"
	startCapabilityDormantTier startCapabilityActivityTier = "dormant"
)

const (
	startCapabilityActiveInterval  = 5 * time.Minute
	startCapabilityRecentInterval  = time.Hour
	startCapabilityDormantInterval = 24 * time.Hour
)

type startCapabilityCacheEntry struct {
	capability adapters.ThreadStartCapability
	activity   startCapabilityActivityTier
	hasResult  bool
	lastProbe  time.Time
	nextProbe  time.Time
	inFlight   bool
	done       chan struct{}
	waiters    int
}

// agentStartCapabilityCache keeps native-start probes independent per CLI.
// Discovery never waits on a child CLI: a missing first result is fail-closed
// and the following regular discovery publishes the completed value.
type agentStartCapabilityCache struct {
	mu           sync.Mutex
	entries      map[adapters.AgentType]*startCapabilityCacheEntry
	now          func() time.Time
	probeTimeout time.Duration
	logProbe     func(adapters.AgentType, startCapabilityActivityTier, string, time.Time)
}

// The optional argument preserves callers compiled against the former
// TTL-based constructor. Probe intervals are deliberately fixed by activity.
func newAgentStartCapabilityCache(_ ...time.Duration) *agentStartCapabilityCache {
	return &agentStartCapabilityCache{
		entries:      make(map[adapters.AgentType]*startCapabilityCacheEntry),
		now:          time.Now,
		probeTimeout: defaultStartProbeTimeout,
	}
}

func startCapabilityTier(agentType adapters.AgentType, sessions []*adapters.SessionInfo, now time.Time) startCapabilityActivityTier {
	recentCutoff := now.Add(-adapters.RecentSessionWindow)
	activeCutoff := now.Add(-15 * time.Minute)
	hasRecent := false
	for _, session := range sessions {
		if session == nil || session.AgentType != agentType {
			continue
		}
		switch session.Status {
		case adapters.StatusRunning, adapters.StatusWaitingUser, adapters.StatusWaitingApproval:
			return startCapabilityActiveTier
		}
		if !session.LastActivity.Before(activeCutoff) {
			return startCapabilityActiveTier
		}
		if !session.LastActivity.Before(recentCutoff) {
			hasRecent = true
		}
	}
	if hasRecent {
		return startCapabilityRecentTier
	}
	return startCapabilityDormantTier
}

func startCapabilityProbeInterval(tier startCapabilityActivityTier) time.Duration {
	switch tier {
	case startCapabilityActiveTier:
		return startCapabilityActiveInterval
	case startCapabilityRecentTier:
		return startCapabilityRecentInterval
	default:
		return startCapabilityDormantInterval
	}
}

func (c *agentStartCapabilityCache) currentTime() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *agentStartCapabilityCache) Get(parent context.Context, registry *adapters.Registry, sessionSets ...[]*adapters.SessionInfo) []map[string]interface{} {
	var sessions []*adapters.SessionInfo
	if len(sessionSets) > 0 {
		sessions = sessionSets[0]
	}
	entries := make([]map[string]interface{}, 0, len(supportedStartAgents))
	for _, agentType := range supportedStartAgents {
		adapter, _ := registry.Get(string(agentType))
		if !advertiseStartCapability(agentType, adapter) {
			continue
		}
		tier := startCapabilityTier(agentType, sessions, c.currentTime())
		capability, hasResult, inFlight, implemented := c.getOrStart(parent, registry, agentType, tier)
		entries = append(entries, startCapabilityEntry(agentType, capability, hasResult, inFlight, implemented))
	}
	return entries
}

// ProbeForStart is the mandatory first-prompt preflight. It forces a fresh
// result when no probe is running, but joins an in-flight discovery probe so a
// user action never launches a second CLI process for the same agent.
func (c *agentStartCapabilityCache) ProbeForStart(parent context.Context, registry *adapters.Registry, agentType adapters.AgentType, sessions []*adapters.SessionInfo) (adapters.ThreadStartCapability, error) {
	tier := startCapabilityTier(agentType, sessions, c.currentTime())
	waitedForProbe := false
	for {
		c.mu.Lock()
		entry := c.entryLocked(agentType)
		if entry.inFlight {
			done := entry.done
			entry.waiters++
			c.mu.Unlock()
			waitedForProbe = true
			select {
			case <-done:
				c.mu.Lock()
				entry.waiters--
				c.mu.Unlock()
				continue
			case <-parent.Done():
				c.mu.Lock()
				entry.waiters--
				c.mu.Unlock()
				return adapters.ThreadStartCapability{}, parent.Err()
			}
		}
		if waitedForProbe && entry.hasResult {
			capability := entry.capability
			c.mu.Unlock()
			return capability, nil
		}
		entry.inFlight = true
		entry.done = make(chan struct{})
		c.mu.Unlock()
		return c.probeAndStore(parent, registry, agentType, tier)
	}
}

func (c *agentStartCapabilityCache) getOrStart(parent context.Context, registry *adapters.Registry, agentType adapters.AgentType, tier startCapabilityActivityTier) (adapters.ThreadStartCapability, bool, bool, bool) {
	adapter, exists := registry.Get(string(agentType))
	_, implemented := adapter.(adapters.NativeThreadStarter)
	if !exists || adapter == nil || !implemented {
		return adapters.ThreadStartCapability{}, false, false, false
	}

	c.mu.Lock()
	entry := c.entryLocked(agentType)
	entry.activity = tier
	if entry.inFlight {
		capability, hasResult := entry.capability, entry.hasResult
		c.mu.Unlock()
		return capability, hasResult, true, true
	}
	if entry.hasResult {
		entry.nextProbe = entry.lastProbe.Add(startCapabilityProbeInterval(tier))
	}
	if entry.hasResult && c.currentTime().Before(entry.nextProbe) {
		capability := entry.capability
		c.mu.Unlock()
		return capability, true, false, true
	}
	entry.inFlight = true
	entry.done = make(chan struct{})
	capability, hasResult := entry.capability, entry.hasResult
	c.mu.Unlock()
	go func() { _, _ = c.probeAndStore(parent, registry, agentType, tier) }()
	return capability, hasResult, true, true
}

func (c *agentStartCapabilityCache) entryLocked(agentType adapters.AgentType) *startCapabilityCacheEntry {
	entry := c.entries[agentType]
	if entry == nil {
		entry = &startCapabilityCacheEntry{}
		c.entries[agentType] = entry
	}
	return entry
}

func (c *agentStartCapabilityCache) probeAndStore(parent context.Context, registry *adapters.Registry, agentType adapters.AgentType, tier startCapabilityActivityTier) (adapters.ThreadStartCapability, error) {
	capability, err := c.runProbe(parent, registry, agentType)
	now := c.currentTime()
	next := now.Add(startCapabilityProbeInterval(tier))
	c.mu.Lock()
	entry := c.entryLocked(agentType)
	entry.capability = capability
	entry.activity = tier
	entry.hasResult = true
	entry.lastProbe = now
	entry.nextProbe = next
	entry.inFlight = false
	done := entry.done
	entry.done = nil
	c.mu.Unlock()
	if done != nil {
		close(done)
	}
	if c.logProbe != nil {
		outcome := "unavailable"
		if err != nil {
			outcome = "failed"
		} else if capability.Available {
			outcome = "available"
		}
		c.logProbe(agentType, tier, outcome, next)
	}
	return capability, err
}

func (c *agentStartCapabilityCache) runProbe(parent context.Context, registry *adapters.Registry, agentType adapters.AgentType) (adapters.ThreadStartCapability, error) {
	adapter, exists := registry.Get(string(agentType))
	starter, canStart := adapter.(adapters.NativeThreadStarter)
	if !exists || adapter == nil || !canStart {
		return adapters.ThreadStartCapability{Reason: "native thread creation is not implemented"}, nil
	}
	timeout := c.probeTimeout
	if timeout <= 0 {
		timeout = defaultStartProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	type result struct {
		capability adapters.ThreadStartCapability
		err        error
	}
	resultCh := make(chan result, 1)
	go func() {
		capability, panicErr := probeNativeThreadStart(probeCtx, starter)
		resultCh <- result{capability: capability, err: panicErr}
	}()
	select {
	case result := <-resultCh:
		if result.err != nil {
			return adapters.ThreadStartCapability{Reason: "native thread creation probe failed"}, result.err
		}
		return result.capability, nil
	case <-probeCtx.Done():
		return adapters.ThreadStartCapability{Reason: "native thread creation probe failed"}, probeCtx.Err()
	}
}

func startCapabilityEntry(agentType adapters.AgentType, capability adapters.ThreadStartCapability, hasResult, inFlight, implemented bool) map[string]interface{} {
	entry := map[string]interface{}{
		"agent_type":      string(agentType),
		"available":       false,
		"spawn":           false,
		"attachment_mode": string(adapters.AttachUnsupported),
	}
	if !implemented {
		entry["reason"] = "native thread creation is not implemented"
		return entry
	}
	if !hasResult {
		entry["reason"] = "native thread creation probe is in progress"
		return entry
	}
	entry["available"] = capability.Available
	entry["spawn"] = capability.Available
	entry["control_path"] = capability.ControlPath
	entry["control_version"] = capability.ControlVersion
	entry["attachment_mode"] = string(capability.AttachmentMode)
	if !capability.Available {
		reason := strings.TrimSpace(capability.Reason)
		if reason == "" {
			reason = "native thread creation is unavailable"
		}
		entry["reason"] = reason
	}
	if inFlight {
		// Keep the last known value while the next due probe is running.
		entry["spawn"] = capability.Available
	}
	return entry
}
