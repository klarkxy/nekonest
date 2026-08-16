package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nekonest/daemon/internal/agentexec"
)

type cursorSessionRecord struct {
	nativeID       string
	sessionDir     string
	projectDir     string
	storePath      string
	transcriptPath string
	title          string
	updated        time.Time
}

// CursorAdapter discovers Cursor Agent CLI sessions from ~/.cursor/chats
// and ~/.cursor/projects/*/agent-transcripts. Desktop composer rows without
// a CLI-resumable transcript are not treated as owned native threads.
type CursorAdapter struct {
	chatsRoot    string
	projectsRoot string
	mu           sync.RWMutex
	records      map[string]cursorSessionRecord
	commander    *agentexec.CursorCommander
	watches      pollWatchRegistry
	turns        turnTracker
}

func NewCursorAdapter() *CursorAdapter {
	home, _ := os.UserHomeDir()
	root := strings.TrimSpace(os.Getenv("CURSOR_HOME"))
	if root == "" {
		root = filepath.Join(home, ".cursor")
	}
	a := &CursorAdapter{
		chatsRoot:    filepath.Join(root, "chats"),
		projectsRoot: filepath.Join(root, "projects"),
		records:      make(map[string]cursorSessionRecord),
		commander:    agentexec.NewCursorCommander(),
		turns:        newTurnTracker(AgentCursor),
	}
	a.commander.OnTurnEnd = func(nativeID string, exitCode int, interrupted bool) {
		a.turns.finish(publicSessionID(AgentCursor, nativeID), exitCode, interrupted)
	}
	return a
}

func (a *CursorAdapter) Name() string { return string(AgentCursor) }

func (a *CursorAdapter) IsAvailable() bool {
	return a.commander != nil && a.commander.IsAvailable()
}

func (a *CursorAdapter) UnavailableReason() string {
	if a.IsAvailable() {
		return ""
	}
	return "Cursor Agent CLI (cursor-agent) not found"
}

func (a *CursorAdapter) ProbeThreadStart(ctx context.Context) ThreadStartCapability {
	if a.commander == nil {
		return ThreadStartCapability{Reason: "Cursor commander is unavailable"}
	}
	if err := a.commander.ProbeThreadStart(ctx); err != nil {
		return ThreadStartCapability{Reason: err.Error()}
	}
	return ThreadStartCapability{Available: true, ControlPath: "cli", AttachmentMode: AttachPathBestEffort}
}

func (a *CursorAdapter) StartNativeThread(ctx context.Context, request ThreadStartRequest) (ThreadStartResult, error) {
	if a.commander == nil {
		return ThreadStartResult{}, fmt.Errorf("Cursor commander is unavailable")
	}
	nativeID, created, promptAccepted, err := a.commander.StartThread(
		ctx, request.ProjectDir, request.Prompt, request.Attachments, request.OnComplete,
	)
	return ThreadStartResult{
		SessionID:      publicSessionID(AgentCursor, nativeID),
		Created:        created,
		PromptAccepted: promptAccepted,
	}, err
}

func (a *CursorAdapter) Close() error {
	a.turns.detachAll()
	a.watches.stopAll()
	if a.commander != nil {
		a.commander.StopAll()
	}
	return nil
}

func (a *CursorAdapter) Discover() ([]*SessionInfo, error) {
	if !a.IsAvailable() {
		a.mu.Lock()
		a.records = make(map[string]cursorSessionRecord)
		a.mu.Unlock()
		return nil, nil
	}

	now := time.Now()
	records := make(map[string]cursorSessionRecord)
	sessions := make([]*SessionInfo, 0)
	if err := a.discoverCursorChats(now, records, &sessions); err != nil {
		return nil, err
	}
	if err := a.discoverCursorTranscripts(now, records, &sessions); err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	a.mu.Lock()
	a.records = records
	a.mu.Unlock()
	return sessions, nil
}

