package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/agentexec"
)

type zcodeSessionRecord struct {
	nativeID   string
	projectDir string
	title      string
	updated    time.Time
}

// ZCodeAdapter discovers and resumes local ZCode CLI sessions.
type ZCodeAdapter struct {
	dbPath    string
	mu        sync.RWMutex
	records   map[string]zcodeSessionRecord
	commander *agentexec.ZCodeCommander
	watches   pollWatchRegistry
	turns     turnTracker
}

func NewZCodeAdapter() *ZCodeAdapter {
	home, _ := os.UserHomeDir()
	root := strings.TrimSpace(os.Getenv("ZCODE_HOME"))
	if root == "" {
		root = filepath.Join(home, ".zcode")
	}
	a := &ZCodeAdapter{
		dbPath:    filepath.Join(root, "cli", "db", "db.sqlite"),
		records:   make(map[string]zcodeSessionRecord),
		commander: agentexec.NewZCodeCommander(),
		turns:     newTurnTracker(AgentZCode),
	}
	a.commander.OnTurnEnd = func(nativeID string, exitCode int, interrupted bool) {
		a.turns.finish(publicSessionID(AgentZCode, nativeID), exitCode, interrupted)
	}
	return a
}

func (a *ZCodeAdapter) Name() string { return string(AgentZCode) }

func (a *ZCodeAdapter) IsAvailable() bool {
	return a.commander != nil && a.commander.IsAvailable()
}

func (a *ZCodeAdapter) UnavailableReason() string {
	if a.IsAvailable() {
		return ""
	}
	return agentexec.ZCodeUnavailableReason
}

func (a *ZCodeAdapter) ProbeThreadStart(ctx context.Context) ThreadStartCapability {
	if a.commander == nil {
		return ThreadStartCapability{Reason: agentexec.ZCodeUnavailableReason}
	}
	if err := a.commander.ProbeThreadStart(ctx); err != nil {
		return ThreadStartCapability{Reason: err.Error()}
	}
	return ThreadStartCapability{Available: true, ControlPath: "cli", AttachmentMode: AttachPathBestEffort}
}

func (a *ZCodeAdapter) StartNativeThread(ctx context.Context, request ThreadStartRequest) (ThreadStartResult, error) {
	if !a.IsAvailable() {
		return ThreadStartResult{}, fmt.Errorf("%s", agentexec.ZCodeUnavailableReason)
	}
	startedAt := time.Now().Add(-2 * time.Second)
	nativeID, created, promptAccepted, err := a.commander.StartThread(
		ctx, request.ProjectDir, request.Prompt, request.Attachments, request.OnComplete,
	)
	if strings.TrimSpace(nativeID) == "" {
		nativeID = a.newestSessionInDir(request.ProjectDir, startedAt)
	}
	return ThreadStartResult{
		SessionID:      publicSessionID(AgentZCode, nativeID),
		Created:        created,
		PromptAccepted: promptAccepted,
	}, err
}

func (a *ZCodeAdapter) Close() error {
	a.turns.detachAll()
	a.watches.stopAll()
	if a.commander != nil {
		a.commander.StopAll()
	}
	return nil
}

