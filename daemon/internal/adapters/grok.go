package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/agentexec"
)

type grokSessionRecord struct {
	nativeID   string
	sessionDir string
	projectDir string
}

// GrokBuildAdapter discovers and resumes local Grok Build sessions.
type GrokBuildAdapter struct {
	sessionsDir string
	mu          sync.RWMutex
	records     map[string]grokSessionRecord
	commander   *agentexec.GrokCommander
	watches     pollWatchRegistry
}

// NewGrokBuildAdapter creates a Grok Build adapter using the local session store.
func NewGrokBuildAdapter() *GrokBuildAdapter {
	home, _ := os.UserHomeDir()
	grokHome := strings.TrimSpace(os.Getenv("GROK_HOME"))
	if grokHome == "" {
		grokHome = filepath.Join(home, ".grok")
	}
	return &GrokBuildAdapter{
		sessionsDir: filepath.Join(grokHome, "sessions"),
		records:     make(map[string]grokSessionRecord),
		commander:   agentexec.NewGrokCommander(),
	}
}

func (a *GrokBuildAdapter) Name() string { return string(AgentGrokBuild) }

func (a *GrokBuildAdapter) IsAvailable() bool {
	return a.commander != nil && a.commander.IsAvailable()
}

func (a *GrokBuildAdapter) ProbeThreadStart(ctx context.Context) ThreadStartCapability {
	if a.commander == nil {
		return ThreadStartCapability{Reason: "Grok commander is unavailable"}
	}
	if err := a.commander.ProbeThreadStart(ctx); err != nil {
		return ThreadStartCapability{Reason: err.Error()}
	}
	return ThreadStartCapability{Available: true}
}

func (a *GrokBuildAdapter) StartNativeThread(ctx context.Context, request ThreadStartRequest) (ThreadStartResult, error) {
	if a.commander == nil {
		return ThreadStartResult{}, fmt.Errorf("Grok commander is unavailable")
	}
	nativeID, created, promptAccepted, err := a.commander.StartThread(ctx, request.ProjectDir, request.Prompt)
	return ThreadStartResult{SessionID: publicSessionID(AgentGrokBuild, nativeID), Created: created, PromptAccepted: promptAccepted}, err
}

func (a *GrokBuildAdapter) Close() error {
	a.watches.stopAll()
	if a.commander != nil {
		a.commander.StopAll()
	}
	return nil
}

