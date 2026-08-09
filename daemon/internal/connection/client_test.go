package connection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/daemon/internal/buildinfo"
)

func websocketTestURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func testAuthResponse(mode string) map[string]any {
	return map[string]any{
		"type":           "auth_response",
		"transport_mode": mode,
		"payload": map[string]any{
			"transport_mode": mode,
		},
	}
}

func TestConnectAdvertisesDaemonApplicationVersion(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	authFrames := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			return
		}
		authFrames <- frame
		_ = conn.WriteJSON(map[string]any{
			"type":           "auth_response",
			"transport_mode": "open",
			"payload": map[string]any{
				"server_version": buildinfo.Version,
				"transport_mode": "open",
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := NewClient(context.Background(), websocketTestURL(server.URL), "device", "token")
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	select {
	case frame := <-authFrames:
		payload, _ := frame["payload"].(map[string]any)
		if payload["daemon_version"] != buildinfo.Version {
			t.Fatalf("daemon version payload: %#v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon auth frame not received")
	}
}

func TestConnectUsesConfiguredModeAndRejectsAuthModeMismatch(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	authFrames := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) == nil {
			authFrames <- frame
		}
		_ = conn.WriteJSON(testAuthResponse("open"))
	}))
	defer server.Close()

	client := NewClient(context.Background(), websocketTestURL(server.URL), "device", "token", "sealed")
	if err := client.Connect(); err == nil || !strings.Contains(err.Error(), "transport_mode mismatch") {
		t.Fatalf("Connect error = %v, want transport mismatch", err)
	}
	select {
	case frame := <-authFrames:
		if frame["transport_mode"] != "sealed" {
			t.Fatalf("auth transport_mode = %#v", frame["transport_mode"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("auth frame not received")
	}
}

func TestHeartbeatCarriesNegotiatedTransportEnvelope(t *testing.T) {
	now := time.Unix(1234, 0)
	client := NewClient(context.Background(), "ws://example.invalid", "device-a", "token", "open")

	frame := client.heartbeatMessage(now)
	if frame["protocol_version"] != "1.1" || frame["transport_mode"] != "open" {
		t.Fatalf("heartbeat envelope = %#v", frame)
	}
	if frame["type"] != "heartbeat" || frame["device_id"] != "device-a" || frame["timestamp"] != int64(1234) {
		t.Fatalf("heartbeat identity = %#v", frame)
	}
}

func TestConnectDiscardsSocketWhenServerURLChangesDuringAuth(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	authStarted := make(chan struct{})
	releaseFirst := make(chan struct{})

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		close(authStarted)
		<-releaseFirst
		_ = conn.WriteJSON(testAuthResponse("open"))
	}))
	defer first.Close()

	var secondAuths atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		secondAuths.Add(1)
		_ = conn.WriteJSON(testAuthResponse("open"))
		// Keep the committed socket alive until the test closes the client.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer second.Close()

	client := NewClient(context.Background(), websocketTestURL(first.URL), "device", "token")
	result := make(chan error, 1)
	go func() {
		result <- client.Connect()
	}()

	select {
	case <-authStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first authentication did not start")
	}
	client.SetServerURL(websocketTestURL(second.URL))
	close(releaseFirst)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Connect returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Connect did not retry the new endpoint")
	}
	defer client.Close()

	client.mu.Lock()
	gotURL := client.serverURL
	connected := client.connected
	client.mu.Unlock()
	if gotURL != websocketTestURL(second.URL) || !connected {
		t.Fatalf("committed stale connection: url=%q connected=%v", gotURL, connected)
	}
	if secondAuths.Load() != 1 {
		t.Fatalf("new endpoint auth count = %d, want 1", secondAuths.Load())
	}
}

