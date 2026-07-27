package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nekonest/server/internal/protocol"
)

func TestHashToken(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	want := hex.EncodeToString(sum[:])
	if got := hashToken("secret"); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	h := sha256.Sum256([]byte(""))
	if hashToken("") != hex.EncodeToString(h[:]) {
		t.Fatal("empty")
	}
	if hashToken("a") == hashToken("b") {
		t.Fatal("collision")
	}
}

func TestGenerateToken(t *testing.T) {
	a, b := generateToken(), generateToken()
	if len(a) != 64 {
		t.Fatalf("len=%d", len(a))
	}
	if a == b {
		t.Fatal("not unique")
	}
}

func TestMetadataJSON(t *testing.T) {
	if metadataToJSON(nil) != "{}" {
		t.Fatal("nil")
	}
	s := metadataToJSON(map[string]any{"k": "v"})
	m := jsonToMetadata(s)
	if m["k"] != "v" {
		t.Fatalf("%#v", m)
	}
	if jsonToMetadata("") != nil || jsonToMetadata("{}") != nil {
		t.Fatal("empty")
	}
	if jsonToMetadata("{bad") != nil {
		t.Fatal("bad json")
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestSQLitePragmasApplied(t *testing.T) {
	d := openTestDB(t)
	var busyTimeout int
	if err := d.conn.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout=%d, want 5000", busyTimeout)
	}
	var journalMode string
	if err := d.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode=%q, want wal", journalMode)
	}
}

func TestDeviceRegisterAndValidate(t *testing.T) {
	d := openTestDB(t)
	tok, err := d.RegisterDevice("dev1", "PC")
	if err != nil || tok == "" {
		t.Fatalf("register: %v %q", err, tok)
	}
	if !d.ValidateDeviceToken("dev1", tok) {
		t.Fatal("valid token")
	}
	if d.ValidateDeviceToken("dev1", "wrong") {
		t.Fatal("invalid")
	}
	if d.ValidateDeviceToken("nope", tok) {
		t.Fatal("unknown device")
	}
}

func TestSaveGetMessages(t *testing.T) {
	d := openTestDB(t)
	msgs := []*protocol.SessionMessage{
		{ID: "m1", Role: "user", Content: "hi", Type: "text", Timestamp: 100},
		{ID: "m2", Role: "assistant", Content: "yo", Type: "text", Timestamp: 200},
		{ID: "m3", Role: "user", Content: "again", Type: "text", Timestamp: 300},
	}
	for _, m := range msgs {
		if err := d.SaveMessage("d1", "s1", m); err != nil {
			t.Fatal(err)
		}
	}
	// upsert longer content
	if err := d.SaveMessage("d1", "s1", &protocol.SessionMessage{
		ID: "m2", Role: "assistant", Content: "yo!!", Type: "text", Timestamp: 201,
	}); err != nil {
		t.Fatal(err)
	}

	all, err := d.GetMessages("d1", "s1", 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %v len=%d", err, len(all))
	}
	if all[1].Content != "yo!!" {
		t.Fatalf("upsert content %q", all[1].Content)
	}

	last2, err := d.GetMessages("d1", "s1", 2)
	if err != nil || len(last2) != 2 {
		t.Fatalf("limit: %v %d", err, len(last2))
	}
	if last2[0].ID != "m2" || last2[1].ID != "m3" {
		t.Fatalf("chrono after reverse: %s %s", last2[0].ID, last2[1].ID)
	}

	n, err := d.GetMessageCount("d1", "s1")
	if err != nil || n != 3 {
		t.Fatalf("count %d %v", n, err)
	}

	sids, err := d.ListSessionsWithMessages("d1")
	if err != nil || len(sids) != 1 || sids[0] != "s1" {
		t.Fatalf("sessions %#v %v", sids, err)
	}

	deleted, err := d.DeleteOldMessages(time.Unix(250, 0))
	if err != nil || deleted != 2 {
		t.Fatalf("delete old %d %v", deleted, err)
	}
	if err := d.DeleteSessionMessages("d1", "s1"); err != nil {
		t.Fatal(err)
	}
	n, _ = d.GetMessageCount("d1", "s1")
	if n != 0 {
		t.Fatalf("after delete %d", n)
	}
}

