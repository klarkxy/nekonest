package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptJournalSurvivesRestartAndNormalizesDispatching(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	first, err := loadPromptJournal(path, "device-a", 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.markDispatching("session-1", "message-1", strings.Repeat("猫", 400)); err != nil {
		t.Fatal(err)
	}

	restarted, err := loadPromptJournal(path, "device-a", 8)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := restarted.state("session-1", "message-1")
	if !ok || record.Status != promptJournalIndeterminate {
		t.Fatalf("restart state = %#v, %v; want indeterminate", record, ok)
	}
	if len(record.PromptEcho) > maxPromptJournalEchoBytes {
		t.Fatalf("prompt echo not bounded: %d", len(record.PromptEcho))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "session-1") || !strings.Contains(string(data), "message-1") {
		t.Fatal("journal did not retain bounded routing identifiers needed to replay acceptance after reconnect")
	}
}

func TestPromptJournalAcceptedPersistsAndIsDeviceIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	journal, err := loadPromptJournal(path, "device-a", 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("session", "message", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markAccepted("session", "message"); err != nil {
		t.Fatal(err)
	}

	restarted, err := loadPromptJournal(path, "device-a", 8)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := restarted.state("session", "message")
	if !ok || record.Status != promptJournalAccepted || record.PromptEcho != "hello" {
		t.Fatalf("accepted state lost: %#v, %v", record, ok)
	}
	if _, err := loadPromptJournal(path, "device-b", 8); err == nil {
		t.Fatal("journal was accepted for a different device")
	}
}

func TestPromptJournalNeverEvictsIndeterminateCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	journal, err := loadPromptJournal(path, "device", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("s1", "m1", "one"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markIndeterminate("s1", "m1"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("s2", "m2", "two"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markIndeterminate("s2", "m2"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("s3", "m3", "three"); err == nil {
		t.Fatal("journal evicted an unresolved command to admit a new dispatch")
	}
	if _, ok := journal.state("s1", "m1"); !ok {
		t.Fatal("first indeterminate command was evicted")
	}
	if _, ok := journal.state("s2", "m2"); !ok {
		t.Fatal("second indeterminate command was evicted")
	}
}

func TestPromptJournalOnlyEvictsServerCommittedCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	journal, err := loadPromptJournal(path, "device", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("s1", "m1", "one"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markAccepted("s1", "m1"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("s2", "m2", "two"); err == nil {
		t.Fatal("accepted-but-uncommitted command was evicted")
	}
	if err := journal.markCommitted("s1", "m1"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("s2", "m2", "two"); err != nil {
		t.Fatalf("committed record was not eligible for eviction: %v", err)
	}
	if _, ok := journal.state("s1", "m1"); ok {
		t.Fatal("old committed record was not evicted")
	}
}

func TestPromptJournalEnumeratesUncommittedAcceptedAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	journal, err := loadPromptJournal(path, "device", 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		session string
		message string
	}{
		{"s1", "m1"},
		{"s2", "m2"},
	} {
		if err := journal.markDispatching(item.session, item.message, item.message); err != nil {
			t.Fatal(err)
		}
		if err := journal.markAccepted(item.session, item.message); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.markCommitted("s1", "m1"); err != nil {
		t.Fatal(err)
	}

	restarted, err := loadPromptJournal(path, "device", 4)
	if err != nil {
		t.Fatal(err)
	}
	pending := restarted.uncommittedAccepted()
	if len(pending) != 1 || pending[0].SessionID != "s2" || pending[0].ClientMsgID != "m2" {
		t.Fatalf("uncommitted accepted records = %#v", pending)
	}
}

func TestPromptJournalAtomicRewriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	journal, err := loadPromptJournal(path, "device", 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.markDispatching("session", "message", "prompt"); err != nil {
		t.Fatal(err)
	}
	if err := journal.markAccepted("session", "message"); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".prompt-journal-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary journal files left behind: %v", matches)
	}
}

func TestPromptJournalMigratesVersionOneOpaqueRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	deviceID := "device"
	sessionID := "legacy-session"
	clientMsgID := "legacy-message"
	key := promptJournalKeyFromHash(deviceHash(deviceID), sessionID, clientMsgID)
	v1 := promptJournalDisk{
		Version:    1,
		DeviceHash: deviceHash(deviceID),
		Records: []promptJournalRecord{
			{
				Key:        key,
				Status:     promptJournalAccepted,
				PromptEcho: "legacy",
				UpdatedAt:  1,
			},
		},
	}
	data, err := json.Marshal(v1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	journal, err := loadPromptJournal(path, deviceID, 4)
	if err != nil {
		t.Fatalf("load v1 journal: %v", err)
	}
	record, ok := journal.state(sessionID, clientMsgID)
	if !ok || record.Status != promptJournalAccepted || !record.LegacyOpaque {
		t.Fatalf("migrated v1 record = %#v, %v", record, ok)
	}
	if pending := journal.uncommittedAccepted(); len(pending) != 0 {
		t.Fatalf("opaque v1 record cannot be proactively re-acked: %#v", pending)
	}
	if err := journal.markCommitted(sessionID, clientMsgID); err != nil {
		t.Fatalf("commit migrated record: %v", err)
	}
	record, ok = journal.state(sessionID, clientMsgID)
	if !ok || record.Status != promptJournalCommitted || record.LegacyOpaque ||
		record.SessionID != sessionID || record.ClientMsgID != clientMsgID {
		t.Fatalf("committed migrated record = %#v, %v", record, ok)
	}

	restarted, err := loadPromptJournal(path, deviceID, 4)
	if err != nil {
		t.Fatalf("reload migrated journal: %v", err)
	}
	if record, ok = restarted.state(sessionID, clientMsgID); !ok || record.Status != promptJournalCommitted {
		t.Fatalf("reloaded migrated record = %#v, %v", record, ok)
	}
}
