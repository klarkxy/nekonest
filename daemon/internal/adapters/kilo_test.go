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
			session_id TEXT NOT NULL,
			data TEXT NOT NULL DEFAULT '{}',
			time_updated INTEGER NOT NULL DEFAULT 0
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
