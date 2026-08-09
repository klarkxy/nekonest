package adapters

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/agentexec"
)

type kimiSessionRecord struct {
	nativeID    string
	sessionDir  string
	projectDir  string
	historyPath string
	legacy      bool
}

type kimiIndexRecord struct {
	nativeID   string
	workDirKey string
	sessionDir string
	projectDir string
	title      string
	updated    time.Time
	archived   bool
}

// KimiCLIAdapter discovers both current Kimi Code and legacy Kimi CLI stores.
type KimiCLIAdapter struct {
	currentHome string
	legacyHomes []string
	mu          sync.RWMutex
	records     map[string]kimiSessionRecord
	stateCache  *fileDiscoveryCache[map[string]interface{}]
	indexCache  *fileDiscoveryCache[map[string]kimiIndexRecord]
	commander   *agentexec.KimiCommander
	watches     pollWatchRegistry
}

// NewKimiCLIAdapter creates a Kimi adapter with migration-compatible roots.
func NewKimiCLIAdapter() *KimiCLIAdapter {
	home, _ := os.UserHomeDir()
	currentHome := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	if currentHome == "" {
		currentHome = filepath.Join(home, ".kimi-code")
	}
	legacyHomes := uniquePaths(
		strings.TrimSpace(os.Getenv("KIMI_SHARE_DIR")),
		filepath.Join(home, ".kimi"),
	)
	return &KimiCLIAdapter{
		currentHome: currentHome,
		legacyHomes: legacyHomes,
		records:     make(map[string]kimiSessionRecord),
		stateCache:  newFileDiscoveryCache[map[string]interface{}](),
		indexCache:  newFileDiscoveryCache[map[string]kimiIndexRecord](),
		commander:   agentexec.NewKimiCommander(),
	}
}

func (a *KimiCLIAdapter) Name() string { return string(AgentKimiCLI) }

func (a *KimiCLIAdapter) IsAvailable() bool {
	return a.commander != nil && a.commander.IsAvailable()
}

func (a *KimiCLIAdapter) ProbeThreadStart(ctx context.Context) ThreadStartCapability {
	if a.commander == nil {
		return ThreadStartCapability{Reason: "Kimi commander is unavailable"}
	}
	if err := a.commander.ProbeThreadStart(ctx); err != nil {
		return ThreadStartCapability{Reason: err.Error()}
	}
	return ThreadStartCapability{Available: true}
}

func (a *KimiCLIAdapter) StartNativeThread(ctx context.Context, request ThreadStartRequest) (ThreadStartResult, error) {
	if a.commander == nil {
		return ThreadStartResult{}, fmt.Errorf("Kimi commander is unavailable")
	}
	nativeID, created, promptAccepted, err := a.commander.StartThread(ctx, request.ProjectDir, request.Prompt)
	return ThreadStartResult{SessionID: publicSessionID(AgentKimiCLI, nativeID), Created: created, PromptAccepted: promptAccepted}, err
}

func (a *KimiCLIAdapter) Close() error {
	a.watches.stopAll()
	if a.commander != nil {
		a.commander.StopAll()
	}
	return nil
}

