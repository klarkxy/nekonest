package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nekonest/daemon/internal/adapters"
	"github.com/nekonest/daemon/internal/attach"
)

func TestPromptQueueMigratesV1RunningAndPausedFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	disk := promptQueueDisk{Version: promptQueueLegacyVersion, Items: []promptQueueItem{
		{SessionID: "session", ClientMsgID: "running", AgentType: "codex", Prompt: "one", CreatedAt: 1, Order: 1, Status: "running"},
		{SessionID: "session", ClientMsgID: "paused", AgentType: "codex", Prompt: "two", CreatedAt: 2, Order: 2, Status: "paused"},
	}}
	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	queue, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	items := queue.list("session")
	if len(items) != 2 || items[0].Status != promptQueueBlockedIndeterminate || items[1].Status != promptQueueBlockedIndeterminate {
		t.Fatalf("migrated v1 queue = %#v", items)
	}
	if _, ok, err := queue.claimNext("session"); err != nil || ok {
		t.Fatalf("migrated blocker dispatched: ok=%v err=%v", ok, err)
	}
	var persisted promptQueueDisk
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(updated, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != promptQueueVersion {
		t.Fatalf("persisted version = %d", persisted.Version)
	}
}

func queueItem(sessionID, clientMsgID, prompt string) promptQueueItem {
	return promptQueueItem{SessionID: sessionID, ClientMsgID: clientMsgID, AgentType: "codex", Prompt: prompt}
}

func TestPromptQueueFIFOAndLimitAndIdempotency(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, added, err := queue.enqueue(queueItem("session", "one", "first"))
	if err != nil || !added {
		t.Fatalf("enqueue first = %#v, %v, %v", first, added, err)
	}
	duplicate, added, err := queue.enqueue(queueItem("session", "one", "different retry"))
	if err != nil || added || duplicate.Prompt != "first" || duplicate.Order != first.Order {
		t.Fatalf("duplicate enqueue = %#v, %v, %v", duplicate, added, err)
	}
	if _, _, err := queue.enqueue(queueItem("session", "two", "second")); err != nil {
		t.Fatal(err)
	}
	next, ok := queue.next("session")
	if !ok || next.ClientMsgID != "one" || queue.position("session", "two") != 2 {
		t.Fatalf("FIFO next/position = %#v, %v, %d", next, ok, queue.position("session", "two"))
	}
	for i := 3; i <= maxPromptQueuePerSession; i++ {
		if _, _, err := queue.enqueue(queueItem("session", string(rune('a'+i)), "prompt")); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if _, _, err := queue.enqueue(queueItem("session", "over-limit", "prompt")); err == nil {
		t.Fatal("accepted more than 20 active queue items")
	}
}

func TestSessionStatusBlocksQueueDispatch(t *testing.T) {
	for _, status := range []adapters.AgentStatus{
		adapters.StatusRunning,
		adapters.StatusWaitingApproval,
		adapters.StatusWaitingUser,
	} {
		if !sessionStatusBlocksQueueDispatch(&adapters.SessionInfo{Status: status}) {
			t.Fatalf("status %q did not block queue dispatch", status)
		}
	}
	for _, status := range []adapters.AgentStatus{adapters.StatusIdle, adapters.StatusError} {
		if sessionStatusBlocksQueueDispatch(&adapters.SessionInfo{Status: status}) {
			t.Fatalf("status %q unexpectedly blocked queue dispatch", status)
		}
	}
}

func TestSessionStatusWakesQueueDispatchOnlyWhenIdle(t *testing.T) {
	if !sessionStatusWakesQueueDispatch(&adapters.SessionInfo{Status: adapters.StatusIdle}) {
		t.Fatal("idle session did not wake queue dispatch")
	}
	for _, status := range []adapters.AgentStatus{
		adapters.StatusRunning,
		adapters.StatusWaitingApproval,
		adapters.StatusWaitingUser,
		adapters.StatusError,
	} {
		if sessionStatusWakesQueueDispatch(&adapters.SessionInfo{Status: status}) {
			t.Fatalf("status %q unexpectedly woke queue dispatch", status)
		}
	}
	if sessionStatusWakesQueueDispatch(nil) {
		t.Fatal("missing session unexpectedly woke queue dispatch")
	}
}

func TestPromptQueueCancelAndTransitions(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := queue.enqueue(queueItem("session", "queued", "one")); err != nil {
		t.Fatal(err)
	}
	if err := queue.cancel("session", "queued"); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	if got := queue.list("session"); len(got) != 1 || got[0].Status != promptQueueCancelled {
		t.Fatalf("cancelled item = %#v", got)
	} else if got[0].Prompt != "" || len(got[0].Attachments) != 0 || len(got[0].SealedEnvelope) != 0 {
		t.Fatalf("cancelled item retained application payload: %#v", got[0])
	}
	if _, _, err := queue.enqueue(queueItem("session", "running", "two")); err != nil {
		t.Fatal(err)
	}
	if err := queue.markRunning("session", "running"); err != nil {
		t.Fatal(err)
	}
	if err := queue.cancel("session", "running"); err == nil {
		t.Fatal("running item was cancelled")
	}
	if err := queue.block("session", "running", promptQueueBlockedInterrupted); err != nil {
		t.Fatalf("block running: %v", err)
	}
	if err := queue.cancel("session", "running"); err != nil {
		t.Fatalf("cancel paused: %v", err)
	}
	if _, _, err := queue.enqueue(queueItem("session", "complete", "three")); err != nil {
		t.Fatal(err)
	}
	if err := queue.markRunning("session", "complete"); err != nil {
		t.Fatal(err)
	}
	if err := queue.complete("session", "complete"); err != nil {
		t.Fatalf("complete running: %v", err)
	}
	completed, ok := queue.item("session", "complete")
	if !ok || completed.Status != promptQueueCompleted || completed.Prompt != "" {
		t.Fatalf("completed tombstone = %#v, ok=%v", completed, ok)
	}
}

func TestPromptQueueReloadsPayloadFreeCancelledTombstone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	queue, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	item := queueItem("session", "cancelled", strings.Repeat("private-prompt", 1024))
	item.Attachments = []byte(`[{"url":"https://private.invalid/file"}]`)
	if _, _, err := queue.enqueue(item); err != nil {
		t.Fatal(err)
	}
	if err := queue.cancel("session", "cancelled"); err != nil {
		t.Fatal(err)
	}

	restarted, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := restarted.item("session", "cancelled")
	if !ok || got.Status != promptQueueCancelled {
		t.Fatalf("cancelled tombstone = %#v, ok=%v", got, ok)
	}
	if got.Prompt != "" || len(got.Attachments) != 0 || len(got.SealedEnvelope) != 0 {
		t.Fatalf("reloaded tombstone retained application payload: %#v", got)
	}
}