// Discover reads top-level Grok sessions and excludes nested subagent stores.
func (a *GrokBuildAdapter) Discover() ([]*SessionInfo, error) {
	if _, err := os.Stat(a.sessionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	subagentIDs := discoverGrokSubagentIDs(a.sessionsDir)
	type candidate struct {
		session *SessionInfo
		record  grokSessionRecord
	}
	candidates := make(map[string]candidate)
	err := filepath.WalkDir(a.sessionsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.EqualFold(entry.Name(), "subagents") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(entry.Name(), "summary.json") {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}
		session, record, err := a.parseSummary(path, info.ModTime())
		if err != nil || session == nil {
			return nil
		}
		if _, hidden := subagentIDs[record.nativeID]; hidden {
			return nil
		}
		if previous, exists := candidates[record.nativeID]; exists &&
			!session.LastActivity.After(previous.session.LastActivity) {
			return nil
		}
		candidates[record.nativeID] = candidate{session: session, record: record}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk grok sessions: %w", err)
	}

	records := make(map[string]grokSessionRecord, len(candidates))
	sessions := make([]*SessionInfo, 0, len(candidates))
	for nativeID, item := range candidates {
		records[nativeID] = item.record
		sessions = append(sessions, item.session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	a.mu.Lock()
	a.records = records
	a.mu.Unlock()
	return sessions, nil
}

// discoverGrokSubagentIDs reads the stable child-session markers written below
// a parent's subagents directory. Current Grok stores the child transcript as
// a normal top-level session, so skipping nested directories alone is not
// enough to keep implementation-detail sessions out of the phone UI.
func discoverGrokSubagentIDs(root string) map[string]struct{} {
	ids := make(map[string]struct{})
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() ||
			!strings.EqualFold(entry.Name(), "meta.json") ||
			!pathContainsSegment(filepath.Dir(path), "subagents") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var document map[string]interface{}
		if json.Unmarshal(raw, &document) != nil {
			return nil
		}
		for _, key := range []string{
			"child_session_id",
			"childSessionId",
			"subagent_id",
			"subagentId",
		} {
			if id := firstString(document, key); id != "" {
				ids[id] = struct{}{}
			}
		}
		return nil
	})
	return ids
}

func pathContainsSegment(path, want string) bool {
	for _, segment := range strings.FieldsFunc(filepath.Clean(path), func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if strings.EqualFold(segment, want) {
			return true
		}
	}
	return false
}

// OwnsSession validates a namespaced Grok ID against the discovered store.
func (a *GrokBuildAdapter) OwnsSession(sessionID string) bool {
	nativeID, err := nativeSessionID(AgentGrokBuild, sessionID)
	if err != nil {
		return false
	}
	_, err = a.resolveRecord(nativeID)
	return err == nil
}

func (a *GrokBuildAdapter) parseSummary(path string, fallback time.Time) (*SessionInfo, grokSessionRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, grokSessionRecord{}, err
	}
	var document map[string]interface{}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, grokSessionRecord{}, err
	}
	info, _ := document["info"].(map[string]interface{})
	nativeID := firstString(info, "id", "session_id", "sessionId")
	if nativeID == "" {
		nativeID = firstString(document, "id", "session_id", "sessionId")
	}
	sessionDir := filepath.Dir(path)
	if nativeID == "" {
		nativeID = filepath.Base(sessionDir)
	}
	if nativeID == "" {
		return nil, grokSessionRecord{}, fmt.Errorf("missing grok session id")
	}
	if grokSummaryIsSubagent(document, info) {
		return nil, grokSessionRecord{}, fmt.Errorf("grok subagent session: %s", nativeID)
	}

	projectDir := firstString(info, "cwd", "work_dir", "workDir")
	if projectDir == "" {
		projectDir = firstString(document, "cwd", "work_dir", "workDir")
	}
	if projectDir == "" {
		encoded := filepath.Base(filepath.Dir(sessionDir))
		if decoded, err := url.PathUnescape(encoded); err == nil {
			projectDir = decoded
		}
	}
	last := firstTime(document, "updated_at", "updatedAt", "last_activity", "lastActivity")
	if last.IsZero() {
		last = firstTime(info, "updated_at", "updatedAt", "created_at", "createdAt")
	}
	if last.IsZero() {
		last = fallback
	}
	summary := firstString(document, "session_summary", "summary", "title", "name")
	if grokSummaryIsSynthetic(summary) {
		summary = ""
	}
	if summary == "" {
		summary = nativeID
	}
	status := StatusIdle
	if time.Since(last) < time.Minute {
		status = StatusRunning
	}
	record := grokSessionRecord{
		nativeID:   nativeID,
		sessionDir: sessionDir,
		projectDir: projectDir,
	}
	return &SessionInfo{
		ID:           publicSessionID(AgentGrokBuild, nativeID),
		AgentType:    AgentGrokBuild,
		Status:       status,
		Summary:      truncateRunes(strings.TrimSpace(summary), 120),
		LastActivity: last,
		SessionPath:  sessionDir,
		ProjectDir:   projectDir,
	}, record, nil
}

func grokSummaryIsSubagent(document, info map[string]interface{}) bool {
	for _, values := range []map[string]interface{}{document, info} {
		if firstString(
			values,
			"parent_session_id",
			"parentSessionId",
			"subagent_id",
			"subagentId",
		) != "" ||
			boolField(values, "is_subagent", "isSubagent") {
			return true
		}
	}
	return false
}

func (a *GrokBuildAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	nativeID, err := nativeSessionID(AgentGrokBuild, sessionID)
	if err != nil {
		return nil, err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		return nil, err
	}
	updatesPath := filepath.Join(record.sessionDir, "updates.jsonl")
	if _, statErr := os.Stat(updatesPath); statErr == nil {
		messages, readErr := readGrokUpdatesHistory(updatesPath, nativeID, limit)
		if readErr != nil {
			return nil, fmt.Errorf("read grok updates history: %w", readErr)
		}
		return messages, nil
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("stat grok updates history: %w", statErr)
	}

	path := filepath.Join(record.sessionDir, "chat_history.jsonl")
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		// A valid Grok session can have only summary.json before its first
		// persisted turn. Keep it discoverable and expose an empty history.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open grok history: %w", err)
	}
	defer file.Close()

	var messages []*HistoryMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	index := 0
	skipPrimerResponse := false
	for scanner.Scan() {
		var item map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &item) != nil {
			continue
		}
		if grokTextIsSyntheticPrimer(contentToText(item["content"])) {
			skipPrimerResponse = true
			continue
		}
		if grokHistoryItemIsSynthetic(item) {
			continue
		}
		role := strings.ToLower(firstString(item, "role"))
		if role == "" {
			candidate := strings.ToLower(firstString(item, "type"))
			if candidate == "user" || candidate == "assistant" {
				role = candidate
			}
		}
		_, hasPromptIndex := item["prompt_index"]
		if !hasPromptIndex {
			_, hasPromptIndex = item["promptIndex"]
		}
		// Current Grok stores a hidden primer as an unindexed type=user
		// record. Human prompts in this fallback file carry prompt_index.
		if role == "user" && firstString(item, "role") == "" {
			if !hasPromptIndex {
				continue
			}
		}
		content := contentToText(item["content"])
		if nested, ok := item["message"].(map[string]interface{}); ok {
			if role == "" {
				role = strings.ToLower(firstString(nested, "role"))
			}
			if content == "" {
				content = contentToText(nested["content"])
			}
		}
		if skipPrimerResponse {
			// A primer turn can contain multiple assistant/tool/reasoning
			// records. Suppress the entire turn until the next indexed human
			// prompt instead of exposing later assistant fragments.
			if role != "user" || !hasPromptIndex {
				continue
			}
			skipPrimerResponse = false
		} else if role == "user" {
			skipPrimerResponse = false
		}
		content = strings.TrimSpace(content)
		if (role != "user" && role != "assistant") || content == "" {
			continue
		}
		index++
		messageID := firstString(item, "id", "message_id", "messageId")
		if messageID == "" {
			messageID = fmt.Sprintf("grok_%s_%d", nativeID, index)
		}
		timestamp := firstTime(item, "timestamp", "created_at", "createdAt", "updated_at", "updatedAt")
		if timestamp.IsZero() {
			timestamp = time.Now()
		}
		messages = append(messages, &HistoryMessage{
			ID:        messageID,
			Role:      role,
			Content:   truncateRunes(content, 4000),
			Type:      "text",
			Timestamp: timestamp.Unix(),
		})
	}
	return takeLastHistory(messages, limit), scanner.Err()
}