func (a *KimiCLIAdapter) Discover() ([]*SessionInfo, error) {
	now := time.Now()
	type candidate struct {
		session *SessionInfo
		record  kimiSessionRecord
	}
	candidates := make(map[string]candidate)
	add := func(session *SessionInfo, record kimiSessionRecord) {
		if session == nil || record.nativeID == "" || !sessionIsVisible(session, now) {
			return
		}
		if previous, exists := candidates[record.nativeID]; exists &&
			!session.LastActivity.After(previous.session.LastActivity) {
			return
		}
		candidates[record.nativeID] = candidate{session: session, record: record}
	}

	if err := a.discoverCurrent(add, false); err != nil {
		return nil, err
	}
	for _, root := range a.legacyHomes {
		if err := a.discoverLegacy(root, add, false); err != nil {
			return nil, err
		}
	}

	sessions := make([]*SessionInfo, 0, len(candidates))
	records := make(map[string]kimiSessionRecord, len(candidates))
	for nativeID, item := range candidates {
		sessions = append(sessions, item.session)
		records[nativeID] = item.record
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	a.mu.Lock()
	a.records = records
	a.mu.Unlock()
	a.stateCache.pruneMissing()
	a.stateCache.pruneBefore(recentSessionCutoff(now))
	a.indexCache.pruneMissing()
	return sessions, nil
}

// OwnsSession validates a namespaced Kimi ID against the discovered store.
func (a *KimiCLIAdapter) OwnsSession(sessionID string) bool {
	nativeID, err := nativeSessionID(AgentKimiCLI, sessionID)
	if err != nil {
		return false
	}
	_, err = a.resolveRecord(nativeID)
	return err == nil
}

func (a *KimiCLIAdapter) discoverCurrent(add func(*SessionInfo, kimiSessionRecord), includeOld bool) error {
	index, err := a.readKimiIndex(filepath.Join(a.currentHome, "session_index.jsonl"))
	if err != nil {
		return err
	}
	sessionsRoot := filepath.Join(a.currentHome, "sessions")
	if _, err := os.Stat(sessionsRoot); os.IsNotExist(err) {
		return nil
	}

	seen := make(map[string]bool)
	err = filepath.WalkDir(sessionsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.EqualFold(entry.Name(), "subagents") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(entry.Name(), "state.json") {
			return nil
		}
		relative, err := filepath.Rel(sessionsRoot, path)
		if err != nil || len(strings.FieldsFunc(relative, func(r rune) bool {
			return r == '/' || r == '\\'
		})) != 3 {
			return nil
		}
		sessionDir := filepath.Dir(path)
		nativeID := filepath.Base(sessionDir)
		item := index[nativeID]
		if !includeOld {
			candidateTime := latestFileTime(
				path,
				filepath.Join(sessionDir, "agents", "main", "wire.jsonl"),
				filepath.Join(sessionDir, "context.jsonl"),
				filepath.Join(sessionDir, "wire.jsonl"),
			)
			if item.updated.After(candidateTime) {
				candidateTime = item.updated
			}
			if candidateTime.Before(recentSessionCutoff(time.Now())) {
				return nil
			}
		}
		state, _ := a.readKimiState(path)
		nativeID = firstString(state, "id", "session_id", "sessionId")
		if nativeID == "" {
			nativeID = filepath.Base(sessionDir)
		}
		if nativeID == "" {
			return nil
		}
		seen[nativeID] = true
		item = index[nativeID]
		session, record := kimiSessionFromState(nativeID, sessionDir, state, item)
		add(session, record)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk kimi-code sessions: %w", err)
	}

	// Some Kimi Code versions write the index before the first state snapshot.
	for nativeID, item := range index {
		if seen[nativeID] {
			continue
		}
		sessionDir := item.sessionDir
		if sessionDir == "" {
			sessionDir = filepath.Join(sessionsRoot, item.workDirKey, nativeID)
		}
		if st, err := os.Stat(sessionDir); err != nil || !st.IsDir() {
			continue
		}
		if !includeOld {
			candidateTime := latestFileTime(
				filepath.Join(sessionDir, "state.json"),
				filepath.Join(sessionDir, "agents", "main", "wire.jsonl"),
				filepath.Join(sessionDir, "context.jsonl"),
				filepath.Join(sessionDir, "wire.jsonl"),
			)
			if item.updated.After(candidateTime) {
				candidateTime = item.updated
			}
			if candidateTime.Before(recentSessionCutoff(time.Now())) {
				continue
			}
		}
		session, record := kimiSessionFromState(nativeID, sessionDir, nil, item)
		add(session, record)
	}
	return nil
}

func (a *KimiCLIAdapter) discoverLegacy(root string, add func(*SessionInfo, kimiSessionRecord), includeOld bool) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	sessionsRoot := filepath.Join(root, "sessions")
	if _, err := os.Stat(sessionsRoot); os.IsNotExist(err) {
		return nil
	}

	workDirs, err := readLegacyKimiWorkDirs(filepath.Join(root, "kimi.json"))
	if err != nil {
		return err
	}
	if len(workDirs) == 0 {
		entries, err := os.ReadDir(sessionsRoot)
		if err != nil {
			return fmt.Errorf("read legacy kimi sessions: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				workDirs = append(workDirs, legacyKimiWorkDir{key: entry.Name()})
			}
		}
	}

	for _, workDir := range workDirs {
		base := filepath.Join(sessionsRoot, workDir.key)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.EqualFold(entry.Name(), "subagents") {
				continue
			}
			nativeID := entry.Name()
			sessionDir := filepath.Join(base, nativeID)
			statePath := filepath.Join(sessionDir, "state.json")
			historyPath := firstExistingFile(
				filepath.Join(sessionDir, "context.jsonl"),
				filepath.Join(sessionDir, "wire.jsonl"),
			)
			if !includeOld && latestFileTime(statePath, historyPath).Before(recentSessionCutoff(time.Now())) {
				continue
			}
			state, _ := a.readKimiState(statePath)
			if boolField(state, "archived", "is_archived", "isArchived") {
				continue
			}
			title := firstString(state, "custom_title", "customTitle", "title", "name")
			updated := firstTime(state, "updated_at", "updatedAt", "wire_mtime", "wireMtime", "created_at", "createdAt")
			if fileTime := latestFileTime(
				statePath,
				historyPath,
			); fileTime.After(updated) {
				updated = fileTime
			}
			if updated.IsZero() {
				updated = time.Now()
			}
			if title == "" {
				title = nativeID
			}
			add(newKimiSession(nativeID, sessionDir, workDir.path, title, updated), kimiSessionRecord{
				nativeID:    nativeID,
				sessionDir:  sessionDir,
				projectDir:  workDir.path,
				historyPath: historyPath,
				legacy:      true,
			})
		}
	}
	return nil
}