func (a *CursorAdapter) discoverCursorChats(now time.Time, records map[string]cursorSessionRecord, sessions *[]*SessionInfo) error {
	if strings.TrimSpace(a.chatsRoot) == "" {
		return nil
	}
	if _, err := os.Stat(a.chatsRoot); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat cursor chats: %w", err)
	}
	err := filepath.WalkDir(a.chatsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if name != "meta.json" && name != "store.db" {
			return nil
		}
		sessionDir := filepath.Dir(path)
		if info, infoErr := entry.Info(); infoErr == nil && info.ModTime().Before(recentSessionCutoff(now)) {
			return nil
		}
		nativeID := filepath.Base(sessionDir)
		if !looksLikeCursorNativeID(nativeID) {
			return nil
		}
		if _, exists := records[nativeID]; exists {
			return nil
		}
		record, session, err := a.readCursorSession(sessionDir, nativeID)
		if err != nil || session == nil || !sessionIsVisible(session, now) {
			return nil
		}
		records[nativeID] = record
		*sessions = append(*sessions, session)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk cursor chats: %w", err)
	}
	return nil
}

func (a *CursorAdapter) discoverCursorTranscripts(now time.Time, records map[string]cursorSessionRecord, sessions *[]*SessionInfo) error {
	if strings.TrimSpace(a.projectsRoot) == "" {
		return nil
	}
	entries, err := os.ReadDir(a.projectsRoot)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read cursor projects: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		slug := entry.Name()
		projectDir := resolveCursorWorkspaceSlug(slug)
		transcriptsRoot := filepath.Join(a.projectsRoot, slug, "agent-transcripts")
		kids, err := os.ReadDir(transcriptsRoot)
		if err != nil {
			continue
		}
		for _, kid := range kids {
			if !kid.IsDir() {
				continue
			}
			nativeID := kid.Name()
			if !looksLikeCursorNativeID(nativeID) || cursorTranscriptHidden(nativeID) {
				continue
			}
			jsonl := filepath.Join(transcriptsRoot, nativeID, nativeID+".jsonl")
			a.mergeCursorTranscript(now, records, sessions, nativeID, jsonl, projectDir)
		}
	}
	return nil
}

func cursorTranscriptHidden(nativeID string) bool {
	return strings.Contains(strings.ToLower(nativeID), "subagent")
}

func (a *CursorAdapter) mergeCursorTranscript(
	now time.Time,
	records map[string]cursorSessionRecord,
	sessions *[]*SessionInfo,
	nativeID, jsonl, projectDir string,
) {
	info, err := os.Stat(jsonl)
	if err != nil {
		return
	}
	if info.ModTime().Before(recentSessionCutoff(now)) && records[nativeID].nativeID == "" {
		return
	}
	if rec, ok := records[nativeID]; ok {
		if rec.transcriptPath == "" {
			rec.transcriptPath = jsonl
		}
		if rec.projectDir == "" && projectDir != "" {
			rec.projectDir = projectDir
			publicID := publicSessionID(AgentCursor, nativeID)
			for _, session := range *sessions {
				if session.ID == publicID && session.ProjectDir == "" {
					session.ProjectDir = projectDir
				}
			}
		}
		if (rec.title == "" || rec.title == nativeID) && !info.ModTime().Before(recentSessionCutoff(now)) {
			if title := cursorSummaryFromHistory(readCursorJSONL(jsonl, nativeID, info.ModTime()), nativeID); title != "" && title != nativeID {
				rec.title = title
				publicID := publicSessionID(AgentCursor, nativeID)
				for _, session := range *sessions {
					if session.ID == publicID && (session.Summary == "" || session.Summary == nativeID) {
						session.Summary = title
					}
				}
			}
		}
		records[nativeID] = rec
		return
	}
	updated := info.ModTime()
	messages := readCursorJSONL(jsonl, nativeID, updated)
	if len(messages) == 0 {
		return
	}
	title := cursorSummaryFromHistory(messages, nativeID)
	status := StatusIdle
	if time.Since(updated) < time.Minute {
		status = StatusRunning
	}
	session := &SessionInfo{
		ID:           publicSessionID(AgentCursor, nativeID),
		AgentType:    AgentCursor,
		Status:       status,
		Summary:      title,
		LastActivity: updated,
		SessionPath:  filepath.Dir(jsonl),
		ProjectDir:   projectDir,
	}
	if !sessionIsVisible(session, now) {
		return
	}
	records[nativeID] = cursorSessionRecord{
		nativeID:       nativeID,
		sessionDir:     filepath.Dir(jsonl),
		projectDir:     projectDir,
		transcriptPath: jsonl,
		title:          title,
		updated:        updated,
	}
	*sessions = append(*sessions, session)
}

