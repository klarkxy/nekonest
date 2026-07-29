package adapters

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func createKiloTestDB(t *testing.T, withParentID bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kilo.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	parentColumn := ""
	if withParentID {
		parentColumn = ", parent_id TEXT"
	}
	schema := fmt.Sprintf(`
		CREATE TABLE session (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			directory TEXT NOT NULL DEFAULT '',
			time_updated INTEGER NOT NULL,
			time_archived INTEGER,
			agent TEXT%s
		);
		CREATE TABLE part (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			message_id TEXT NOT NULL DEFAULT '',
			data TEXT NOT NULL DEFAULT '{}',
			time_created INTEGER NOT NULL DEFAULT 0,
			time_updated INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE message (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL DEFAULT '',
			time_created INTEGER NOT NULL DEFAULT 0,
			time_updated INTEGER NOT NULL DEFAULT 0,
			data TEXT NOT NULL DEFAULT '{}'
		);
	`, parentColumn)
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return path
}

func insertKiloSession(
	t *testing.T,
	db *sql.DB,
	withParentID bool,
	id, agent, parentID string,
	updated, archived interface{},
) {
	t.Helper()
	if withParentID {
		if _, err := db.Exec(`
			INSERT INTO session
				(id, title, directory, time_updated, time_archived, agent, parent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, id, id, `D:\nekonest`, updated, archived, agent, nullableString(parentID)); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := db.Exec(`
		INSERT INTO session
			(id, title, directory, time_updated, time_archived, agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, id, `D:\nekonest`, updated, archived, agent); err != nil {
		t.Fatal(err)
	}
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func TestKiloDiscoverExcludesChildSessionsBeforeLimit(t *testing.T) {
	path := createKiloTestDB(t, true)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	insertKiloSession(t, db, true, "root", "custom-root", "", now.Add(-time.Hour).UnixMilli(), nil)
	insertKiloSession(t, db, true, "archived-root", "code", "", now.UnixMilli(), now.UnixMilli())
	insertKiloSession(t, db, true, "old-root", "code", "", now.Add(-8*24*time.Hour).UnixMilli(), nil)
	for i := 0; i < 105; i++ {
		insertKiloSession(
			t,
			db,
			true,
			fmt.Sprintf("child-%03d", i),
			"code",
			"root",
			now.Add(time.Duration(i)*time.Millisecond).UnixMilli(),
			nil,
		)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := NewKiloAdapter()
	adapter.dbPath = path
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "root" {
		t.Fatalf("Discover() returned %#v, want only root", sessions)
	}

	adapter.watcherMu.Lock()
	defer adapter.watcherMu.Unlock()
	if len(adapter.lastDirs) != 1 {
		t.Fatalf("lastDirs contains %d entries, want 1", len(adapter.lastDirs))
	}
	if _, ok := adapter.lastDirs["child-000"]; ok {
		t.Fatal("child session leaked into lastDirs")
	}
}

func TestKiloDiscoverFailsOpenForLegacySchema(t *testing.T) {
	path := createKiloTestDB(t, false)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	insertKiloSession(t, db, false, "legacy-root", "code", "", time.Now().UnixMilli(), nil)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := NewKiloAdapter()
	adapter.dbPath = path
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "legacy-root" {
		t.Fatalf("Discover() returned %#v, want legacy root", sessions)
	}
}

func TestKiloFetchHistoryRejectsUnknownSession(t *testing.T) {
	path := createKiloTestDB(t, true)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	insertKiloSession(t, db, true, "known", "code", "", now.UnixMilli(), nil)
	insertKiloSession(t, db, true, "old-root", "code", "", now.Add(-30*24*time.Hour).UnixMilli(), nil)
	insertKiloSession(t, db, true, "hidden-child", "code", "known", now.UnixMilli(), nil)
	insertKiloSession(t, db, true, "archived", "code", "", now.UnixMilli(), now.UnixMilli())
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := NewKiloAdapter()
	adapter.dbPath = path
	if adapter.OwnsSession("missing") {
		t.Fatal("OwnsSession accepted an ID absent from the Kilo session table")
	}
	if !adapter.OwnsSession("known") {
		t.Fatal("OwnsSession rejected a known Kilo session with empty history")
	}
	if !adapter.OwnsSession("old-root") {
		t.Fatal("OwnsSession rejected an old but otherwise visible root session")
	}
	for _, hiddenID := range []string{"hidden-child", "archived"} {
		if adapter.OwnsSession(hiddenID) {
			t.Fatalf("OwnsSession accepted hidden session %q", hiddenID)
		}
		if _, err := adapter.FetchHistory(hiddenID, 1); err == nil {
			t.Fatalf("FetchHistory accepted hidden session %q", hiddenID)
		}
	}
	if _, err := adapter.FetchHistory("missing", 1); err == nil {
		t.Fatal("FetchHistory accepted an ID absent from the Kilo session table")
	}
	history, err := adapter.FetchHistory("known", 1)
	if err != nil {
		t.Fatalf("known empty session: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("known empty session history = %#v", history)
	}
}

func TestKiloFetchHistoryIncludesStoredExecutionErrors(t *testing.T) {
	path := createKiloTestDB(t, true)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertKiloSession(t, db, true, "known", "code", "", now, nil)
	if _, err := db.Exec(`
		INSERT INTO message
			(id, session_id, time_created, time_updated, data)
		VALUES
			('user-message', 'known', ?, ?, '{"role":"user"}'),
			(
				'error-message',
				'known',
				?,
				?,
				'{"role":"assistant","error":{"name":"MessageAbortedError","data":{"message":"Aborted"}}}'
			)
	`, now-1000, now-1000, now-800, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO part
			(id, session_id, message_id, data, time_created, time_updated)
		VALUES
			(
				'user-part',
				'known',
				'user-message',
				'{"type":"text","text":"ping"}',
				?,
				?
			),
			(
				'partial-part',
				'known',
				'error-message',
				'{"type":"text","text":"partial reply"}',
				?,
				?
			)
	`, now-1000, now-1000, now-500, now-500); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := NewKiloAdapter()
	adapter.dbPath = path
	history, err := adapter.FetchHistory("known", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history = %#v", history)
	}
	if history[0].Role != "user" || history[0].Content != "ping" {
		t.Fatalf("user history = %#v", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "partial reply" {
		t.Fatalf("partial history = %#v", history[1])
	}
	if history[2].ID != "error-message" ||
		history[2].Role != "system" ||
		history[2].Type != "error" ||
		history[2].Content != "Kilo execution failed (MessageAbortedError): Aborted" {
		t.Fatalf("error history = %#v", history[2])
	}

	var events []OutputEvent
	adapter.SetOutputSink(func(event OutputEvent) {
		events = append(events, event)
	})
	adapter.commander.OnStreamStart("known", 1, now-900)
	adapter.commander.OnStreamEnd("known", 1, 1)
	if len(events) != 1 {
		t.Fatalf("exit events = %#v", events)
	}
	if events[0].MessageID != "error-message" ||
		events[0].Type != "error" ||
		events[0].Content != history[2].Content {
		t.Fatalf("exit event = %#v", events[0])
	}
}

func TestKiloRunCleanupCannotStopNextRun(t *testing.T) {
	adapter := NewKiloAdapter()
	adapter.dbPath = filepath.Join(t.TempDir(), "missing.db")
	var events []OutputEvent
	adapter.SetOutputSink(func(event OutputEvent) {
		events = append(events, event)
	})

	startedAt := time.Now().UnixMilli()
	adapter.commander.OnStreamStart("same", 1, startedAt)
	adapter.commander.OnAgentOutput(
		"same",
		1,
		"error",
		"MessageAbortedError: Aborted",
		"",
	)
	adapter.commander.OnStreamStart("same", 2, startedAt+1)
	adapter.commander.OnStreamEnd("same", 1, 1)

	newKey := kiloRunKey{sessionID: "same", runNumber: 2}
	if _, ok := adapter.runs.Load(newKey); !ok {
		t.Fatal("old run cleanup removed the new run")
	}
	adapter.commander.OnAgentOutput("same", 2, "assistant", "new reply", "new-part")
	adapter.commander.OnStreamEnd("same", 2, 0)

	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Type != "error" ||
		events[0].MessageID != fmt.Sprintf("kilo_error_%d_1", startedAt) {
		t.Fatalf("old error event = %#v", events[0])
	}
	if events[1].Type != "assistant" ||
		events[1].MessageID != "new-part" ||
		events[1].Content != "new reply" {
		t.Fatalf("new run event = %#v", events[1])
	}
	if _, ok := adapter.runs.Load(newKey); ok {
		t.Fatal("new run state leaked after exit")
	}
}
