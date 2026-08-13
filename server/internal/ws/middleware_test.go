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
)

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