func looksLikeCursorNativeID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		return true
	}
	return len(id) >= 8 && !strings.ContainsAny(id, `/\`)
}

func (a *CursorAdapter) readCursorSession(sessionDir, nativeID string) (cursorSessionRecord, *SessionInfo, error) {
	metaPath := filepath.Join(sessionDir, "meta.json")
	storePath := filepath.Join(sessionDir, "store.db")
	title, cwd, updated := cursorMetaFromJSON(metaPath)
	if storeTitle, storeCWD, storeUpdated, ok := cursorMetaFromStore(storePath); ok {
		if title == "" {
			title = storeTitle
		}
		if cwd == "" {
			cwd = storeCWD
		}
		if storeUpdated.After(updated) {
			updated = storeUpdated
		}
	}
	if updated.IsZero() {
		if info, err := os.Stat(storePath); err == nil {
			updated = info.ModTime()
		} else if info, err := os.Stat(metaPath); err == nil {
			updated = info.ModTime()
		} else if info, err := os.Stat(sessionDir); err == nil {
			updated = info.ModTime()
		}
	}
	if title == "" {
		title = nativeID
	}
	status := StatusIdle
	if time.Since(updated) < time.Minute {
		status = StatusRunning
	}
	record := cursorSessionRecord{
		nativeID:   nativeID,
		sessionDir: sessionDir,
		projectDir: cwd,
		storePath:  storePath,
		title:      title,
		updated:    updated,
	}
	return record, &SessionInfo{
		ID:           publicSessionID(AgentCursor, nativeID),
		AgentType:    AgentCursor,
		Status:       status,
		Summary:      truncateRunes(title, 120),
		LastActivity: updated,
		SessionPath:  sessionDir,
		ProjectDir:   cwd,
	}, nil
}

func cursorMetaFromJSON(path string) (title, cwd string, updated time.Time) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", time.Time{}
	}
	var payload map[string]interface{}
	if json.Unmarshal(raw, &payload) != nil {
		return "", "", time.Time{}
	}
	return firstString(payload, "title", "name"),
		firstString(payload, "cwd", "workspacePath"),
		firstTime(payload, "updatedAtMs", "updated_at_ms", "updatedAt", "updated_at")
}

func cursorMetaFromStore(path string) (title, cwd string, updated time.Time, ok bool) {
	if _, err := os.Stat(path); err != nil {
		return "", "", time.Time{}, false
	}
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return "", "", time.Time{}, false
	}
	defer db.Close()
	rows, err := db.Query(`SELECT key, value FROM meta`)
	if err != nil {
		return "", "", time.Time{}, false
	}
	defer rows.Close()
	values := map[string]interface{}{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			continue
		}
		decoded := cursorDecodeJSON(raw)
		if decoded == nil {
			values[key] = string(raw)
			continue
		}
		if nested, ok := decoded.(map[string]interface{}); ok {
			for k, v := range nested {
				if _, exists := values[k]; !exists {
					values[k] = v
				}
			}
		}
		values[key] = decoded
	}
	title = firstString(values, "title", "name")
	cwd = firstString(values, "cwd", "workspacePath")
	updated = firstTime(values, "updatedAtMs", "updated_at_ms", "updatedAt", "updated_at")
	return title, cwd, updated, title != "" || cwd != "" || !updated.IsZero()
}

func cursorDecodeJSON(raw []byte) interface{} {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	var value interface{}
	if json.Unmarshal([]byte(text), &value) == nil {
		return value
	}
	return nil
}

func (a *CursorAdapter) OwnsSession(sessionID string) bool {
	nativeID, err := nativeSessionID(AgentCursor, sessionID)
	if err != nil {
		return false
	}
	_, err = a.resolveRecord(nativeID)
	return err == nil
}

func (a *CursorAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	nativeID, err := nativeSessionID(AgentCursor, sessionID)
	if err != nil {
		return nil, err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		return nil, err
	}
	messages := a.readCursorStoreHistory(record.storePath, nativeID, record.updated)
	if len(messages) == 0 {
		messages = a.readCursorTranscriptHistory(record)
	}
	return takeLastHistory(messages, limit), nil
}

func (a *CursorAdapter) readCursorStoreHistory(storePath, nativeID string, fallback time.Time) []*HistoryMessage {
	if _, err := os.Stat(storePath); err != nil {
		return nil
	}
	db, err := openReadOnlySQLite(storePath)
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, data FROM blobs ORDER BY rowid`)
	if err != nil {
		rows, err = db.Query(`SELECT key, value FROM blobs ORDER BY rowid`)
		if err != nil {
			return nil
		}
	}
	defer rows.Close()
	var messages []*HistoryMessage
	index := 0
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			continue
		}
		value := cursorDecodeJSON(raw)
		obj, _ := value.(map[string]interface{})
		if obj == nil {
			continue
		}
		role, text := cursorTurnFromValue(obj)
		if (role != "user" && role != "assistant") || text == "" {
			continue
		}
		index++
		timestamp := firstTime(obj, "timestamp", "created_at", "createdAt", "updatedAtMs")
		if timestamp.IsZero() {
			timestamp = fallback
		}
		messageID := strings.TrimSpace(key)
		if messageID == "" {
			messageID = fmt.Sprintf("cursor_%s_%d", nativeID, index)
		}
		messages = append(messages, &HistoryMessage{
			ID:        messageID,
			Role:      role,
			Content:   truncateRunes(text, 4000),
			Type:      "text",
			Timestamp: timestamp.Unix(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	sortHistoryMessages(messages)
	return messages
}

func cursorTurnFromValue(value map[string]interface{}) (string, string) {
	role := strings.ToLower(firstString(value, "role"))
	if role == "system" || role == "developer" || role == "instruction" {
		return "", ""
	}
	if nested, ok := value["message"].(map[string]interface{}); ok {
		if role == "" {
			role = strings.ToLower(firstString(nested, "role"))
		}
		if text := strings.TrimSpace(contentToText(nested["content"])); text != "" {
			return normalizeCursorRole(role), text
		}
	}
	text := strings.TrimSpace(contentToText(value["content"]))
	if text == "" {
		text = strings.TrimSpace(firstString(value, "text"))
	}
	return normalizeCursorRole(role), text
}

func normalizeCursorRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human":
		return "user"
	case "assistant", "model", "ai":
		return "assistant"
	default:
		return ""
	}
}

