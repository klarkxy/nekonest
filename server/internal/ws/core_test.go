package ws

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
	coreprotocol "github.com/klarkxy/nekonest/relaycore/protocol"
	"github.com/nekonest/server/internal/db"
)

// This is the standalone production composition test: it uses the real SQLite
// adapter and the public relaycore.Engine registered by cmd/server.
func TestStandaloneCoreRegistrationAndDaemonWebSocket(t *testing.T) {
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	database, err := db.New(filepath.Join(t.TempDir(), "nekonest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	engine := NewCore(database, "")
	defer engine.Close()

	server := httptest.NewServer(engine.Handler())
	defer server.Close()

	body := bytes.NewBufferString(`{"device_id":"host-a","name":"Host A","os":"windows","transport_mode":"sealed"}`)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/devices/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("registration status=%d", response.StatusCode)
	}
	var registration coreprotocol.DeviceRegistrationResponse
	if err := json.NewDecoder(response.Body).Decode(&registration); err != nil {
		t.Fatal(err)
	}
	if registration.DeviceID != "host-a" || registration.Token == "" || registration.ConnectionState != coreprotocol.ConnectionReady {
		t.Fatalf("registration=%#v", registration)
	}

	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws/daemon", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	auth := coreprotocol.NewMessage(coreprotocol.MsgRegisterDevice, registration.DeviceID)
	auth.TransportMode = coreprotocol.TransportSealed
	auth.Payload = map[string]any{
		"device_id":      registration.DeviceID,
		"token":          registration.Token,
		"daemon_version": "test",
	}
	if err := conn.WriteJSON(auth); err != nil {
		t.Fatal(err)
	}
	var authResponse coreprotocol.NekoMessage
	if err := conn.ReadJSON(&authResponse); err != nil {
		t.Fatal(err)
	}
	if authResponse.Type != coreprotocol.MsgAuthResponse || authResponse.ProtocolVersion != coreprotocol.CurrentProtocolVersion {
		t.Fatalf("auth response=%#v", authResponse)
	}
}

func TestStandaloneCoreHealthReportsProtocol13(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "nekonest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	engine := NewCore(database, "")
	defer engine.Close()
	res := httptest.NewRecorder()
	engine.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", res.Code, res.Body.String())
	}
	var health map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["protocol_version"] != coreprotocol.CurrentProtocolVersion {
		t.Fatalf("health=%#v", health)
	}
}
