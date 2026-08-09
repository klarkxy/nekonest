package ws

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nekonest/server/internal/protocol"
)

func TestDefaultTransportModeIsSealed(t *testing.T) {
	server := &Server{}
	if got := server.TransportMode(); got != protocol.TransportSealed {
		t.Fatalf("default transport mode = %q, want %q", got, protocol.TransportSealed)
	}
}

func TestHealthReportsConfiguredTransportMode(t *testing.T) {
	server := New(testDB(t))
	if err := server.SetTransportMode(protocol.TransportOpen); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("health status=%d", res.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["transport_mode"] != "open" {
		t.Fatalf("health payload=%#v", body)
	}
}

func TestRegisterDeviceReturnsCurrentTransportModeAndRejectsMismatch(t *testing.T) {
	server := New(testDB(t))
	if err := server.SetTransportMode(protocol.TransportSealed); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/devices/register", bytes.NewBufferString(`{"name":"PC","transport_mode":"sealed"}`))
	response := httptest.NewRecorder()
	server.handleRegisterDevice(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["transport_mode"] != "sealed" {
		t.Fatalf("register body=%#v", body)
	}
	mismatch := httptest.NewRequest(http.MethodPost, "/api/devices/register", bytes.NewBufferString(`{"name":"PC","transport_mode":"open"}`))
	bad := httptest.NewRecorder()
	server.handleRegisterDevice(bad, mismatch)
	if bad.Code != http.StatusConflict {
		t.Fatalf("mismatch status=%d body=%s", bad.Code, bad.Body.String())
	}
}

func TestAttentionEventIsDeduplicatedAcrossServerInstances(t *testing.T) {
	database := testDB(t)
	first := New(database)
	second := New(database)
	msg := &protocol.NekoMessage{Payload: map[string]any{"event_id": "stable-event"}}
	if !first.acceptAttentionEvent("dev1", msg) {
		t.Fatal("first server should accept attention event")
	}
	if second.acceptAttentionEvent("dev1", msg) {
		t.Fatal("second server must not rebroadcast durable duplicate")
	}
}