func TestPairCode(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.RegisterDevice("dev2", "X"); err != nil {
		t.Fatal(err)
	}
	code := "123456"
	if err := d.CreatePairCode(code, "dev2", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	dev, err := d.ConsumePairCode(code)
	if err != nil || dev != "dev2" {
		t.Fatalf("consume %q %v", dev, err)
	}
	if _, err := d.ConsumePairCode(code); err == nil {
		t.Fatal("reuse should fail")
	}
	if err := d.CreatePairCode("000000", "dev2", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ConsumePairCode("000000"); err == nil {
		t.Fatal("expired should fail")
	}
}

func TestDeviceListAndPush(t *testing.T) {
	d := openTestDB(t)
	tok, err := d.RegisterDevice("devL", "Lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateDeviceLastSeen("devL"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateDeviceSessions("devL", 3); err != nil {
		t.Fatal(err)
	}
	dev, err := d.GetDevice("devL")
	if err != nil || dev.Name != "Lab" {
		t.Fatalf("%#v %v", dev, err)
	}
	list, err := d.ListDevices()
	if err != nil || len(list) < 1 {
		t.Fatal(err)
	}
	if !d.ValidateDeviceToken("devL", tok) {
		t.Fatal("token")
	}
	sub := &PushSubscription{
		DeviceID: "devL",
		Endpoint: "https://fcm.googleapis.com/fcm/send/x",
		P256DH:   "p",
		Auth:     "a",
	}
	if err := d.SavePushSubscription(sub); err != nil {
		t.Fatal(err)
	}
	// upsert same endpoint same device
	sub.Auth = "a2"
	if err := d.SavePushSubscription(sub); err != nil {
		t.Fatal(err)
	}
	subs, err := d.GetPushSubscriptions("devL")
	if err != nil || len(subs) != 1 || subs[0].Auth != "a2" {
		t.Fatalf("%#v %v", subs, err)
	}
	// The same browser endpoint can subscribe to another device too.
	subB := &PushSubscription{
		DeviceID: "other",
		Endpoint: sub.Endpoint,
		P256DH:   "p",
		Auth:     "steal",
	}
	if err := d.SavePushSubscription(subB); err != nil {
		t.Fatal(err)
	}
	subs, _ = d.GetPushSubscriptions("devL")
	if len(subs) != 1 || subs[0].Auth != "a2" {
		t.Fatalf("stolen %#v", subs)
	}
	subsB, _ := d.GetPushSubscriptions("other")
	if len(subsB) != 1 || subsB[0].Auth != "steal" {
		t.Fatalf("other mapping missing %#v", subsB)
	}
	if err := d.DeletePushSubscription(sub.Endpoint); err != nil {
		t.Fatal(err)
	}
	subs, _ = d.GetPushSubscriptions("devL")
	if len(subs) != 0 {
		t.Fatal("deleted")
	}
	subsB, _ = d.GetPushSubscriptions("other")
	if len(subsB) != 0 {
		t.Fatal("endpoint delete should remove every invalid mapping")
	}
}

func TestLegacyPushSchemaMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE push_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			endpoint TEXT NOT NULL UNIQUE,
			p256dh TEXT NOT NULL DEFAULT '',
			auth TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);
		INSERT INTO push_subscriptions
			(device_id, endpoint, p256dh, auth, created_at)
			VALUES ('a', 'https://example.test/push', 'p', 'a', 1);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.SavePushSubscription(&PushSubscription{
		DeviceID: "b",
		Endpoint: "https://example.test/push",
		P256DH:   "p2",
		Auth:     "b",
	}); err != nil {
		t.Fatal(err)
	}
	for _, deviceID := range []string{"a", "b"} {
		subs, err := d.GetPushSubscriptions(deviceID)
		if err != nil || len(subs) != 1 {
			t.Fatalf("%s: %#v %v", deviceID, subs, err)
		}
	}
}