func kimiSessionFromState(nativeID, sessionDir string, state map[string]interface{}, item kimiIndexRecord) (*SessionInfo, kimiSessionRecord) {
	if boolField(state, "archived", "is_archived", "isArchived") || item.archived {
		return nil, kimiSessionRecord{}
	}
	projectDir := firstString(state, "work_dir", "workDir", "cwd", "project_dir", "projectDir")
	if projectDir == "" {
		projectDir = item.projectDir
	}
	title := firstString(state, "custom_title", "customTitle", "title", "name")
	if title == "" {
		title = item.title
	}
	if title == "" {
		title = nativeID
	}
	updated := firstTime(state, "updated_at", "updatedAt", "wire_mtime", "wireMtime", "created_at", "createdAt")
	if item.updated.After(updated) {
		updated = item.updated
	}
	historyPath := firstExistingFile(
		filepath.Join(sessionDir, "agents", "main", "wire.jsonl"),
		filepath.Join(sessionDir, "context.jsonl"),
		filepath.Join(sessionDir, "wire.jsonl"),
	)
	if fileTime := latestFileTime(filepath.Join(sessionDir, "state.json"), historyPath); fileTime.After(updated) {
		updated = fileTime
	}
	if updated.IsZero() {
		updated = time.Now()
	}
	return newKimiSession(nativeID, sessionDir, projectDir, title, updated), kimiSessionRecord{
		nativeID:    nativeID,
		sessionDir:  sessionDir,
		projectDir:  projectDir,
		historyPath: historyPath,
	}
}

func newKimiSession(nativeID, sessionDir, projectDir, title string, updated time.Time) *SessionInfo {
	status := StatusIdle
	if time.Since(updated) < time.Minute {
		status = StatusRunning
	}
	return &SessionInfo{
		ID:           publicSessionID(AgentKimiCLI, nativeID),
		AgentType:    AgentKimiCLI,
		Status:       status,
		Summary:      truncateRunes(strings.TrimSpace(title), 120),
		LastActivity: updated,
		SessionPath:  sessionDir,
		ProjectDir:   projectDir,
	}
}