func (a *CursorAdapter) readCursorTranscriptHistory(record cursorSessionRecord) []*HistoryMessage {
	if record.transcriptPath != "" {
		if messages := readCursorJSONL(record.transcriptPath, record.nativeID, record.updated); len(messages) > 0 {
			return messages
		}
	}
	if strings.TrimSpace(a.projectsRoot) == "" {
		return nil
	}
	entries, err := os.ReadDir(a.projectsRoot)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(a.projectsRoot, entry.Name(), "agent-transcripts", record.nativeID, record.nativeID+".jsonl")
		messages := readCursorJSONL(path, record.nativeID, record.updated)
		if len(messages) > 0 {
			return messages
		}
	}
	return nil
}

func cursorSummaryFromHistory(messages []*HistoryMessage, fallback string) string {
	for _, message := range messages {
		if message == nil || message.Role != "user" {
			continue
		}
		if title := cursorCollapsedTitle(message.Content); title != "" {
			return title
		}
	}
	return truncateRunes(strings.TrimSpace(fallback), 120)
}

func cursorCollapsedTitle(text string) string {
	text = strings.TrimSpace(text)
	if query := cursorTaggedText(text, "user_query"); query != "" {
		text = query
	}
	return truncateRunes(strings.Join(strings.Fields(text), " "), 120)
}