func TestPushSubscriptionsAreBoundedPerDevice(t *testing.T) {
	d := openTestDB(t)
	for i := 0; i < maxPushSubscriptionsPerDevice+5; i++ {
		if err := d.SavePushSubscription(&PushSubscription{
			DeviceID: "bounded",
			Endpoint: fmt.Sprintf("https://push.example/%02d", i),
			P256DH:   "p",
			Auth:     "a",
		}); err != nil {
			t.Fatal(err)
		}
	}
	subs, err := d.GetPushSubscriptions("bounded")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != maxPushSubscriptionsPerDevice {
		t.Fatalf("subscription count=%d want=%d", len(subs), maxPushSubscriptionsPerDevice)
	}
}

func TestLegacyPromptCommandSchemaMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-prompts.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE prompt_commands (
			device_id TEXT NOT NULL,
			client_msg_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			attachments_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending', 'accepted', 'failed')),
			error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (device_id, client_msg_id)
		);
		INSERT INTO prompt_commands
			(device_id, client_msg_id, session_id, prompt, status, error, created_at, updated_at)
			VALUES ('d', 'local_old', 's', 'hello', 'failed', 'offline', 1, 2);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	cmd, err := d.GetPromptCommand("d", "local_old")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Status != PromptFailed || cmd.Outcome != PromptFailed || !cmd.RetryAllowed {
		t.Fatalf("migrated command=%#v", cmd)
	}
}

func TestPromptCommandIdempotencyAndExplicitRetry(t *testing.T) {
	d := openTestDB(t)
	input := &PromptCommand{
		DeviceID:        "dev",
		ClientMsgID:     "local_1",
		SessionID:       "session",
		Prompt:          "hello",
		AttachmentsJSON: "[]",
	}
	stored, forward, err := d.RegisterPromptCommand(input, false)
	if err != nil || !forward || stored.Status != PromptRegistered {
		t.Fatalf("first register: %#v forward=%v err=%v", stored, forward, err)
	}
	stored, transitioned, err := d.MarkPromptForwarded("dev", "local_1")
	if err != nil || !transitioned || stored.Status != PromptPending {
		t.Fatalf("mark forwarded: %#v transitioned=%v err=%v", stored, transitioned, err)
	}
	stored, forward, err = d.RegisterPromptCommand(input, false)
	if err != nil || forward || stored.Status != PromptPending {
		t.Fatalf("pending replay: %#v forward=%v err=%v", stored, forward, err)
	}
	conflict := *input
	conflict.Prompt = "different"
	if _, _, err := d.RegisterPromptCommand(&conflict, false); !errors.Is(err, ErrPromptCommandConflict) {
		t.Fatalf("conflict err=%v", err)
	}

	stored, transitioned, err = d.MarkPromptFailed(
		"dev", "local_1", "offline", "transport_error", true,
	)
	if err != nil || !transitioned || stored.Status != PromptFailed || !stored.RetryAllowed {
		t.Fatalf("mark failed: %#v transitioned=%v err=%v", stored, transitioned, err)
	}
	stored, forward, err = d.RegisterPromptCommand(input, false)
	if err != nil || forward || stored.Status != PromptFailed {
		t.Fatalf("implicit failed replay: %#v forward=%v err=%v", stored, forward, err)
	}
	stored, forward, err = d.RegisterPromptCommand(input, true)
	if err != nil || !forward || stored.Status != PromptRegistered {
		t.Fatalf("explicit retry: %#v forward=%v err=%v", stored, forward, err)
	}
	stored, transitioned, err = d.MarkPromptAccepted("dev", "local_1")
	if err != nil || !transitioned || stored.Status != PromptAccepted || stored.CommitSent {
		t.Fatalf("accept: %#v transitioned=%v err=%v", stored, transitioned, err)
	}
	uncommitted, err := d.ListUncommittedAcceptedPrompts("dev", 10)
	if err != nil || len(uncommitted) != 1 || uncommitted[0].ClientMsgID != "local_1" {
		t.Fatalf("uncommitted=%#v err=%v", uncommitted, err)
	}
	if err := d.MarkPromptCommitted("dev", "local_1"); err != nil {
		t.Fatal(err)
	}
	uncommitted, err = d.ListUncommittedAcceptedPrompts("dev", 10)
	if err != nil || len(uncommitted) != 0 {
		t.Fatalf("after commit=%#v err=%v", uncommitted, err)
	}
	stored, forward, err = d.RegisterPromptCommand(input, true)
	if err != nil || forward || stored.Status != PromptAccepted {
		t.Fatalf("accepted replay: %#v forward=%v err=%v", stored, forward, err)
	}
	count, err := d.PromptCommandCount("dev", "local_1")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}

	late := *input
	late.ClientMsgID = "local_late_accept"
	if _, forward, err := d.RegisterPromptCommand(&late, false); err != nil || !forward {
		t.Fatalf("late register forward=%v err=%v", forward, err)
	}
	if _, _, err := d.MarkPromptFailed(
		"dev", late.ClientMsgID, "transport failed", "transport_error", true,
	); err != nil {
		t.Fatal(err)
	}
	stored, transitioned, err = d.MarkPromptAccepted("dev", late.ClientMsgID)
	if err != nil || !transitioned || stored.Status != PromptAccepted {
		t.Fatalf("late accept: %#v transitioned=%v err=%v", stored, transitioned, err)
	}

	unknown := *input
	unknown.ClientMsgID = "local_indeterminate"
	if _, forward, err := d.RegisterPromptCommand(&unknown, false); err != nil || !forward {
		t.Fatalf("unknown register forward=%v err=%v", forward, err)
	}
	stored, transitioned, err = d.MarkPromptFailed(
		"dev", unknown.ClientMsgID, "unknown prompt status", PromptIndeterminate, false,
	)
	if err != nil || !transitioned || stored.Status != PromptIndeterminate || stored.RetryAllowed {
		t.Fatalf("indeterminate: %#v transitioned=%v err=%v", stored, transitioned, err)
	}
	stored, forward, err = d.RegisterPromptCommand(&unknown, true)
	if err != nil || forward || stored.Status != PromptIndeterminate {
		t.Fatalf("indeterminate retry: %#v forward=%v err=%v", stored, forward, err)
	}

	tighten := *input
	tighten.ClientMsgID = "local_tighten"
	if _, forward, err := d.RegisterPromptCommand(&tighten, false); err != nil || !forward {
		t.Fatalf("tighten register forward=%v err=%v", forward, err)
	}
	if _, _, err := d.MarkPromptFailed(
		"dev", tighten.ClientMsgID, "offline", "transport_error", true,
	); err != nil {
		t.Fatal(err)
	}
	stored, transitioned, err = d.MarkPromptFailed(
		"dev", tighten.ClientMsgID, "unknown", PromptIndeterminate, false,
	)
	if err != nil || !transitioned || stored.Status != PromptIndeterminate || stored.RetryAllowed {
		t.Fatalf("tightened result: %#v transitioned=%v err=%v", stored, transitioned, err)
	}
}