func (a *KimiCLIAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	nativeID, err := nativeSessionID(AgentKimiCLI, sessionID)
	if err != nil {
		return nil, err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		return nil, err
	}
	if record.historyPath == "" {
		return nil, nil
	}
	file, err := os.Open(record.historyPath)
	if err != nil {
		return nil, fmt.Errorf("open kimi history: %w", err)
	}
	defer file.Close()

	var messages []*HistoryMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	messageIndex := 0
	turnIndex := 0
	var assistantTurn *HistoryMessage
	nextMessageID := func(messageID string) string {
		messageIndex++
		if messageID != "" {
			return messageID
		}
		return fmt.Sprintf("kimi_%s_%d", nativeID, messageIndex)
	}
	flushAssistantTurn := func() {
		if assistantTurn == nil {
			return
		}
		assistantTurn.Content = truncateRunes(strings.TrimSpace(assistantTurn.Content), 4000)
		if assistantTurn.Content != "" {
			messages = append(messages, assistantTurn)
		}
		assistantTurn = nil
	}
	for scanner.Scan() {
		var item map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &item) != nil {
			continue
		}
		role, content, timestamp, messageID, eventType := parseKimiHistoryItem(item)
		if !record.legacy {
			switch eventType {
			case "turnbegin", "steerinput":
				flushAssistantTurn()
				turnIndex++
				if role == "user" && content != "" {
					if timestamp.IsZero() {
						timestamp = time.Now()
					}
					messages = append(messages, &HistoryMessage{
						ID:        nextMessageID(messageID),
						Role:      "user",
						Content:   truncateRunes(strings.TrimSpace(content), 4000),
						Type:      "text",
						Timestamp: timestamp.Unix(),
					})
				}
				continue
			case "step.begin":
				flushAssistantTurn()
				turnIndex++
				continue
			case "textpart", "content.part":
				if content == "" {
					continue
				}
				if timestamp.IsZero() {
					timestamp = time.Now()
				}
				if assistantTurn == nil {
					if turnIndex == 0 {
						turnIndex = 1
					}
					if messageID == "" {
						messageID = fmt.Sprintf("kimi_%s_turn_%d_assistant", nativeID, turnIndex)
					}
					assistantTurn = &HistoryMessage{
						ID:        nextMessageID(messageID),
						Role:      "assistant",
						Type:      "text",
						Timestamp: timestamp.Unix(),
					}
				}
				assistantTurn.Content += content
				continue
			case "turnend", "step.end":
				flushAssistantTurn()
				continue
			case "tool.call", "tool.result":
				// Tool events delimit model work but are not chat text. The
				// surrounding step.begin/step.end owns assistant aggregation.
				continue
			}
		}
		if (role != "user" && role != "assistant") || content == "" {
			continue
		}
		flushAssistantTurn()
		if timestamp.IsZero() {
			timestamp = time.Now()
		}
		messages = append(messages, &HistoryMessage{
			ID:        nextMessageID(messageID),
			Role:      role,
			Content:   truncateRunes(strings.TrimSpace(content), 4000),
			Type:      "text",
			Timestamp: timestamp.Unix(),
		})
	}
	flushAssistantTurn()
	return takeLastHistory(messages, limit), scanner.Err()
}