func TestPromptQueueCompactsTerminalTombstones(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < compactQueueTombstonesAt; i++ {
		item := promptQueueItem{
			SessionID: "session", ClientMsgID: fmt.Sprintf("cancelled-%d", i), AgentType: "codex",
			CreatedAt: int64(i + 1), Order: uint64(i + 1), Status: promptQueueCancelled,
		}
		queue.items[promptQueueKey(item.SessionID, item.ClientMsgID)] = item
	}
	if _, added, err := queue.enqueue(queueItem("session", "next", "continue after compaction")); err != nil || !added {
		t.Fatalf("enqueue after compaction = added=%v err=%v", added, err)
	}
	if got := queue.list("session"); len(got) != retainQueueTombstones+1 {
		t.Fatalf("compacted queue size = %d", len(got))
	}
}

func TestPromptQueueFailedWriteDoesNotChangeMemory(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	queue.path = t.TempDir()
	if _, _, err := queue.enqueue(queueItem("session", "message", "prompt")); err == nil {
		t.Fatal("enqueue unexpectedly succeeded with a directory as the queue path")
	}
	if got := queue.list("session"); len(got) != 0 {
		t.Fatalf("failed enqueue changed memory: %#v", got)
	}
}

func TestPromptQueueRestartBlocksRunningItemIndeterminate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	queue, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := queue.enqueue(queueItem("session", "message", "prompt")); err != nil {
		t.Fatal(err)
	}
	if err := queue.markRunning("session", "message"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := queue.enqueue(queueItem("session", "later", "later prompt")); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	items := restarted.list("session")
	if len(items) != 2 || items[0].Status != promptQueueBlockedIndeterminate {
		t.Fatalf("restart item = %#v", items)
	}
	if err := restarted.resumeSession("session"); err != nil {
		t.Fatalf("resume indeterminate queue: %v", err)
	}
	if _, ok, err := restarted.claimNext("session"); err != nil || ok {
		t.Fatalf("indeterminate blocker allowed dispatch: ok=%v err=%v", ok, err)
	}
	if err := restarted.skipIndeterminate("session", "message"); err != nil {
		t.Fatalf("skip indeterminate blocker: %v", err)
	}
	if next, ok := restarted.next("session"); !ok || next.ClientMsgID != "later" {
		t.Fatalf("next after explicit skip = %#v, %v", next, ok)
	}
}

func TestPromptQueueSealedEnvelopeRoundTripsByteForByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	queue, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	wire := []byte(` { "nonce" : "A+/=", "ciphertext" : "\nnot-normalized" } `)
	item := queueItem("session", "sealed", "")
	item.SealedEnvelope = wire
	queued, added, err := queue.enqueue(item)
	if err != nil || !added || !bytes.Equal(queued.SealedEnvelope, wire) {
		t.Fatalf("enqueue sealed = %#v, %v, %v", queued, added, err)
	}
	restarted, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	items := restarted.list("session")
	if len(items) != 1 || !bytes.Equal(items[0].SealedEnvelope, wire) {
		t.Fatalf("sealed envelope changed: %#v", items)
	}
}

