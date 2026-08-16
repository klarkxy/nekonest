package adapters

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestCursorDiscoverRequiresAgentCLI(t *testing.T) {
	root := t.TempDir()
	nativeID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionDir := filepath.Join(root, "chats", "workspacehash", nativeID)
	mustMkdirAll(t, sessionDir)
	now := time.Now().UTC().Truncate(time.Millisecond)
	mustWriteJSON(t, filepath.Join(sessionDir, "meta.json"), map[string]interface{}{
		"title":       "cursor thread",
		"cwd":         `D:\repo`,
		"updatedAtMs": now.UnixMilli(),
	})
	writeCursorStore(t, filepath.Join(sessionDir, "store.db"), now, nativeID)

	adapter := NewCursorAdapter()
	adapter.chatsRoot = filepath.Join(root, "chats")
	adapter.projectsRoot = filepath.Join(root, "projects")
	adapter.commander.SetCLIPath(filepath.Join(root, "cursor.exe"))
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("editor binary must not enable Cursor: %#v", sessions)
	}

	dummy := filepath.Join(root, "cursor-agent.exe")
	if err := os.WriteFile(dummy, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter.commander.SetCLIPath(dummy)
	sessions, err = adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "cursor:"+nativeID {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].ProjectDir != `D:\repo` || sessions[0].Summary != "cursor thread" {
		t.Fatalf("session = %#v", sessions[0])
	}
	if !adapter.OwnsSession("cursor:"+nativeID) || adapter.OwnsSession(nativeID) {
		t.Fatal("ownership")
	}
	history, err := adapter.FetchHistory("cursor:"+nativeID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hello cursor" || history[1].Content != "ok" {
		t.Fatalf("history = %#v", history)
	}
	if history[0].Timestamp != 100 || history[1].Timestamp != 200 {
		t.Fatalf("history timestamps = %#v", history)
	}
	first, err := adapter.Watch("cursor:" + nativeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	assertWatchClosed(t, first)
}

func writeCursorStore(t *testing.T, path string, now time.Time, nativeID string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('title', '"cursor thread"'), ('cwd', '"D:\\repo"'), ('updatedAtMs', ?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO blobs(id, data) VALUES (?, ?), (?, ?), (?, ?)`,
		"a1", `{"role":"assistant","content":[{"type":"text","text":"ok"}],"timestamp":200}`,
		"sys", `{"role":"system","content":[{"type":"text","text":"hidden"}],"timestamp":150}`,
		"u1", `{"role":"user","content":[{"type":"text","text":"hello cursor"}],"timestamp":100}`,
	); err != nil {
		t.Fatal(err)
	}
	_ = nativeID
}

func TestCursorDiscoversAgentTranscripts(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "0 code", "nekonest")
	mustMkdirAll(t, project)
	nativeID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	hiddenID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	emptyID := "cccccccc-dddd-eeee-ffff-000000000000"
	slug := cursorProjectSlug(project)
	if slug == "" {
		t.Fatal("empty project slug")
	}
	cursorHome := filepath.Join(root, "cursor-home")
	transcriptDir := filepath.Join(cursorHome, "projects", slug, "agent-transcripts", nativeID)
	mustMkdirAll(t, transcriptDir)
	mustWriteLines(t, filepath.Join(transcriptDir, nativeID+".jsonl"),
		`{"role":"user","message":{"content":[{"type":"text","text":"hello   cursor\ntranscript"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"ok transcript"}]}}`,
	)
	hiddenDir := filepath.Join(cursorHome, "projects", slug, "agent-transcripts", hiddenID)
	mustMkdirAll(t, hiddenDir)
	mustWriteLines(t, filepath.Join(hiddenDir, hiddenID+".jsonl"),
		`{"role":"system","message":{"content":[{"type":"text","text":"hidden"}]}}`,
	)
	emptyDir := filepath.Join(cursorHome, "projects", "empty-window", "agent-transcripts", emptyID)
	mustMkdirAll(t, emptyDir)
	mustWriteLines(t, filepath.Join(emptyDir, emptyID+".jsonl"),
		`{"role":"user","message":{"content":[{"type":"text","text":"no project"}]}}`,
	)

	adapter := NewCursorAdapter()
	adapter.chatsRoot = filepath.Join(cursorHome, "chats")
	adapter.projectsRoot = filepath.Join(cursorHome, "projects")
	dummy := filepath.Join(root, "cursor-agent.exe")
	if err := os.WriteFile(dummy, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter.commander.SetCLIPath(dummy)

	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %#v", sessions)
	}
	var found, uncategorized *SessionInfo
	for _, session := range sessions {
		switch session.ID {
		case "cursor:" + nativeID:
			found = session
		case "cursor:" + emptyID:
			uncategorized = session
		}
	}
	if found == nil || found.Summary != "hello cursor transcript" {
		t.Fatalf("transcript session = %#v", found)
	}
	if got := cursorCollapsedTitle("<timestamp>Sunday</timestamp> <user_query> 全面深度审查 </user_query>"); got != "全面深度审查" {
		t.Fatalf("wrapped title = %q", got)
	}
	if !sameFilePath(found.ProjectDir, project) {
		t.Fatalf("project dir = %q want %q", found.ProjectDir, project)
	}
	if uncategorized == nil || uncategorized.ProjectDir != "" {
		t.Fatalf("empty-window session = %#v", uncategorized)
	}
	if !adapter.OwnsSession("cursor:"+nativeID) || adapter.OwnsSession("cursor:"+hiddenID) {
		t.Fatal("ownership")
	}
	history, err := adapter.FetchHistory("cursor:"+nativeID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hello   cursor\ntranscript" || history[1].Content != "ok transcript" {
		t.Fatalf("history = %#v", history)
	}
}

func TestCursorWorkspaceSlugRoundTrip(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "0 code", "nekonest")
	mustMkdirAll(t, project)
	slug := cursorProjectSlug(project)
	got := resolveCursorWorkspaceSlug(slug)
	if !sameFilePath(got, project) {
		t.Fatalf("slug %q resolved to %q want %q", slug, got, project)
	}
	if resolveCursorWorkspaceSlug("empty-window") != "" {
		t.Fatal("empty-window")
	}
	if resolveCursorWorkspaceSlug("1786853112336") != "" {
		t.Fatal("numeric slug")
	}
}

func TestCursorNewestSessionIgnoresOlderThreads(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	mustMkdirAll(t, project)
	oldID := "11111111-2222-3333-4444-555555555555"
	newID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	slug := cursorProjectSlug(project)
	projectsRoot := filepath.Join(root, "projects")
	oldPath := filepath.Join(projectsRoot, slug, "agent-transcripts", oldID, oldID+".jsonl")
	newPath := filepath.Join(projectsRoot, slug, "agent-transcripts", newID, newID+".jsonl")
	mustWriteLines(t, oldPath, `{"role":"user","message":{"content":[{"type":"text","text":"old"}]}}`)
	mustWriteLines(t, newPath, `{"role":"user","message":{"content":[{"type":"text","text":"new"}]}}`)
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now().Add(-time.Minute)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	adapter := NewCursorAdapter()
	adapter.chatsRoot = filepath.Join(root, "chats")
	adapter.projectsRoot = projectsRoot
	dummy := filepath.Join(root, "cursor-agent.exe")
	if err := os.WriteFile(dummy, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter.commander.SetCLIPath(dummy)
	if got := adapter.newestSessionInDir(project, newTime.Add(-time.Minute)); got != newID {
		t.Fatalf("recent fallback = %q", got)
	}
	if got := adapter.newestSessionInDir(project, newTime.Add(time.Minute)); got != "" {
		t.Fatalf("future cutoff adopted %q", got)
	}
	if got := adapter.newestSessionInDir(filepath.Join(root, "other"), newTime.Add(-time.Hour)); got != "" {
		t.Fatalf("other dir adopted %q", got)
	}
}

func sameFilePath(got, want string) bool {
	if got == "" || want == "" {
		return got == want
	}
	gotAbs, err := filepath.Abs(got)
	if err != nil {
		return false
	}
	wantAbs, err := filepath.Abs(want)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(gotAbs), filepath.Clean(wantAbs))
}