func readGrokUpdatesHistory(path, nativeID string, limit int) ([]*HistoryMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	type turnBuffer struct {
		user        strings.Builder
		assistant   strings.Builder
		userAt      time.Time
		assistantAt time.Time
		synthetic   bool
	}
	var (
		turn     turnBuffer
		turnNo   int
		messages []*HistoryMessage
	)
	flush := func() {
		if turn.synthetic {
			turn = turnBuffer{}
			return
		}
		user := strings.TrimSpace(turn.user.String())
		assistant := strings.TrimSpace(turn.assistant.String())
		if user == "" && assistant == "" {
			turn = turnBuffer{}
			return
		}
		turnNo++
		if user != "" {
			messages = append(messages, grokHistoryMessage(
				fmt.Sprintf("grok_%s_turn_%d_user", nativeID, turnNo),
				"user",
				user,
				turn.userAt,
			))
		}
		if assistant != "" {
			messages = append(messages, grokHistoryMessage(
				fmt.Sprintf("grok_%s_turn_%d_assistant", nativeID, turnNo),
				"assistant",
				assistant,
				turn.assistantAt,
			))
		}
		turn = turnBuffer{}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var item map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &item) != nil {
			continue
		}
		params, _ := item["params"].(map[string]interface{})
		update, _ := params["update"].(map[string]interface{})
		kind := strings.ToLower(firstString(update, "sessionUpdate", "session_update"))
		synthetic := grokHistoryItemIsSynthetic(item) ||
			grokHistoryItemIsSynthetic(params) ||
			grokHistoryItemIsSynthetic(update)
		if synthetic {
			if kind == "user_message_chunk" {
				if turn.assistant.Len() > 0 {
					flush()
				}
				turn.synthetic = true
			}
			continue
		}
		timestamp := firstTime(item, "timestamp", "created_at", "createdAt")
		switch kind {
		case "user_message_chunk":
			if turn.synthetic || turn.assistant.Len() > 0 {
				flush()
			}
			content := contentToText(update["content"])
			if content == "" {
				continue
			}
			if turn.userAt.IsZero() {
				turn.userAt = timestamp
			}
			turn.user.WriteString(content)
		case "agent_message_chunk":
			if turn.synthetic {
				continue
			}
			content := contentToText(update["content"])
			if content == "" {
				continue
			}
			if turn.assistantAt.IsZero() {
				turn.assistantAt = timestamp
			}
			turn.assistant.WriteString(content)
		case "turn_completed":
			flush()
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return takeLastHistory(messages, limit), nil
}

func grokHistoryMessage(id, role, content string, timestamp time.Time) *HistoryMessage {
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return &HistoryMessage{
		ID:        id,
		Role:      role,
		Content:   truncateRunes(content, 4000),
		Type:      "text",
		Timestamp: timestamp.Unix(),
	}
}

func grokHistoryItemIsSynthetic(item map[string]interface{}) bool {
	if item == nil {
		return false
	}
	if grokTextIsSyntheticPrimer(contentToText(item["content"])) {
		return true
	}
	if firstString(item, "synthetic_reason", "syntheticReason") != "" ||
		boolField(item, "hidden", "is_hidden", "isHidden", "synthetic", "is_synthetic", "isSynthetic") {
		return true
	}
	role := strings.ToLower(firstString(item, "role", "message_role", "messageRole", "author_role", "authorRole"))
	if role == "system" || role == "developer" {
		return true
	}
	for _, key := range []string{"type", "message_type", "messageType", "kind", "source"} {
		marker := strings.ToLower(firstString(item, key))
		if marker == "system" ||
			strings.Contains(marker, "primer") ||
			strings.Contains(marker, "synthetic") {
			return true
		}
	}
	for _, key := range []string{"metadata", "message", "author"} {
		if nested, ok := item[key].(map[string]interface{}); ok && grokHistoryItemIsSynthetic(nested) {
			return true
		}
	}
	return false
}

func grokTextIsSyntheticPrimer(text string) bool {
	prefix := strings.ToLower(truncateRunes(strings.TrimSpace(text), 768))
	if strings.Contains(prefix, "[grok-build-vscode primer") {
		return true
	}
	return strings.Contains(prefix, "## hidden primer") &&
		strings.Contains(prefix, "system message, not a user request")
}

func grokSummaryIsSynthetic(summary string) bool {
	if grokTextIsSyntheticPrimer(summary) {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(summary))
	return strings.Contains(normalized, "plan mode exit verdict") &&
		strings.Contains(normalized, "primer")
}

func (a *GrokBuildAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
	if _, err := nativeSessionID(AgentGrokBuild, sessionID); err != nil {
		return nil, err
	}
	if _, err := a.resolvePublicSession(sessionID); err != nil {
		return nil, err
	}
	ctx, watchID := a.watches.start(sessionID)
	ch := make(chan *SessionInfo, 4)
	go func() {
		defer close(ch)
		defer a.watches.finish(sessionID, watchID)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		var last time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				session, err := a.resolvePublicSession(sessionID)
				if err != nil {
					continue
				}
				if session.LastActivity.After(last) {
					last = session.LastActivity
					select {
					case ch <- session:
					default:
					}
				}
			}
		}
	}()
	return ch, nil
}

