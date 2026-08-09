package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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
	if err := queue.resume("session", "queued"); err == nil {
		t.Fatal("cancelled item resumed")
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
	if err := queue.pause("session", "running"); err != nil {
		t.Fatalf("pause running: %v", err)
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
	for _, item := range queue.list("session") {
		if item.ClientMsgID == "complete" {
			t.Fatal("completed item remained in the queue")
		}
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

func TestPromptQueueFailsClosedAtCancelledTombstoneLimit(t *testing.T) {
	queue, err := loadPromptQueue(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxCancelledQueuePerSession; i++ {
		item := promptQueueItem{
			SessionID: "session", ClientMsgID: fmt.Sprintf("cancelled-%d", i), AgentType: "codex",
			CreatedAt: int64(i + 1), Order: uint64(i + 1), Status: promptQueueCancelled,
		}
		queue.items[promptQueueKey(item.SessionID, item.ClientMsgID)] = item
	}
	if _, _, err := queue.enqueue(queueItem("session", "blocked", "must not bypass tombstone cap")); err == nil {
		t.Fatal("enqueue bypassed cancelled tombstone limit")
	}
	if got := queue.list("session"); len(got) != maxCancelledQueuePerSession {
		t.Fatalf("failed enqueue changed tombstones: %d", len(got))
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

func TestPromptQueueRestartPausesRunningItem(t *testing.T) {
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
	restarted, err := loadPromptQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	items := restarted.list("session")
	if len(items) != 1 || items[0].Status != promptQueuePaused {
		t.Fatalf("restart item = %#v", items)
	}
	if err := restarted.resume("session", "message"); err != nil {
		t.Fatalf("resume paused item: %v", err)
	}
	if next, ok := restarted.next("session"); !ok || next.ClientMsgID != "message" {
		t.Fatalf("resumed next = %#v, %v", next, ok)
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

func TestPromptQueuePauseResumeSessionKeepsFIFO(t *testing.T) {
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
	if err := queue.pauseSession("session"); err != nil {
		t.Fatal(err)
	}
	for _, item := range queue.list("session") {
		if item.Status != promptQueuePaused {
			t.Fatalf("unpaused item after terminal pause: %#v", item)
		}
	}
	if err := queue.resumeSession("session"); err != nil {
		t.Fatal(err)
	}
	if next, ok := queue.next("session"); !ok || next.ClientMsgID != "one" {
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
	prompt, refs, err := queuedPromptPayload("device", "session", item)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "keep this" || len(refs) != 1 || refs[0].URL != "https://nest.example/api/attachments/one" {
		t.Fatalf("queued payload = %q, %#v", prompt, refs)
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
	if _, _, err := queuedPromptPayload("device", "session", item); err == nil || !strings.Contains(err.Error(), "limit 5") {
		t.Fatalf("queued payload limit error = %v", err)
	}
}