func parseKimiHistoryItem(item map[string]interface{}) (string, string, time.Time, string, string) {
	role := strings.ToLower(firstString(item, "role"))
	content := strings.TrimSpace(contentToText(item["content"]))
	preserveFragmentWhitespace := false
	timestamp := firstTime(item, "time", "timestamp", "created_at", "createdAt", "updated_at", "updatedAt")
	messageID := firstString(item, "id", "message_id", "messageId")
	eventType := strings.ToLower(firstString(item, "type", "event_type", "eventType"))

	// Kimi Code wire protocol v1.4+ stores object payload fields at the top
	// level: {type:"context.append_*", message|event:{...}, time:<epoch-ms>}.
	// Accept a nested payload too so older prerelease writers remain readable.
	if eventType == "context.append_message" {
		container := item
		if payload, ok := item["payload"].(map[string]interface{}); ok {
			if _, flattened := container["message"]; !flattened {
				container = payload
			}
		}
		message, _ := container["message"].(map[string]interface{})
		role = strings.ToLower(firstString(message, "role"))
		content = contentToText(message["content"])
		messageID = firstString(message, "id", "message_id", "messageId", "providerMessageId")
		return role, strings.TrimSpace(content), timestamp, messageID, eventType
	}
	if eventType == "context.append_loop_event" {
		container := item
		if payload, ok := item["payload"].(map[string]interface{}); ok {
			if _, flattened := container["event"]; !flattened {
				container = payload
			}
		}
		event, _ := container["event"].(map[string]interface{})
		eventType = strings.ToLower(firstString(event, "type"))
		messageID = firstString(event, "uuid", "stepUuid", "messageId")
		if eventType == "content.part" {
			part, _ := event["part"].(map[string]interface{})
			if strings.EqualFold(firstString(part, "type"), "text") {
				role = "assistant"
				content, _ = part["text"].(string)
				preserveFragmentWhitespace = true
			} else {
				// Thinking and media parts stay out of imported chat text.
				role = ""
				content = ""
			}
		} else {
			role = ""
			content = ""
		}
		if preserveFragmentWhitespace {
			return role, content, timestamp, messageID, eventType
		}
		return role, strings.TrimSpace(content), timestamp, messageID, eventType
	}

	for _, key := range []string{"message", "payload", "event"} {
		nested, ok := item[key].(map[string]interface{})
		if !ok {
			continue
		}
		if role == "" {
			role = strings.ToLower(firstString(nested, "role"))
		}
		if content == "" {
			content = strings.TrimSpace(contentToText(nested["content"]))
			if content == "" {
				content = firstString(nested, "text", "user_input", "userInput", "message")
			}
		}
		if timestamp.IsZero() {
			timestamp = firstTime(nested, "timestamp", "created_at", "createdAt", "updated_at", "updatedAt")
		}
		if messageID == "" {
			messageID = firstString(nested, "id", "message_id", "messageId")
		}
		if eventType == "" {
			eventType = strings.ToLower(firstString(nested, "type", "event_type", "eventType"))
		}

		// Persisted wire records use:
		// {"timestamp":...,"message":{"type":"TurnBegin|TextPart","payload":{...}}}
		payload, _ := nested["payload"].(map[string]interface{})
		if payload == nil {
			continue
		}
		switch eventType {
		case "turnbegin", "steerinput":
			role = "user"
			content = kimiUserInputText(payload)
		case "textpart":
			role = "assistant"
			preserveFragmentWhitespace = true
			if text, ok := payload["text"].(string); ok {
				content = text
			} else {
				content = contentToText(payload["content"])
			}
		case "thinkpart":
			// Thinking is intentionally omitted from imported chat history.
			role = ""
			content = ""
		}
		if messageID == "" {
			messageID = firstString(payload, "id", "message_id", "messageId")
		}
	}
	if role == "" && (strings.Contains(eventType, "turnbegin") || strings.Contains(eventType, "user")) {
		role = "user"
		if content == "" {
			content = firstString(item, "user_input", "userInput", "text", "message")
		}
	}
	if role == "" && (strings.Contains(eventType, "assistant") || strings.Contains(eventType, "contentpart")) {
		role = "assistant"
	}
	if preserveFragmentWhitespace {
		return role, content, timestamp, messageID, eventType
	}
	return role, strings.TrimSpace(content), timestamp, messageID, eventType
}

func kimiUserInputText(payload map[string]interface{}) string {
	input, ok := payload["user_input"]
	if !ok {
		input = payload["userInput"]
	}
	if text := strings.TrimSpace(contentToText(input)); text != "" {
		return text
	}
	if text, ok := input.(string); ok {
		return strings.TrimSpace(text)
	}
	return firstString(payload, "text", "content", "message")
}

