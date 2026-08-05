package startjournal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRequest(prompt string) Request {
	return Request{
		AgentType:    "claude_code",
		ProjectDir:   `windows:d:/repo`,
		PromptDigest: PromptDigest(prompt),
	}
}

func TestJournalDuplicateReplaysTerminalResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "starts.json")
	journal, err := Load(path, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	req := testRequest("hello")
	started, created, err := journal.Begin("operation-1", req)
	if err != nil || !created || started.Status != StatusStarting {
		t.Fatalf("begin = %#v, created=%v, err=%v", started, created, err)
	}
	want, err := journal.FinishWithPromptOutcome("operation-1", StatusOwned, "native-session", "", true)
	if err != nil {
		t.Fatal(err)
	}

	replayed, created, err := journal.Begin("operation-1", req)
	if err != nil || created || replayed != want {
		t.Fatalf("duplicate = %#v, created=%v, err=%v; want %#v", replayed, created, err, want)
	}

	restarted, err := Load(path, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	replayed, created, err = restarted.Begin("operation-1", req)
	if err != nil || created || replayed.Status != StatusOwned || replayed.SessionID != "native-session" {
		t.Fatalf("restart replay = %#v, created=%v, err=%v", replayed, created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hello") {
		t.Fatal("thread start journal persisted prompt plaintext")
	}
}

func TestJournalRejectsConflictingDuplicate(t *testing.T) {
	journal, err := Load(filepath.Join(t.TempDir(), "starts.json"), "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Begin("same-op", testRequest("first")); err != nil {
		t.Fatal(err)
	}
	conflict := testRequest("different")
	if _, _, err := journal.Begin("same-op", conflict); !IsConflict(err) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}
	record, ok := journal.Lookup("same-op")
	if !ok || record.PromptDigest != PromptDigest("first") || record.Status != StatusStarting {
		t.Fatalf("conflict changed original record: %#v, ok=%v", record, ok)
	}
}

func TestJournalRecoversStartingAsIndeterminate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "starts.json")
	journal, err := Load(path, "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Begin("crash-op", testRequest("hello")); err != nil {
		t.Fatal(err)
	}

	restarted, err := Load(path, "device")
	if err != nil {
		t.Fatal(err)
	}
	record, created, err := restarted.Begin("crash-op", testRequest("hello"))
	if err != nil || created || record.Status != StatusIndeterminate || record.Message != RecoveredMessage {
		t.Fatalf("recovered = %#v, created=%v, err=%v", record, created, err)
	}

	again, err := Load(path, "device")
	if err != nil {
		t.Fatal(err)
	}
	record, ok := again.Lookup("crash-op")
	if !ok || record.Status != StatusIndeterminate {
		t.Fatalf("persisted recovery = %#v, ok=%v", record, ok)
	}
}

func TestJournalFailClosedOnWrongDeviceAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "starts.json")
	journal, err := Load(path, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Begin("op", testRequest("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, "device-b"); err == nil {
		t.Fatal("journal loaded for a different device")
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, "device-a"); err == nil {
		t.Fatal("corrupt journal loaded")
	}
}

func TestJournalFailedPersistenceDoesNotChangeMemory(t *testing.T) {
	journal, err := Load(filepath.Join(t.TempDir(), "starts.json"), "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Begin("op", testRequest("hello")); err != nil {
		t.Fatal(err)
	}
	journal.path = t.TempDir()
	if _, err := journal.FinishWithPromptOutcome("op", StatusOwned, "native", "", true); err == nil {
		t.Fatal("finish unexpectedly persisted to a directory path")
	}
	record, ok := journal.FailClosed("op", "journal update failed")
	if !ok || record.Status != StatusIndeterminate {
		t.Fatalf("fail-closed memory state = %#v, ok=%v", record, ok)
	}
	replayed, created, err := journal.Begin("op", testRequest("hello"))
	if err != nil || created || replayed.Status != StatusIndeterminate {
		t.Fatalf("duplicate after persistence failure = %#v, created=%v, err=%v", replayed, created, err)
	}
}

func TestJournalRejectsOwnedWithoutPromptAcknowledgement(t *testing.T) {
	journal, err := Load(filepath.Join(t.TempDir(), "starts.json"), "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Begin("op", testRequest("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishWithPromptOutcome("op", StatusOwned, "native", "", false); err == nil {
		t.Fatal("owned result without prompt acknowledgement was accepted")
	}
}

func TestConflictErrorSupportsErrorsAs(t *testing.T) {
	err := error(&ConflictError{OperationID: "op"})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.OperationID != "op" {
		t.Fatalf("errors.As failed: %v", err)
	}
}

func TestJournalAtomicRewriteLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "starts.json")
	journal, err := Load(path, "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Begin("operation", testRequest("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Finish("operation", StatusFailed, "", "not available"); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".thread-start-journal-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary journal files remain: %v", matches)
	}
}
