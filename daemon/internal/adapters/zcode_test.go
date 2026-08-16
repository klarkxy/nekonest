package adapters

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestZCodeDiscoverRequiresInstalledCLI(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db.sqlite")
	now := time.Now().UTC().Truncate(time.Millisecond)
	writeZCodeStore(t, dbPath, now, zcodeStoreSession{
		id:        "sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		directory: `D:\repo`,
		title:     "visible zcode",
		taskType:  "interactive",
	})

	adapter := NewZCodeAdapter()
	adapter.dbPath = dbPath
	adapter.commander.SetCLIPath(filepath.Join(root, "missing-zcode"))
	if adapter.IsAvailable() {
		t.Fatal("zcode must stay unavailable by default")
	}
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("discovered leftover store without CLI: %#v", sessions)
	}

	dummy := filepath.Join(root, "zcode.exe")
	if err := os.WriteFile(dummy, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter.commander.SetCLIPath(dummy)
	sessions, err = adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("runtime-disabled zcode still discovered: %#v", sessions)
	}
	adapter.commander.EnableRuntimeForTest()
	sessions, err = adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "zcode:sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].ProjectDir != `D:\repo` || sessions[0].Summary != "visible zcode" {
		t.Fatalf("session = %#v", sessions[0])
	}
	if !adapter.OwnsSession("zcode:sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") ||
		adapter.OwnsSession("sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") ||
		adapter.OwnsSession("zcode:missing") {
		t.Fatal("ownership")
	}
}

func TestZCodeFiltersSubagentsAndReadsHistory(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db.sqlite")
	now := time.Now().UTC().Truncate(time.Millisecond)
	parent := "sess_11111111-2222-3333-4444-555555555555"
	writeZCodeStore(t, dbPath, now,
		zcodeStoreSession{
			id: parent, directory: `D:\repo`, title: "parent", taskType: "interactive",
			messages: []zcodeStoreMessage{
				{id: "msg-user", role: "user", text: "hello zcode", created: now},
				{id: "msg-think", role: "assistant", partType: "thinking", text: "hidden thought", created: now},
				{id: "msg-asst", role: "assistant", text: "hi there", created: now},
			},
		},
		zcodeStoreSession{
			id: "sess_subagent_agent_zzzz", directory: `D:\repo`, title: "child",
			parentID: parent, taskType: "subagent_child",
		},
	)
	dummy := filepath.Join(root, "zcode.exe")
	if err := os.WriteFile(dummy, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := NewZCodeAdapter()
	adapter.dbPath = dbPath
	adapter.commander.SetCLIPath(dummy)
	adapter.commander.EnableRuntimeForTest()
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "zcode:"+parent {
		t.Fatalf("sessions = %#v", sessions)
	}
	if adapter.OwnsSession("zcode:sess_subagent_agent_zzzz") {
		t.Fatal("subagent should not be owned as a main thread")
	}
	history, err := adapter.FetchHistory("zcode:"+parent, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hello zcode" || history[1].Content != "hi there" {
		t.Fatalf("history = %#v", history)
	}
	first, err := adapter.Watch("zcode:" + parent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Watch("zcode:" + parent)
	if err != nil {
		t.Fatal(err)
	}
	assertWatchClosed(t, first)
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	assertWatchClosed(t, second)
}

func TestZCodeNewestSessionIgnoresOlderThreads(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "db.sqlite")
	now := time.Now().UTC().Truncate(time.Millisecond)
	oldID := "sess_11111111-2222-3333-4444-555555555555"
	newID := "sess_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeZCodeStore(t, dbPath, now,
		zcodeStoreSession{
			id: oldID, directory: `D:\repo`, title: "old", taskType: "interactive",
			updated: now.Add(-time.Hour),
		},
		zcodeStoreSession{
			id: newID, directory: `D:\repo`, title: "new", taskType: "interactive",
			updated: now,
		},
	)
	dummy := filepath.Join(root, "zcode.exe")
	if err := os.WriteFile(dummy, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := NewZCodeAdapter()
	adapter.dbPath = dbPath
	adapter.commander.SetCLIPath(dummy)
	adapter.commander.EnableRuntimeForTest()
	if got := adapter.newestSessionInDir(`D:\repo`, now.Add(-time.Minute)); got != newID {
		t.Fatalf("recent fallback = %q", got)
	}
	if got := adapter.newestSessionInDir(`D:\repo`, now.Add(time.Minute)); got != "" {
		t.Fatalf("future cutoff adopted %q", got)
	}
	if got := adapter.newestSessionInDir(`D:\other`, now.Add(-time.Hour)); got != "" {
		t.Fatalf("other dir adopted %q", got)
	}
}

type zcodeStoreSession struct {
	id        string
	directory string
	title     string
	parentID  string
	taskType  string
	archived  bool
	updated   time.Time
	messages  []zcodeStoreMessage
}

type zcodeStoreMessage struct {
	id       string
	role     string
	partType string
	text     string
	created  time.Time
}

func writeZCodeStore(t *testing.T, path string, now time.Time, sessions ...zcodeStoreSession) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE session (
			id TEXT PRIMARY KEY, project_id TEXT, workspace_id TEXT, parent_id TEXT,
			slug TEXT, directory TEXT, path TEXT, title TEXT, version TEXT,
			time_created INTEGER, time_updated INTEGER, time_archived INTEGER, task_type TEXT
		)`,
		`CREATE TABLE message (
			id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, time_updated INTEGER,
			data TEXT, sequence INTEGER
		)`,
		`CREATE TABLE part (
			id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER,
			time_updated INTEGER, data TEXT, sequence INTEGER
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range sessions {
		var archived interface{}
		if session.archived {
			archived = now.UnixMilli()
		}
		updated := session.updated
		if updated.IsZero() {
			updated = now
		}
		if _, err := db.Exec(
			`INSERT INTO session(id, parent_id, directory, path, title, time_created, time_updated, time_archived, task_type)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			session.id, nullIfEmpty(session.parentID), session.directory, session.directory, session.title,
			updated.UnixMilli(), updated.UnixMilli(), archived, session.taskType,
		); err != nil {
			t.Fatal(err)
		}
		for i, message := range session.messages {
			created := message.created.UnixMilli()
			if _, err := db.Exec(
				`INSERT INTO message(id, session_id, time_created, time_updated, data, sequence) VALUES (?, ?, ?, ?, ?, ?)`,
				message.id, session.id, created, created,
				`{"role":"`+message.role+`"}`, i,
			); err != nil {
				t.Fatal(err)
			}
			partType := message.partType
			if partType == "" {
				partType = "text"
			}
			if _, err := db.Exec(
				`INSERT INTO part(id, message_id, session_id, time_created, time_updated, data, sequence) VALUES (?, ?, ?, ?, ?, ?, 0)`,
				message.id+"-part", message.id, session.id, created, created,
				`{"type":"`+partType+`","text":"`+message.text+`"}`,
			); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func nullIfEmpty(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