func TestPromptQueueBlockedFailureResumeKeepsFIFO(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if _, _, err := queue.enqueue(queueItem("session", id, id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.markRunning("session", "one"); err != nil {
		t.Fatal(err)
	}
	if err := queue.blockSession("session", promptQueueBlockedFailed); err != nil {
		t.Fatal(err)
	}
	items := queue.list("session")
	if items[0].Status != promptQueueBlockedFailed || items[1].Status != promptQueueQueued {
		t.Fatalf("failed blocker queue = %#v", items)
	}
	if err := queue.resumeSession("session"); err != nil {
		t.Fatal(err)
	}
	if next, ok := queue.next("session"); !ok || next.ClientMsgID != "two" {
		t.Fatalf("resumed FIFO next = %#v, %v", next, ok)
	}
}

func TestPromptQueueClaimNextAllowsOneConcurrentRunner(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if _, _, err := queue.enqueue(queueItem("session", id, id)); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	claimed := make(chan promptQueueItem, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, ok, err := queue.claimNext("session")
			if err != nil {
				errs <- err
				return
			}
			if ok {
				claimed <- item
			}
		}()
	}
	wg.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var got []promptQueueItem
	for item := range claimed {
		got = append(got, item)
	}
	if len(got) != 1 || got[0].ClientMsgID != "one" || got[0].Status != promptQueueRunning {
		t.Fatalf("concurrent claims = %#v", got)
	}
	if next, ok := queue.next("session"); !ok || next.ClientMsgID != "two" {
		t.Fatalf("remaining FIFO item = %#v, ok=%v", next, ok)
	}
}

func TestQueuedPromptPayloadDefersAttachmentMaterialization(t *testing.T) {
	item := queueItem("session", "message", "keep this")
	item.Attachments = []byte(`[{"url":"https://nest.example/api/attachments/one","name":"one.txt","mime":"text/plain"}]`)
	prompt, refs, mode, err := queuedPromptPayload("device", "session", item)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "keep this" || len(refs) != 1 || refs[0].URL != "https://nest.example/api/attachments/one" || mode != "" {
		t.Fatalf("queued payload = %q, %#v, mode=%q", prompt, refs, mode)
	}
}

func TestQueuedPromptPayloadRejectsMoreThanFiveAttachments(t *testing.T) {
	item := queueItem("session", "message", "inspect")
	item.Attachments = []byte(`[
		{"url":"https://nest.example/api/attachments/1"},
		{"url":"https://nest.example/api/attachments/2"},
		{"url":"https://nest.example/api/attachments/3"},
		{"url":"https://nest.example/api/attachments/4"},
		{"url":"https://nest.example/api/attachments/5"},
		{"url":"https://nest.example/api/attachments/6"}
	]`)
	if _, _, _, err := queuedPromptPayload("device", "session", item); err == nil || !strings.Contains(err.Error(), "limit 5") {
		t.Fatalf("queued payload limit error = %v", err)
	}
}

func TestQueuedPromptPayloadPreservesPlanMode(t *testing.T) {
	item := queueItem("session", "message", "make a plan")
	item.CollaborationMode = "plan"
	prompt, refs, mode, err := queuedPromptPayload("device", "session", item)
	if err != nil || prompt != "make a plan" || len(refs) != 0 || mode != "plan" {
		t.Fatalf("queued plan payload = %q, %#v, %q, %v", prompt, refs, mode, err)
	}
	if _, err := normalizeCollaborationMode("execute"); err == nil {
		t.Fatal("unsupported collaboration mode was accepted")
	}
}

func TestQueuedPromptRetainsAttachmentsAfterPostBoundaryJournalFailure(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "prompt-journal.json")
	journal, err := loadPromptJournal(journalPath, "device", 8)
	if err != nil {
		t.Fatal(err)
	}
	attachmentDir := t.TempDir()
	attachmentPath := filepath.Join(attachmentDir, "file.txt")
	if err := os.WriteFile(attachmentPath, []byte("still in use"), 0o600); err != nil {
		t.Fatal(err)
	}
	var request adapters.PromptRequest
	target := &routingAdapter{name: "claude_code", sendPrompt: func(got adapters.PromptRequest) error {
		request = got
		// The native process has started. Force only the following accepted
		// journal transition to fail.
		journal.path = t.TempDir()
		return nil
	}}
	item := promptQueueItem{SessionID: "session", ClientMsgID: "message", AgentType: "claude_code", Prompt: "inspect"}
	crossed, dispatchErr := dispatchQueuedPrompt(
		target, journal, newPromptAcceptanceCache(8), nil, "device", item,
		"inspect", []attach.LocalFile{{Path: attachmentPath, Name: "file.txt", MIME: "text/plain"}},
		attachmentDir, 1, newActiveTurnRegistry(), nil,
	)
	if dispatchErr == nil || !crossed {
		t.Fatalf("dispatch = crossed %v, err %v", crossed, dispatchErr)
	}
	if _, err := os.Stat(attachmentPath); err != nil {
		t.Fatalf("post-boundary attachment was removed early: %v", err)
	}
	if request.OnComplete == nil {
		t.Fatal("native process did not receive attachment cleanup ownership")
	}
	request.OnComplete()
	if _, err := os.Stat(attachmentDir); !os.IsNotExist(err) {
		t.Fatalf("attachment directory still exists after native completion: %v", err)
	}
}