func TestSetServerURLLinearizesAgainstInFlightMessageDispatch(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		if err := conn.WriteJSON(testAuthResponse("open")); err != nil {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := NewClient(context.Background(), websocketTestURL(server.URL), "device", "token")
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	client.mu.Lock()
	oldConn := client.conn
	client.mu.Unlock()
	if oldConn == nil {
		t.Fatal("missing current connection")
	}

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	var calls atomic.Int32
	client.OnMessage(func([]byte) {
		if calls.Add(1) == 1 {
			close(callbackStarted)
			<-releaseCallback
			close(callbackDone)
		}
	})

	go client.dispatchMessage(oldConn, []byte(`{"type":"old"}`))
	select {
	case <-callbackStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("old callback did not start")
	}

	switchStarted := make(chan struct{})
	switchReturned := make(chan struct{})
	go func() {
		close(switchStarted)
		client.SetServerURL("ws://127.0.0.1:1")
		close(switchReturned)
	}()
	<-switchStarted
	select {
	case <-switchReturned:
		t.Fatal("SetServerURL returned while an old callback was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseCallback)
	select {
	case <-callbackDone:
	case <-time.After(2 * time.Second):
		t.Fatal("old callback did not finish")
	}
	select {
	case <-switchReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("SetServerURL did not return after the old callback finished")
	}

	// Even if ReadMessage had already produced a final old frame, dispatching it
	// after the switch linearization point must be a no-op.
	client.dispatchMessage(oldConn, []byte(`{"type":"stale"}`))
	if got := calls.Load(); got != 1 {
		t.Fatalf("stale connection callback ran after SetServerURL returned: calls=%d", got)
	}
}

func TestSetServerURLAndPublishMakesEndpointAndRuntimeStateAtomic(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	newAuth := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			if err := conn.WriteJSON(testAuthResponse("open")); err != nil {
				return
			}
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}))
	}

	oldServer := newAuth()
	defer oldServer.Close()
	newServer := newAuth()
	defer newServer.Close()

	client := NewClient(context.Background(), websocketTestURL(oldServer.URL), "device", "token")
	if err := client.Connect(); err != nil {
		t.Fatalf("connect old endpoint: %v", err)
	}
	defer client.Close()

	client.mu.Lock()
	oldConn := client.conn
	client.mu.Unlock()
	if oldConn == nil {
		t.Fatal("missing old connection")
	}

	type runtimeState struct {
		serverURL string
	}
	var runtimeCfg atomic.Pointer[runtimeState]
	runtimeCfg.Store(&runtimeState{serverURL: websocketTestURL(oldServer.URL)})

	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	published := make(chan struct{})
	var (
		observedMu sync.Mutex
		observed   []string
	)
	client.OnMessage(func([]byte) {
		state := runtimeCfg.Load()
		observedMu.Lock()
		observed = append(observed, state.serverURL)
		call := len(observed)
		observedMu.Unlock()
		if call == 1 {
			close(oldStarted)
			<-releaseOld
		}
	})

	go client.dispatchMessage(oldConn, []byte(`{"type":"old"}`))
	select {
	case <-oldStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("old callback did not start")
	}

	newURL := websocketTestURL(newServer.URL)
	switchDone := make(chan struct{})
	go func() {
		client.SetServerURLAndPublish(newURL, func() {
			runtimeCfg.Store(&runtimeState{serverURL: newURL})
			close(published)
		})
		close(switchDone)
	}()

	select {
	case <-published:
		t.Fatal("runtime config was published while the old callback was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseOld)
	select {
	case <-switchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("endpoint/config switch did not finish")
	}
	select {
	case <-published:
	default:
		t.Fatal("runtime config was not published before switch returned")
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("connect new endpoint: %v", err)
	}
	client.mu.Lock()
	newConn := client.conn
	client.mu.Unlock()
	if newConn == nil || newConn == oldConn {
		t.Fatal("missing distinct new connection")
	}
	client.dispatchMessage(newConn, []byte(`{"type":"new"}`))

	observedMu.Lock()
	got := append([]string(nil), observed...)
	observedMu.Unlock()
	want := []string{websocketTestURL(oldServer.URL), newURL}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("callback runtime states = %q, want %q", got, want)
	}
}