func (a *ZCodeAdapter) Discover() ([]*SessionInfo, error) {
	if !a.IsAvailable() {
		a.mu.Lock()
		a.records = make(map[string]zcodeSessionRecord)
		a.mu.Unlock()
		return nil, nil
	}
	if _, err := os.Stat(a.dbPath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat zcode store: %w", err)
	}
	db, err := openReadOnlySQLite(a.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now()
	rows, err := db.Query(`
		SELECT id, COALESCE(directory, ''), COALESCE(path, ''), COALESCE(title, ''),
		       COALESCE(parent_id, ''), COALESCE(task_type, ''), time_updated, time_archived
		FROM session
	`)
	if err != nil {
		return nil, fmt.Errorf("query zcode sessions: %w", err)
	}
	defer rows.Close()

	records := make(map[string]zcodeSessionRecord)
	sessions := make([]*SessionInfo, 0)
	for rows.Next() {
		var (
			id, directory, path, title, parentID, taskType string
			updatedMillis                                  int64
			archived                                       sql.NullInt64
		)
		if err := rows.Scan(&id, &directory, &path, &title, &parentID, &taskType, &updatedMillis, &archived); err != nil {
			continue
		}
		if zcodeSessionHidden(id, parentID, taskType, archived.Valid) {
			continue
		}
		projectDir := strings.TrimSpace(directory)
		if projectDir == "" {
			projectDir = strings.TrimSpace(path)
		}
		updated := zcodeMillisTime(updatedMillis)
		if updated.IsZero() {
			updated = now
		}
		summary := strings.TrimSpace(title)
		if summary == "" {
			summary = id
		}
		status := StatusIdle
		if time.Since(updated) < time.Minute {
			status = StatusRunning
		}
		session := &SessionInfo{
			ID:           publicSessionID(AgentZCode, id),
			AgentType:    AgentZCode,
			Status:       status,
			Summary:      truncateRunes(summary, 120),
			LastActivity: updated,
			SessionPath:  a.dbPath,
			ProjectDir:   projectDir,
		}
		if !sessionIsVisible(session, now) {
			continue
		}
		records[id] = zcodeSessionRecord{nativeID: id, projectDir: projectDir, title: summary, updated: updated}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
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

func zcodeSessionHidden(id, parentID, taskType string, archived bool) bool {
	if archived || strings.TrimSpace(parentID) != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(taskType), "subagent_child") {
		return true
	}
	return strings.Contains(strings.ToLower(id), "subagent")
}

func zcodeMillisTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	if ms < 1_000_000_000_000 {
		return time.Unix(ms, 0)
	}
	return time.UnixMilli(ms)
}

func (a *ZCodeAdapter) OwnsSession(sessionID string) bool {
	nativeID, err := nativeSessionID(AgentZCode, sessionID)
	if err != nil {
		return false
	}
	_, err = a.resolveRecord(nativeID)
	return err == nil
}

func (a *ZCodeAdapter) FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error) {
	nativeID, err := nativeSessionID(AgentZCode, sessionID)
	if err != nil {
		return nil, err
	}
	if _, err := a.resolveRecord(nativeID); err != nil {
		return nil, err
	}
	db, err := openReadOnlySQLite(a.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT m.id, m.time_created, COALESCE(m.data, ''), COALESCE(p.data, '')
		FROM message m
		LEFT JOIN part p ON p.message_id = m.id
		WHERE m.session_id = ?
		ORDER BY COALESCE(m.sequence, 0), m.time_created, m.id, COALESCE(p.sequence, 0), p.id
	`, nativeID)
	if err != nil {
		return nil, fmt.Errorf("query zcode history: %w", err)
	}
	defer rows.Close()

	type pending struct {
		id      string
		role    string
		created time.Time
		parts   []string
	}
	var (
		messages []*HistoryMessage
		current  pending
		index    int
	)
	flush := func() {
		if current.id == "" {
			return
		}
		content := strings.TrimSpace(strings.Join(current.parts, "\n"))
		if (current.role == "user" || current.role == "assistant") && content != "" {
			index++
			messageID := current.id
			if messageID == "" {
				messageID = fmt.Sprintf("zcode_%s_%d", nativeID, index)
			}
			timestamp := current.created
			if timestamp.IsZero() {
				timestamp = time.Now()
			}
			messages = append(messages, &HistoryMessage{
				ID:        messageID,
				Role:      current.role,
				Content:   truncateRunes(content, 4000),
				Type:      "text",
				Timestamp: timestamp.Unix(),
			})
		}
		current = pending{}
	}
	for rows.Next() {
		var id, messageRaw, partRaw string
		var created int64
		if err := rows.Scan(&id, &created, &messageRaw, &partRaw); err != nil {
			continue
		}
		if current.id != id {
			flush()
			current = pending{id: id, role: zcodeMessageRole(messageRaw), created: zcodeMillisTime(created)}
		}
		if text := zcodePartText(partRaw); text != "" {
			current.parts = append(current.parts, text)
		}
	}
	flush()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return takeLastHistory(messages, limit), nil
}

func zcodeMessageRole(raw string) string {
	var payload map[string]interface{}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	return strings.ToLower(firstString(payload, "role"))
}

func zcodePartText(raw string) string {
	var payload map[string]interface{}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	kind := strings.ToLower(firstString(payload, "type"))
	if kind != "" && kind != "text" && kind != "input_text" && kind != "output_text" {
		return ""
	}
	return strings.TrimSpace(firstString(payload, "text", "content"))
}

func (a *ZCodeAdapter) Watch(sessionID string) (<-chan *SessionInfo, error) {
	if _, err := nativeSessionID(AgentZCode, sessionID); err != nil {
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

func (a *ZCodeAdapter) SendPrompt(sessionID string, request PromptRequest) error {
	if !a.IsAvailable() {
		return fmt.Errorf("%s", agentexec.ZCodeUnavailableReason)
	}
	if !a.turns.begin(sessionID, request) {
		return agentexec.ErrSessionBusy
	}
	nativeID, err := nativeSessionID(AgentZCode, sessionID)
	if err != nil {
		a.turns.abort(sessionID, request.Generation)
		return err
	}
	record, err := a.resolveRecord(nativeID)
	if err != nil {
		a.turns.abort(sessionID, request.Generation)
		return err
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

func (a *ZCodeAdapter) AcknowledgePrompt(sessionID string, generation uint64) {
	a.turns.accepted(sessionID, generation)
}
func (a *ZCodeAdapter) AbandonPrompt(sessionID string, generation uint64) {
	a.turns.abort(sessionID, generation)
}
func (a *ZCodeAdapter) SetControlSink(sink func(ControlEvent)) { a.turns.setSink(sink) }

func (a *ZCodeAdapter) Approve(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentZCode, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Approve(nativeID, approvalID)
}

func (a *ZCodeAdapter) Deny(sessionID, approvalID string) error {
	nativeID, err := nativeSessionID(AgentZCode, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Deny(nativeID, approvalID)
}

func (a *ZCodeAdapter) Interrupt(sessionID string) error {
	nativeID, err := nativeSessionID(AgentZCode, sessionID)
	if err != nil {
		return err
	}
	return a.commander.Interrupt(nativeID)
}

func (a *ZCodeAdapter) SetOutputSink(sink OutputSink) {
	if a.commander == nil {
		return
	}
	if sink == nil {
		a.commander.OnAgentOutput = nil
		return
	}
	a.commander.OnAgentOutput = func(nativeID, msgType, content, messageID string) {
		sink(OutputEvent{
			SessionID: publicSessionID(AgentZCode, nativeID),
			AgentType: AgentZCode,
			Type:      msgType,
			Content:   content,
			MessageID: messageID,
		})
	}
}

func (a *ZCodeAdapter) resolveRecord(nativeID string) (zcodeSessionRecord, error) {
	a.mu.RLock()
	record, ok := a.records[nativeID]
	a.mu.RUnlock()
	if ok {
		return record, nil
	}
	if _, err := a.Discover(); err != nil {
		return zcodeSessionRecord{}, err
	}
	a.mu.RLock()
	record, ok = a.records[nativeID]
	a.mu.RUnlock()
	if !ok {
		return zcodeSessionRecord{}, fmt.Errorf("zcode session not found: %s", nativeID)
	}
	return record, nil
}

func (a *ZCodeAdapter) resolvePublicSession(sessionID string) (*SessionInfo, error) {
	sessions, err := a.Discover()
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return nil, fmt.Errorf("zcode session not found: %s", sessionID)
}

func (a *ZCodeAdapter) newestSessionInDir(projectDir string, notBefore time.Time) string {
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
		nativeID, err := nativeSessionID(AgentZCode, session.ID)
		if err == nil {
			return nativeID
		}
	}
	return ""
}

var _ ClosableAdapter = (*ZCodeAdapter)(nil)
var _ OutputAdapter = (*ZCodeAdapter)(nil)
var _ NativeThreadStarter = (*ZCodeAdapter)(nil)
