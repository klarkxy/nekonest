package ws

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/buildinfo"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
)

func newWebSocketTestServer(t *testing.T, phoneSecret string) (*Server, *httptest.Server, string) {
	t.Helper()
	database := testDB(t)
	token, err := database.RegisterDevice("dev1", "PC")
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithSecret(database, phoneSecret)
	// Integration tests exercise plaintext application frames under open mode.
	if err := server.SetTransportMode(protocol.TransportOpen); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	httpServer := httptest.NewServer(CORSMiddleware(mux))
	t.Cleanup(httpServer.Close)
	return server, httpServer, token
}

// v1Envelope stamps protocol negotiation fields for open-mode test clients.
func v1Envelope(msgType protocol.MessageType, deviceID string, payload map[string]any) *protocol.NekoMessage {
	return &protocol.NekoMessage{
		ProtocolVersion: protocol.CurrentProtocolVersion,
		TransportMode:   protocol.TransportOpen,
		Type:            msgType,
		DeviceID:        deviceID,
		Timestamp:       time.Now().Unix(),
		Payload:         payload,
	}
}

func websocketURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}

func boolPayload(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func connectDaemon(t *testing.T, httpServer *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL(httpServer.URL, "/ws/daemon"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(v1Envelope(protocol.MsgRegisterDevice, "dev1", map[string]any{
		"device_id":      "dev1",
		"token":          token,
		"daemon_version": buildinfo.Version,
	})); err != nil {
		t.Fatal(err)
	}
	var auth protocol.NekoMessage
	if err := conn.ReadJSON(&auth); err != nil {
		t.Fatal(err)
	}
	if auth.Type != protocol.MsgAuthResponse {
		t.Fatalf("auth response: %#v", auth)
	}
	if stringPayload(auth.Payload, "server_version") != buildinfo.Version {
		t.Fatalf("server version: %#v", auth.Payload)
	}
	return conn
}

func connectDaemonReady(
	t *testing.T,
	server *Server,
	httpServer *httptest.Server,
	token string,
) *websocket.Conn {
	t.Helper()
	online := make(chan string, 1)
	previous := server.connMgr.onDeviceUp
	server.connMgr.OnDeviceUp(func(deviceID, appVersion string) {
		if previous != nil {
			previous(deviceID, appVersion)
		}
		online <- deviceID
	})
	conn := connectDaemon(t, httpServer, token)
	select {
	case deviceID := <-online:
		if deviceID != "dev1" {
			t.Fatalf("unexpected online device %q", deviceID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not finish its online transition")
	}
	return conn
}

func connectPhone(t *testing.T, httpServer *httptest.Server, secret string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": []string{httpServer.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(
		websocketURL(httpServer.URL, "/ws/phone?secret="+secret),
		header,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(v1Envelope(protocol.MsgSubscribe, "dev1", map[string]any{
		"subscription_id": "subscription-test",
		"pwa_version":     buildinfo.Version,
	})); err != nil {
		t.Fatal(err)
	}
	// Explicit ACK followed by initial session and device snapshots.
	for i := 0; i < 3; i++ {
		var snapshot protocol.NekoMessage
		if err := conn.ReadJSON(&snapshot); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if snapshot.Type != protocol.MsgSubscribeAck ||
				snapshot.DeviceID != "dev1" ||
				stringPayload(snapshot.Payload, "subscription_id") != "subscription-test" ||
				stringPayload(snapshot.Payload, "server_version") != buildinfo.Version ||
				boolPayload(snapshot.Payload, "refresh_required") {
				t.Fatalf("subscribe ACK: %#v", snapshot)
			}
		}
	}
	return conn
}

func TestComponentVersionsReportRefreshAndDaemonUpdateState(t *testing.T) {
	oldLimiter := wsRateLimiter
	wsRateLimiter = newRateLimiter(100, time.Minute)
	t.Cleanup(func() { wsRateLimiter = oldLimiter })

	server, httpServer, token := newWebSocketTestServer(t, "phone-secret")
	daemon := connectDaemonReadyWithVersion(t, server, httpServer, token, "0.1.0")
	defer daemon.Close()

	healthResp, err := http.Get(httpServer.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer healthResp.Body.Close()
	var health map[string]any
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health["server_version"] != buildinfo.Version || health["protocol_version"] != protocol.CurrentProtocolVersion {
		t.Fatalf("health versions: %#v", health)
	}

	header := http.Header{"Origin": []string{httpServer.URL}}
	phone, _, err := websocket.DefaultDialer.Dial(
		websocketURL(httpServer.URL, "/ws/phone?secret=phone-secret"),
		header,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	if err := phone.WriteJSON(v1Envelope(protocol.MsgSubscribe, "dev1", map[string]any{
		"subscription_id": "version-test",
		"pwa_version":     "0.1.0",
	})); err != nil {
		t.Fatal(err)
	}

	var ack protocol.NekoMessage
	if err := phone.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Type != protocol.MsgSubscribeAck ||
		stringPayload(ack.Payload, "server_version") != buildinfo.Version ||
		stringPayload(ack.Payload, "daemon_version") != "0.1.0" ||
		!boolPayload(ack.Payload, "refresh_required") {
		t.Fatalf("version ACK: %#v", ack)
	}

	// session_list then device_list
	var snapshot protocol.NekoMessage
	if err := phone.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := phone.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != protocol.MsgDeviceList {
		t.Fatalf("device snapshot: %#v", snapshot)
	}
	rawDevices, _ := json.Marshal(snapshot.Payload["devices"])
	var devices []*protocol.Device
	if err := json.Unmarshal(rawDevices, &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DaemonVersion != "0.1.0" {
		t.Fatalf("device versions: %#v", devices)
	}

	// A replacement can authenticate before the old daemon's read loop has
	// observed its close. The phone must still receive the new live release.
	replacement, _, err := websocket.DefaultDialer.Dial(websocketURL(httpServer.URL, "/ws/daemon"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := replacement.WriteJSON(v1Envelope(protocol.MsgRegisterDevice, "dev1", map[string]any{
		"device_id":      "dev1",
		"token":          token,
		"daemon_version": "0.1.1",
	})); err != nil {
		t.Fatal(err)
	}
	var replacementAuth protocol.NekoMessage
	if err := replacement.ReadJSON(&replacementAuth); err != nil {
		t.Fatal(err)
	}
	if replacementAuth.Type != protocol.MsgAuthResponse {
		t.Fatalf("replacement auth: %#v", replacementAuth)
	}

	_ = phone.SetReadDeadline(time.Now().Add(2 * time.Second))
	var versionUpdate protocol.NekoMessage
	if err := phone.ReadJSON(&versionUpdate); err != nil {
		t.Fatal(err)
	}
	if versionUpdate.Type != protocol.MsgDeviceOnline ||
		stringPayload(versionUpdate.Payload, "daemon_version") != "0.1.1" {
		t.Fatalf("replacement version update: %#v", versionUpdate)
	}
	_ = phone.SetReadDeadline(time.Time{})
}

func connectDaemonReadyWithVersion(
	t *testing.T,
	server *Server,
	httpServer *httptest.Server,
	token, daemonVersion string,
) *websocket.Conn {
	t.Helper()
	online := make(chan string, 1)
	previous := server.connMgr.onDeviceUp
	server.connMgr.OnDeviceUp(func(deviceID, appVersion string) {
		if previous != nil {
			previous(deviceID, appVersion)
		}
		online <- deviceID
	})
	conn, _, err := websocket.DefaultDialer.Dial(websocketURL(httpServer.URL, "/ws/daemon"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(v1Envelope(protocol.MsgRegisterDevice, "dev1", map[string]any{
		"device_id":      "dev1",
		"token":          token,
		"daemon_version": daemonVersion,
	})); err != nil {
		t.Fatal(err)
	}
	var auth protocol.NekoMessage
	if err := conn.ReadJSON(&auth); err != nil {
		t.Fatal(err)
	}
	if !boolPayload(auth.Payload, "update_required") {
		t.Fatalf("daemon update state: %#v", auth.Payload)
	}
	select {
	case <-online:
	case <-time.After(2 * time.Second):
		t.Fatal("versioned daemon did not finish its online transition")
	}
	return conn
}

func expectMessageTooBigClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("close error=%v, want websocket close %d", err, websocket.CloseMessageTooBig)
	}
}

func TestUnauthenticatedFirstFrameReadLimit(t *testing.T) {
	oldLimiter := wsRateLimiter
	wsRateLimiter = newRateLimiter(100, time.Minute)
	t.Cleanup(func() { wsRateLimiter = oldLimiter })

	server, httpServer, token := newWebSocketTestServer(t, "phone-secret")
	padding := strings.Repeat("x", unauthenticatedReadLimit)

	t.Run("daemon", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(
			websocketURL(httpServer.URL, "/ws/daemon"),
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.WriteJSON(&protocol.NekoMessage{
			Type: protocol.MsgRegisterDevice,
			Payload: map[string]any{
				"device_id": "dev1",
				"token":     token,
				"padding":   padding,
			},
		}); err != nil {
			t.Fatal(err)
		}
		expectMessageTooBigClose(t, conn)
		if server.connMgr.IsDaemonOnline("dev1") {
			t.Fatal("oversized unauthenticated daemon frame registered a connection")
		}
	})

	t.Run("phone", func(t *testing.T) {
		header := http.Header{"Origin": []string{httpServer.URL}}
		conn, _, err := websocket.DefaultDialer.Dial(
			websocketURL(httpServer.URL, "/ws/phone?secret=phone-secret"),
			header,
		)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.WriteJSON(&protocol.NekoMessage{
			Type:     protocol.MsgSubscribe,
			DeviceID: "dev1",
			Payload: map[string]any{
				"subscription_id": "oversized-first-frame",
				"padding":         padding,
			},
		}); err != nil {
			t.Fatal(err)
		}
		expectMessageTooBigClose(t, conn)
		server.connMgr.mu.RLock()
		phoneCount := len(server.connMgr.phoneConns["dev1"])
		outboundCount := len(server.connMgr.phoneOutbounds)
		server.connMgr.mu.RUnlock()
		if phoneCount != 0 || outboundCount != 0 {
			t.Fatalf("oversized phone frame registered state: phones=%d outbounds=%d", phoneCount, outboundCount)
		}
	})
}

func TestPromptAckWaitsForDaemonAndAcceptedReplayIsIdempotent(t *testing.T) {
	oldLimiter := wsRateLimiter
	wsRateLimiter = newRateLimiter(100, time.Minute)
	t.Cleanup(func() { wsRateLimiter = oldLimiter })

	server, httpServer, token := newWebSocketTestServer(t, "phone-secret")
	daemon := connectDaemonReady(t, server, httpServer, token)
	phone := connectPhone(t, httpServer, "phone-secret")

	prompt := &protocol.NekoMessage{
		Type:      protocol.MsgSendPrompt,
		DeviceID:  "dev1",
		SessionID: "session-1",
		Payload: map[string]any{
			"prompt":        "hello",
			"client_msg_id": "local_prompt_1",
		},
	}
	if err := phone.WriteJSON(prompt); err != nil {
		t.Fatal(err)
	}
	var forwarded protocol.NekoMessage
	if err := daemon.ReadJSON(&forwarded); err != nil {
		t.Fatal(err)
	}
	if forwarded.Type != protocol.MsgSendPrompt || forwarded.DeviceID != "dev1" {
		t.Fatalf("forwarded: %#v", forwarded)
	}

	phoneResult := make(chan protocol.NekoMessage, 1)
	phoneErr := make(chan error, 1)
	go func() {
		var msg protocol.NekoMessage
		if err := phone.ReadJSON(&msg); err != nil {
			phoneErr <- err
			return
		}
		phoneResult <- msg
	}()
	select {
	case msg := <-phoneResult:
		t.Fatalf("server emitted premature ACK: %#v", msg)
	case err := <-phoneErr:
		t.Fatalf("phone read before daemon ACK: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := phone.WriteJSON(prompt); err != nil {
		t.Fatal(err)
	}
	var statusQuery protocol.NekoMessage
	if err := daemon.ReadJSON(&statusQuery); err != nil {
		t.Fatal(err)
	}
	if statusQuery.Type != protocol.MsgPromptStatusQuery ||
		stringPayload(statusQuery.Payload, "client_msg_id") != "local_prompt_1" {
		t.Fatalf("pending replay should query status: %#v", statusQuery)
	}
	if err := daemon.WriteJSON(&protocol.NekoMessage{
		Type:     protocol.MsgPromptNotSeen,
		DeviceID: "dev1",
		Payload:  map[string]any{"client_msg_id": "local_prompt_1"},
	}); err != nil {
		t.Fatal(err)
	}
	var recovered protocol.NekoMessage
	if err := daemon.ReadJSON(&recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Type != protocol.MsgSendPrompt ||
		stringPayload(recovered.Payload, "client_msg_id") != "local_prompt_1" ||
		stringPayload(recovered.Payload, "prompt") != "hello" {
		t.Fatalf("not-seen recovery: %#v", recovered)
	}

	if err := daemon.WriteJSON(&protocol.NekoMessage{
		Type:      protocol.MsgPromptAccepted,
		DeviceID:  "dev1",
		SessionID: "session-1",
		Payload: map[string]any{
			"client_msg_id": "local_prompt_1",
		},
	}); err != nil {
		t.Fatal(err)
	}
	var committed protocol.NekoMessage
	if err := daemon.ReadJSON(&committed); err != nil {
		t.Fatal(err)
	}
	if committed.Type != protocol.MsgPromptCommitted ||
		stringPayload(committed.Payload, "client_msg_id") != "local_prompt_1" {
		t.Fatalf("committed: %#v", committed)
	}
	select {
	case ack := <-phoneResult:
		if ack.Type != protocol.MsgPromptSent || stringPayload(ack.Payload, "client_msg_id") != "local_prompt_1" {
			t.Fatalf("ACK: %#v", ack)
		}
	case err := <-phoneErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for accepted ACK")
	}

	// A successful server write of prompt_committed is not a daemon ACK. If
	// that frame was lost, the daemon keeps its accepted journal entry and
	// repeats prompt_accepted after reconnect. Even though commit_sent is
	// already true in SQLite, the server must resend prompt_committed.
	if err := daemon.WriteJSON(&protocol.NekoMessage{
		Type:      protocol.MsgPromptAccepted,
		DeviceID:  "dev1",
		SessionID: "session-1",
		Payload: map[string]any{
			"client_msg_id": "local_prompt_1",
		},
	}); err != nil {
		t.Fatal(err)
	}
	var duplicateAcceptedCommit protocol.NekoMessage
	if err := daemon.ReadJSON(&duplicateAcceptedCommit); err != nil {
		t.Fatal(err)
	}
	if duplicateAcceptedCommit.Type != protocol.MsgPromptCommitted ||
		stringPayload(duplicateAcceptedCommit.Payload, "client_msg_id") != "local_prompt_1" {
		t.Fatalf("duplicate accepted commit recovery: %#v", duplicateAcceptedCommit)
	}
	var duplicateAcceptedAck protocol.NekoMessage
	if err := phone.ReadJSON(&duplicateAcceptedAck); err != nil {
		t.Fatal(err)
	}
	if duplicateAcceptedAck.Type != protocol.MsgPromptSent ||
		stringPayload(duplicateAcceptedAck.Payload, "client_msg_id") != "local_prompt_1" {
		t.Fatalf("duplicate accepted ACK recovery: %#v", duplicateAcceptedAck)
	}

	if err := phone.WriteJSON(prompt); err != nil {
		t.Fatal(err)
	}
	var replayAck protocol.NekoMessage
	if err := phone.ReadJSON(&replayAck); err != nil {
		t.Fatal(err)
	}
	if replayAck.Type != protocol.MsgPromptSent {
		t.Fatalf("accepted replay: %#v", replayAck)
	}
	var replayCommit protocol.NekoMessage
	if err := daemon.ReadJSON(&replayCommit); err != nil {
		t.Fatal(err)
	}
	if replayCommit.Type != protocol.MsgPromptCommitted {
		t.Fatalf("accepted replay should only commit, got: %#v", replayCommit)
	}

	if err := daemon.WriteJSON(&protocol.NekoMessage{
		Type:      protocol.MsgSessionUpdate,
		DeviceID:  "spoofed-device",
		SessionID: "session-1",
		Payload:   map[string]any{"status": "idle"},
	}); err != nil {
		t.Fatal(err)
	}
	var normalized protocol.NekoMessage
	if err := phone.ReadJSON(&normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Type != protocol.MsgSessionUpdate || normalized.DeviceID != "dev1" {
		t.Fatalf("daemon identity was not normalized: %#v", normalized)
	}

	messages, err := server.db.GetMessages("dev1", "session-1", 10)
	if err != nil || len(messages) != 1 || messages[0].ID != "local_prompt_1" {
		t.Fatalf("persisted messages: %#v err=%v", messages, err)
	}
}

func TestAcceptedPromptPersistenceFailureDefersCommitUntilReconnectHealing(t *testing.T) {
	oldLimiter := wsRateLimiter
	wsRateLimiter = newRateLimiter(100, time.Minute)
	t.Cleanup(func() { wsRateLimiter = oldLimiter })

	dbPath := filepath.Join(t.TempDir(), "persistence-failure.db")
	database, err := db.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	token, err := database.RegisterDevice("dev1", "PC")
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithSecret(database, "phone-secret")
	if err := server.SetTransportMode(protocol.TransportOpen); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	httpServer := httptest.NewServer(CORSMiddleware(mux))
	t.Cleanup(httpServer.Close)

	// Inject a deterministic history-write failure without preventing the
	// prompt_commands acceptance transition itself.
	adminDB, err := sql.Open(
		"sqlite",
		dbPath+"?_pragma=busy_timeout(5000)",
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if _, err := adminDB.Exec(`
		CREATE TRIGGER fail_session_message_insert
		BEFORE INSERT ON session_messages
		BEGIN
			SELECT RAISE(FAIL, 'forced session persistence failure');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	daemon := connectDaemonReady(t, server, httpServer, token)
	phone := connectPhone(t, httpServer, "phone-secret")
	prompt := &protocol.NekoMessage{
		Type:      protocol.MsgSendPrompt,
		DeviceID:  "dev1",
		SessionID: "session-persist",
		Payload: map[string]any{
			"prompt":        "must persist before ACK",
			"client_msg_id": "local_persist_1",
		},
	}
	if err := phone.WriteJSON(prompt); err != nil {
		t.Fatal(err)
	}
	var forwarded protocol.NekoMessage
	if err := daemon.ReadJSON(&forwarded); err != nil {
		t.Fatal(err)
	}
	if forwarded.Type != protocol.MsgSendPrompt {
		t.Fatalf("forwarded: %#v", forwarded)
	}
	if err := daemon.WriteJSON(&protocol.NekoMessage{
		Type:      protocol.MsgPromptAccepted,
		DeviceID:  "dev1",
		SessionID: "session-persist",
		Payload:   map[string]any{"client_msg_id": "local_persist_1"},
	}); err != nil {
		t.Fatal(err)
	}

	daemonFrames := make(chan protocol.NekoMessage, 1)
	daemonErrors := make(chan error, 1)
	go func() {
		var msg protocol.NekoMessage
		if err := daemon.ReadJSON(&msg); err != nil {
			daemonErrors <- err
			return
		}
		daemonFrames <- msg
	}()
	phoneFrames := make(chan protocol.NekoMessage, 1)
	phoneErrors := make(chan error, 1)
	go func() {
		var msg protocol.NekoMessage
		if err := phone.ReadJSON(&msg); err != nil {
			phoneErrors <- err
			return
		}
		phoneFrames <- msg
	}()
	select {
	case msg := <-daemonFrames:
		t.Fatalf("commit emitted before history persistence: %#v", msg)
	case err := <-daemonErrors:
		t.Fatalf("daemon read before reconnect: %v", err)
	case msg := <-phoneFrames:
		t.Fatalf("phone ACK emitted before history persistence: %#v", msg)
	case err := <-phoneErrors:
		t.Fatalf("phone read before reconnect: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	cmd, err := database.GetPromptCommand("dev1", "local_persist_1")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Status != db.PromptAccepted || cmd.CommitSent {
		t.Fatalf("accepted command should remain uncommitted: %#v", cmd)
	}
	if count, err := database.GetMessageCount("dev1", "session-persist"); err != nil || count != 0 {
		t.Fatalf("history count=%d err=%v", count, err)
	}

	if _, err := adminDB.Exec(`DROP TRIGGER fail_session_message_insert`); err != nil {
		t.Fatal(err)
	}
	_ = daemon.Close()

	// On reconnect, replayPromptCommits must first heal the missing user
	// message and only then allow the daemon to discard its accepted journal.
	reconnected := connectDaemon(t, httpServer, token)
	var healedCommit protocol.NekoMessage
	if err := reconnected.ReadJSON(&healedCommit); err != nil {
		t.Fatal(err)
	}
	if healedCommit.Type != protocol.MsgPromptCommitted ||
		stringPayload(healedCommit.Payload, "client_msg_id") != "local_persist_1" {
		t.Fatalf("reconnect healing commit: %#v", healedCommit)
	}
	messages, err := database.GetMessages("dev1", "session-persist", 10)
	if err != nil || len(messages) != 1 || messages[0].ID != "local_persist_1" {
		t.Fatalf("healed history: %#v err=%v", messages, err)
	}
}

func TestPhoneRESTAndWebSocketOriginAndAuth(t *testing.T) {
	oldLimiter := wsRateLimiter
	wsRateLimiter = newRateLimiter(100, time.Minute)
	t.Cleanup(func() { wsRateLimiter = oldLimiter })

	_, httpServer, _ := newWebSocketTestServer(t, "phone-secret")

	req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/devices", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-Neko-Secret", "phone-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin REST status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, httpServer.URL+"/api/devices", nil)
	req.Header.Set("Origin", httpServer.URL)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated REST status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, httpServer.URL+"/api/devices", nil)
	req.Header.Set("Origin", httpServer.URL)
	req.Header.Set("X-Neko-Secret", "phone-secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("same-origin authenticated REST status=%d", resp.StatusCode)
	}

	header := http.Header{"Origin": []string{"https://evil.example"}}
	conn, resp, err := websocket.DefaultDialer.Dial(
		websocketURL(httpServer.URL, "/ws/phone?secret=phone-secret"),
		header,
	)
	if conn != nil {
		conn.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin WS err=%v response=%v", err, resp)
	}
	resp.Body.Close()

	header.Set("Origin", httpServer.URL)
	conn, _, err = websocket.DefaultDialer.Dial(websocketURL(httpServer.URL, "/ws/phone"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(v1Envelope(protocol.MsgSubscribe, "dev1", nil)); err != nil {
		t.Fatal(err)
	}
	var unauthorized protocol.NekoMessage
	if err := conn.ReadJSON(&unauthorized); err != nil {
		t.Fatal(err)
	}
	if unauthorized.Type != protocol.MsgError || stringPayload(unauthorized.Payload, "message") != "unauthorized" {
		t.Fatalf("unauthorized WS response: %#v", unauthorized)
	}
}

func TestPushSubscribeValidation(t *testing.T) {
	_, httpServer, _ := newWebSocketTestServer(t, "phone-secret")
	post := func(payload map[string]any) int {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(
			http.MethodPost,
			httpServer.URL+"/api/push/subscribe",
			bytes.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", httpServer.URL)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Neko-Secret", "phone-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	valid := map[string]any{
		"device_id": "dev1",
		"endpoint":  "https://fcm.googleapis.com/fcm/send/test",
		"p256dh": func() string {
			key := make([]byte, 65)
			key[0] = 0x04
			return base64.RawURLEncoding.EncodeToString(key)
		}(),
		"auth": base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}
	missingKey := map[string]any{}
	for key, value := range valid {
		missingKey[key] = value
	}
	delete(missingKey, "auth")
	if status := post(missingKey); status != http.StatusBadRequest {
		t.Fatalf("missing auth status=%d", status)
	}

	unknown := map[string]any{}
	for key, value := range valid {
		unknown[key] = value
	}
	unknown["device_id"] = "unknown"
	if status := post(unknown); status != http.StatusNotFound {
		t.Fatalf("unknown device status=%d", status)
	}

	tooLong := map[string]any{}
	for key, value := range valid {
		tooLong[key] = value
	}
	tooLong["p256dh"] = strings.Repeat("x", 513)
	if status := post(tooLong); status != http.StatusBadRequest {
		t.Fatalf("long key status=%d", status)
	}

	if status := post(valid); status != http.StatusOK {
		t.Fatalf("valid subscription status=%d", status)
	}
}

func TestMessagesRESTReportsTruncation(t *testing.T) {
	server, httpServer, _ := newWebSocketTestServer(t, "phone-secret")
	for i := 1; i <= 3; i++ {
		if err := server.db.SaveMessage("dev1", "session-history", &protocol.SessionMessage{
			ID:        "m" + string(rune('0'+i)),
			Role:      "assistant",
			Content:   "message",
			Type:      "text",
			Timestamp: int64(i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(
		http.MethodGet,
		httpServer.URL+"/api/messages?device_id=dev1&session_id=session-history&limit=2",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", httpServer.URL)
	req.Header.Set("X-Neko-Secret", "phone-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Messages  []*protocol.SessionMessage `json:"messages"`
		Limit     int                        `json:"limit"`
		Truncated bool                       `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || body.Limit != 2 || !body.Truncated ||
		len(body.Messages) != 2 || body.Messages[0].ID != "m2" || body.Messages[1].ID != "m3" {
		t.Fatalf("status=%d body=%#v", resp.StatusCode, body)
	}
}

func TestFailedResubscribeCannotRouteCommandsToOldDevice(t *testing.T) {
	oldLimiter := wsRateLimiter
	wsRateLimiter = newRateLimiter(100, time.Minute)
	t.Cleanup(func() { wsRateLimiter = oldLimiter })

	server, httpServer, token := newWebSocketTestServer(t, "phone-secret")
	daemon := connectDaemonReady(t, server, httpServer, token)
	phone := connectPhone(t, httpServer, "phone-secret")

	if err := phone.WriteJSON(&protocol.NekoMessage{
		Type:     protocol.MsgSubscribe,
		DeviceID: "missing-device",
		Payload:  map[string]any{"subscription_id": "switch-1"},
	}); err != nil {
		t.Fatal(err)
	}
	var switchError protocol.NekoMessage
	if err := phone.ReadJSON(&switchError); err != nil {
		t.Fatal(err)
	}
	if switchError.Type != protocol.MsgError {
		t.Fatalf("failed switch response: %#v", switchError)
	}
	if switchError.DeviceID != "missing-device" ||
		stringPayload(switchError.Payload, "subscription_id") != "switch-1" {
		t.Fatalf("failed switch identity: %#v", switchError)
	}

	if err := phone.WriteJSON(&protocol.NekoMessage{
		Type:      protocol.MsgSendPrompt,
		DeviceID:  "missing-device",
		SessionID: "session-1",
		Payload: map[string]any{
			"prompt":        "must not reach dev1",
			"client_msg_id": "local_wrong_device",
		},
	}); err != nil {
		t.Fatal(err)
	}
	var routeError protocol.NekoMessage
	if err := phone.ReadJSON(&routeError); err != nil {
		t.Fatal(err)
	}
	if routeError.Type != protocol.MsgError ||
		stringPayload(routeError.Payload, "message") != "device_id does not match subscription" {
		t.Fatalf("wrong-device response: %#v", routeError)
	}

	daemonResult := make(chan protocol.NekoMessage, 1)
	daemonErr := make(chan error, 1)
	go func() {
		var msg protocol.NekoMessage
		if err := daemon.ReadJSON(&msg); err != nil {
			daemonErr <- err
			return
		}
		daemonResult <- msg
	}()
	select {
	case msg := <-daemonResult:
		t.Fatalf("wrong-device prompt reached old subscription: %#v", msg)
	case err := <-daemonErr:
		t.Fatalf("daemon read: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}
