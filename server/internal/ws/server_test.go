package ws

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekonest/server/internal/opslog"
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

func TestLoggingMiddlewareRecordsRouteStatusAndNotRawURL(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "json", Level: slog.LevelInfo}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	mux := http.NewServeMux()
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
	req := httptest.NewRequest(http.MethodGet, "/api/devices?token=token-sentinel", nil)
	LoggingMiddleware(mux).ServeHTTP(httptest.NewRecorder(), req)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["route"] != "/api/devices" || record["status"] != float64(http.StatusCreated) {
		t.Fatalf("unexpected HTTP log: %#v", record)
	}
	if _, ok := record["duration_ms"]; !ok {
		t.Fatalf("missing duration: %#v", record)
	}
	if strings.Contains(output.String(), "raw-user-input") || strings.Contains(output.String(), "token-sentinel") {
		t.Fatalf("raw URL leaked: %q", output.String())
	}
}

func TestLoggingMiddlewarePreservesWebSocketResponseWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws/phone", nil)
	called := false
	LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if w != recorder {
			t.Fatal("websocket handler received a wrapped ResponseWriter")
		}
	})).ServeHTTP(recorder, req)
	if !called {
		t.Fatal("websocket handler was not called")
	}
}

func TestLoggingMiddlewareRedactsUnknownAPIPath(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "json", Level: slog.LevelInfo}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	req := httptest.NewRequest(http.MethodGet, "/api/private/path-sentinel?token=query-sentinel", nil)
	LoggingMiddleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(output.String(), "path-sentinel") || strings.Contains(output.String(), "query-sentinel") {
		t.Fatalf("unknown API path leaked: %q", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["route"] != "api_unmatched" {
		t.Fatalf("unexpected route label: %#v", record)
	}
}

func TestRejectedFrameLogRedactsUnknownMessageType(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "json", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	hostile := protocol.MessageType("prompt-sentinel C:\\Users\\path-sentinel?token=secret-sentinel")
	opslog.Warn("server.ws", "frame_rejected", "frame rejected", "frame_type", safeMessageTypeForLog(hostile))
	if strings.Contains(output.String(), "prompt-sentinel") || strings.Contains(output.String(), "path-sentinel") || strings.Contains(output.String(), "secret-sentinel") {
		t.Fatalf("hostile message type leaked: %q", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["frame_type"] != "unknown" {
		t.Fatalf("frame_type=%v want unknown", record["frame_type"])
	}
}

func TestDeviceRegistrationLogNormalizesOSWithoutChangingPersistence(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "json", Level: slog.LevelInfo}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")

	database := testDB(t)
	server := New(database)
	hostileOS := "darwin\nsecret-sentinel C:\\Users\\path-sentinel"
	payload, err := json.Marshal(map[string]string{
		"device_id":      "device-hostile-os",
		"name":           "Host",
		"os":             hostileOS,
		"transport_mode": "sealed",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/devices/register", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	server.handleRegisterDevice(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(output.String(), "secret-sentinel") || strings.Contains(output.String(), "path-sentinel") {
		t.Fatalf("hostile OS leaked into operator log: %q", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["os"] != "unknown" {
		t.Fatalf("logged os=%v want unknown", record["os"])
	}
	devices, err := database.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].OS != hostileOS {
		t.Fatalf("registration persistence changed: %#v", devices)
	}
}

func TestSafeDeviceOSForLogAllowlist(t *testing.T) {
	for input, want := range map[string]string{
		"windows":   "windows",
		" WINDOWS ": "windows",
		"linux":     "linux",
		"Linux":     "linux",
		"darwin":    "unknown",
		"":          "unknown",
	} {
		if got := safeDeviceOSForLog(input); got != want {
			t.Fatalf("safeDeviceOSForLog(%q)=%q want=%q", input, got, want)
		}
	}
}