func TestRegisteredPromptSurvivesRestartAndCanBeResent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	first, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	input := &PromptCommand{
		DeviceID:        "dev",
		ClientMsgID:     "local_restart",
		SessionID:       "session",
		Prompt:          "hello",
		AttachmentsJSON: "[]",
	}
	if _, forward, err := first.RegisterPromptCommand(input, false); err != nil || !forward {
		t.Fatalf("register forward=%v err=%v", forward, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stored, err := second.GetPromptCommand("dev", "local_restart")
	if err != nil || stored.Status != PromptRegistered {
		t.Fatalf("after restart: %#v err=%v", stored, err)
	}
	stored, forward, err := second.RegisterPromptCommand(input, false)
	if err != nil || !forward || stored.Status != PromptRegistered {
		t.Fatalf("restart replay: %#v forward=%v err=%v", stored, forward, err)
	}
}

func TestConsumePairCodeConcurrent(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.RegisterDevice("dev3", "Y"); err != nil {
		t.Fatal(err)
	}
	if err := d.CreatePairCode("concur99", "dev3", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	type res struct {
		id  string
		err error
	}
	ch := make(chan res, 8)
	const n = 8
	for i := 0; i < n; i++ {
		go func() {
			id, err := d.ConsumePairCode("concur99")
			ch <- res{id, err}
		}()
	}
	var ok int
	for i := 0; i < n; i++ {
		r := <-ch
		if r.err == nil && r.id == "dev3" {
			ok++
		}
	}
	// Atomic UPDATE must yield exactly one winner regardless of contention.
	if ok != 1 {
		t.Fatalf("want exactly 1 success, got %d", ok)
	}
}