func (a *GrokBuildAdapter) SendPrompt(sessionID string, request PromptRequest) error {
	nativeID, err := nativeSessionID(AgentGrokBuild, sessionID)
	if err != nil {
		return err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		return err
	}
	return a.commander.SendPromptInDir(
		nativeID,
		request.Prompt,
		record.projectDir,
		request.Attachments,
		request.OnComplete,
	)
}

func (a *GrokBuildAdapter) Approve(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentGrokBuild, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Approve(nativeID, approvalID)
}

func (a *GrokBuildAdapter) Deny(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentGrokBuild, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Deny(nativeID, approvalID)
}

func (a *GrokBuildAdapter) Interrupt(sessionID string) error {
	nativeID, err := nativeSessionID(AgentGrokBuild, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Interrupt(nativeID)
}

func (a *GrokBuildAdapter) SetOutputSink(sink OutputSink) {
	if a.commander == nil {
		return
	}
	if sink == nil {
		a.commander.OnAgentOutput = nil
		return
	}
	a.commander.OnAgentOutput = func(nativeID, msgType, content, messageID string) {
		sink(OutputEvent{
			SessionID: publicSessionID(AgentGrokBuild, nativeID),
			AgentType: AgentGrokBuild,
			Type:      msgType,
			Content:   content,
			MessageID: messageID,
		})
	}
}

func (a *GrokBuildAdapter) resolveRecord(nativeID string) (grokSessionRecord, error) {
	a.mu.RLock()
	record, ok := a.records[nativeID]
	a.mu.RUnlock()
	if ok {
		return record, nil
	}
	if _, err := a.Discover(); err != nil {
		return grokSessionRecord{}, err
	}
	a.mu.RLock()
	record, ok = a.records[nativeID]
	a.mu.RUnlock()
	if !ok {
		return grokSessionRecord{}, fmt.Errorf("grok session not found: %s", nativeID)
	}
	return record, nil
}

func (a *GrokBuildAdapter) resolvePublicSession(sessionID string) (*SessionInfo, error) {
	sessions, err := a.Discover()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return nil, fmt.Errorf("grok session not found: %s", sessionID)
}

func firstString(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstTime(values map[string]interface{}, keys ...string) time.Time {
	for _, key := range keys {
		if parsed, ok := parseMessageTime(values[key]); ok {
			return parsed
		}
	}
	return time.Time{}
}

var _ ClosableAdapter = (*GrokBuildAdapter)(nil)
var _ OutputAdapter = (*GrokBuildAdapter)(nil)
var _ NativeThreadStarter = (*GrokBuildAdapter)(nil)