func cursorTaggedText(text, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	lower := strings.ToLower(text)
	start := strings.Index(lower, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(lower[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(lower[start : start+end])
}

func readCursorJSONL(path, nativeID string, fallback time.Time) []*HistoryMessage {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var messages []*HistoryMessage
	index := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item map[string]interface{}
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		role, text := cursorTurnFromValue(item)
		if (role != "user" && role != "assistant") || text == "" {
			continue
		}
		index++
		timestamp := firstTime(item, "timestamp", "created_at", "createdAt")
		if timestamp.IsZero() {
			timestamp = fallback
		}
		messages = append(messages, &HistoryMessage{
			ID:        fmt.Sprintf("cursor_%s_%d", nativeID, index),
			Role:      role,
			Content:   truncateRunes(text, 4000),
			Type:      "text",
			Timestamp: timestamp.Unix(),
		})
	}
	sortHistoryMessages(messages)
	return messages
}

func (a *CursorAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
	if _, err := nativeSessionID(AgentCursor, sessionID); err != nil {
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

func (a *CursorAdapter) SendPrompt(sessionID string, request PromptRequest) error {
	if !a.turns.begin(sessionID, request) {
		return agentexec.ErrSessionBusy
	}
	nativeID, err := nativeSessionID(AgentCursor, sessionID)
	if err != nil {
		a.turns.abort(sessionID, request.Generation)
		return err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		a.turns.abort(sessionID, request.Generation)
		return err
	}
	if strings.TrimSpace(record.projectDir) == "" {
		a.turns.abort(sessionID, request.Generation)
		return fmt.Errorf("cursor session has no resolved project directory")
	}
	err = a.commander.SendPromptInDir(nativeID, request.Prompt, record.projectDir, request.Attachments, request.OnComplete)
	if err != nil {
		a.turns.abort(sessionID, request.Generation)
		return err
	}
	if !request.DeferAcceptance {
		a.turns.accepted(sessionID, request.Generation)
	}
	return nil
}

func (a *CursorAdapter) AcknowledgePrompt(sessionID string, generation uint64) {
	a.turns.accepted(sessionID, generation)
}
func (a *CursorAdapter) AbandonPrompt(sessionID string, generation uint64) {
	a.turns.abort(sessionID, generation)
}
func (a *CursorAdapter) SetControlSink(sink func(ControlEvent)) { a.turns.setSink(sink) }

func (a *CursorAdapter) Approve(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentCursor, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Approve(nativeID, approvalID)
}

func (a *CursorAdapter) Deny(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentCursor, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Deny(nativeID, approvalID)
}

func (a *CursorAdapter) Interrupt(sessionID string) error {
	nativeID, err := nativeSessionID(AgentCursor, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Interrupt(nativeID)
}

func (a *CursorAdapter) SetOutputSink(sink OutputSink) {
	if a.commander == nil {
		return
	}
	if sink == nil {
		a.commander.OnAgentOutput = nil
		return
	}
	a.commander.OnAgentOutput = func(nativeID, msgType, content, messageID string) {
		sink(OutputEvent{
			SessionID: publicSessionID(AgentCursor, nativeID),
			AgentType: AgentCursor,
			Type:      msgType,
			Content:   content,
			MessageID: messageID,
		})
	}
}

func (a *CursorAdapter) resolveRecord(nativeID string) (cursorSessionRecord, error) {
	a.mu.RLock()
	record, ok := a.records[nativeID]
	a.mu.RUnlock()
	if ok {
		return record, nil
	}
	if _, err := a.Discover(); err != nil {
		return cursorSessionRecord{}, err
	}
	a.mu.RLock()
	record, ok = a.records[nativeID]
	a.mu.RUnlock()
	if !ok {
		return cursorSessionRecord{}, fmt.Errorf("cursor session not found: %s", nativeID)
	}
	return record, nil
}

func (a *CursorAdapter) resolvePublicSession(sessionID string) (*SessionInfo, error) {
	sessions, err := a.Discover()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return nil, fmt.Errorf("cursor session not found: %s", sessionID)
}

func (a *CursorAdapter) newestSessionInDir(projectDir string, notBefore time.Time) string {
	sessions, err := a.Discover()
	if err != nil {
		return ""
	}
	want := strings.TrimSpace(projectDir)
	for _, session := range sessions {
		if want != "" && !strings.EqualFold(filepath.Clean(session.ProjectDir), filepath.Clean(want)) {
			continue
		}
		if !notBefore.IsZero() && session.LastActivity.Before(notBefore) {
			continue
		}
		nativeID, err := nativeSessionID(AgentCursor, session.ID)
		if err == nil {
			return nativeID
		}
	}
	return ""
}

func cursorProjectSlug(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	abs = filepath.Clean(abs)
	vol := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs, vol)
	rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	rest = strings.ReplaceAll(rest, string(os.PathSeparator), "-")
	rest = strings.ReplaceAll(rest, " ", "-")
	if vol != "" {
		letter := strings.ToLower(string(vol[0]))
		if rest == "" {
			return letter
		}
		return letter + "-" + rest
	}
	return rest
}

func resolveCursorWorkspaceSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.EqualFold(slug, "empty-window") || cursorAllDigits(slug) {
		return ""
	}
	var root string
	rest := slug
	if len(slug) >= 2 && slug[1] == '-' && isASCIILetter(rune(slug[0])) {
		root = strings.ToUpper(string(slug[0])) + ":" + string(os.PathSeparator)
		rest = slug[2:]
	} else if runtime.GOOS != "windows" {
		root = string(os.PathSeparator)
		rest = strings.TrimPrefix(slug, "-")
	} else {
		return ""
	}
	parts := make([]string, 0, strings.Count(rest, "-")+1)
	for _, part := range strings.Split(rest, "-") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		trimmed := strings.TrimRight(root, `\/`)
		if cursorDirExists(trimmed) {
			return filepath.Clean(trimmed)
		}
		return ""
	}
	return findExistingCursorPath(root, parts)
}

func findExistingCursorPath(prefix string, parts []string) string {
	if len(parts) == 0 {
		if cursorDirExists(prefix) {
			return filepath.Clean(prefix)
		}
		return ""
	}
	for i := len(parts); i >= 1; i-- {
		names := []string{strings.Join(parts[:i], " ")}
		if hyphen := strings.Join(parts[:i], "-"); hyphen != names[0] {
			names = append(names, hyphen)
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			next := filepath.Join(prefix, name)
			if !cursorDirExists(next) {
				continue
			}
			if i == len(parts) {
				return filepath.Clean(next)
			}
			if found := findExistingCursorPath(next, parts[i:]); found != "" {
				return found
			}
		}
	}
	return ""
}

func cursorDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func cursorAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

var _ ClosableAdapter = (*CursorAdapter)(nil)
var _ OutputAdapter = (*CursorAdapter)(nil)
var _ NativeThreadStarter = (*CursorAdapter)(nil)