func (a *KimiCLIAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
	if _, err := nativeSessionID(AgentKimiCLI, sessionID); err != nil {
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

func (a *KimiCLIAdapter) SendPrompt(sessionID string, request PromptRequest) error {
	nativeID, err := nativeSessionID(AgentKimiCLI, sessionID)
	if err != nil {
		return err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		return err
	}
	if record.legacy {
		return a.commander.SendLegacyPromptInDir(
			nativeID,
			request.Prompt,
			record.projectDir,
			request.Attachments,
			request.OnComplete,
		)
	}
	return a.commander.SendPromptInDir(
		nativeID,
		request.Prompt,
		record.projectDir,
		request.Attachments,
		request.OnComplete,
	)
}

func (a *KimiCLIAdapter) Approve(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentKimiCLI, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Approve(nativeID, approvalID)
}

func (a *KimiCLIAdapter) Deny(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentKimiCLI, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Deny(nativeID, approvalID)
}

func (a *KimiCLIAdapter) Interrupt(sessionID string) error {
	nativeID, err := nativeSessionID(AgentKimiCLI, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Interrupt(nativeID)
}

func (a *KimiCLIAdapter) SetOutputSink(sink OutputSink) {
	if a.commander == nil {
		return
	}
	if sink == nil {
		a.commander.OnAgentOutput = nil
		return
	}
	a.commander.OnAgentOutput = func(nativeID, msgType, content, messageID string) {
		sink(OutputEvent{
			SessionID: publicSessionID(AgentKimiCLI, nativeID),
			AgentType: AgentKimiCLI,
			Type:      msgType,
			Content:   content,
			MessageID: messageID,
		})
	}
}

func (a *KimiCLIAdapter) resolveRecord(nativeID string) (kimiSessionRecord, error) {
	a.mu.RLock()
	record, ok := a.records[nativeID]
	a.mu.RUnlock()
	if ok {
		return record, nil
	}
	if _, err := a.Discover(); err != nil {
		return kimiSessionRecord{}, err
	}
	a.mu.RLock()
	record, ok = a.records[nativeID]
	a.mu.RUnlock()
	if !ok {
		var exact kimiSessionRecord
		capture := func(_ *SessionInfo, candidate kimiSessionRecord) {
			if candidate.nativeID == nativeID {
				exact = candidate
			}
		}
		_ = a.discoverCurrent(capture, true)
		if exact.nativeID == "" {
			for _, root := range a.legacyHomes {
				_ = a.discoverLegacy(root, capture, true)
				if exact.nativeID != "" {
					break
				}
			}
		}
		if exact.nativeID == "" {
			return kimiSessionRecord{}, fmt.Errorf("kimi session not found: %s", nativeID)
		}
		return exact, nil
	}
	return record, nil
}

func (a *KimiCLIAdapter) readKimiState(path string) (map[string]interface{}, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return a.stateCache.load(path, info, func() (map[string]interface{}, error) {
		return readJSONMap(path)
	})
}

func (a *KimiCLIAdapter) readKimiIndex(path string) (map[string]kimiIndexRecord, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return make(map[string]kimiIndexRecord), nil
	}
	if err != nil {
		return nil, err
	}
	return a.indexCache.load(path, info, func() (map[string]kimiIndexRecord, error) {
		return readKimiIndex(path)
	})
}

func (a *KimiCLIAdapter) resolvePublicSession(sessionID string) (*SessionInfo, error) {
	sessions, err := a.Discover()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return nil, fmt.Errorf("kimi session not found: %s", sessionID)
}

func readKimiIndex(path string) (map[string]kimiIndexRecord, error) {
	records := make(map[string]kimiIndexRecord)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return records, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open kimi session index: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		var item map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &item) != nil {
			continue
		}
		nativeID := firstString(item, "id", "session_id", "sessionId")
		if nested, ok := item["session"].(map[string]interface{}); ok && nativeID == "" {
			nativeID = firstString(nested, "id", "session_id", "sessionId")
		}
		if nativeID == "" {
			continue
		}
		records[nativeID] = kimiIndexRecord{
			nativeID:   nativeID,
			workDirKey: firstString(item, "work_dir_key", "workDirKey", "workspace_key", "workspaceKey"),
			sessionDir: firstString(item, "session_dir", "sessionDir"),
			projectDir: firstString(item, "work_dir", "workDir", "cwd", "project_dir", "projectDir"),
			title:      firstString(item, "title", "name", "custom_title", "customTitle"),
			updated:    firstTime(item, "updated_at", "updatedAt", "last_activity", "lastActivity", "created_at", "createdAt"),
			archived:   boolField(item, "archived", "is_archived", "isArchived"),
		}
	}
	return records, scanner.Err()
}

type legacyKimiWorkDir struct {
	path string
	key  string
}

func readLegacyKimiWorkDirs(path string) ([]legacyKimiWorkDir, error) {
	document, err := readJSONMap(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy kimi metadata: %w", err)
	}
	raw, _ := document["work_dirs"].([]interface{})
	var result []legacyKimiWorkDir
	for _, value := range raw {
		item, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		workDir := firstString(item, "path", "work_dir", "workDir", "cwd")
		if workDir == "" {
			continue
		}
		sum := md5.Sum([]byte(workDir))
		key := hex.EncodeToString(sum[:])
		kaos := firstString(item, "kaos")
		if kaos != "" && !strings.EqualFold(kaos, "local") {
			key = kaos + "_" + key
		}
		result = append(result, legacyKimiWorkDir{path: workDir, key: key})
	}
	return result, nil
}

func readJSONMap(path string) (map[string]interface{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func boolField(values map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if value, ok := values[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func latestFileTime(paths ...string) time.Time {
	var latest time.Time
	for _, path := range paths {
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func uniquePaths(paths ...string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		key := strings.ToLower(cleaned)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, cleaned)
	}
	return result
}

var _ ClosableAdapter = (*KimiCLIAdapter)(nil)
var _ OutputAdapter = (*KimiCLIAdapter)(nil)
var _ NativeThreadStarter = (*KimiCLIAdapter)(nil)
