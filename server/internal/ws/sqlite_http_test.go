package ws

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekonest/server/internal/protocol"
)

func TestRegisterDeviceReturnsCurrentTransportModeAndRejectsMismatch(t *testing.T) {
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	database := testDB(t)
	engine := NewCore(database, "")
	if err := engine.SetTransportMode(protocol.TransportSealed); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	engine.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/devices/register", bytes.NewBufferString(`{"name":"PC","transport_mode":"sealed"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["transport_mode"] != "sealed" || body["connection_state"] != "ready" {
		t.Fatalf("register body=%#v", body)
	}

	mismatch := httptest.NewRequest(http.MethodPost, "/api/devices/register", bytes.NewBufferString(`{"name":"PC","transport_mode":"open"}`))
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, mismatch)
	if bad.Code != http.StatusConflict {
		t.Fatalf("mismatch status=%d body=%s", bad.Code, bad.Body.String())
	}
}

func TestHandleRegisterRequiresBootstrapWhenSecret(t *testing.T) {
	database := testDB(t)
	engine := NewCore(database, "phone")
	mux := http.NewServeMux()
	engine.RegisterRoutes(mux)
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}

	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "boot")
	req2 := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"nest"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Neko-Bootstrap", "boot")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("register %d %s", rr2.Code, rr2.Body.String())
	}
}

func TestPairGenerateAndConsumeHTTP(t *testing.T) {
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "boot")
	database := testDB(t)
	engine := NewCore(database, "phone")
	mux := http.NewServeMux()
	engine.RegisterRoutes(mux)

	reg := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"p"}`))
	reg.Header.Set("Content-Type", "application/json")
	reg.Header.Set("X-Neko-Bootstrap", "boot")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, reg)
	var dev map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &dev)

	genBody := `{"device_id":"` + dev["device_id"].(string) + `","token":"` + dev["token"].(string) + `"}`
	gen := httptest.NewRequest(http.MethodPost, "/api/pair/generate", strings.NewReader(genBody))
	gen.Header.Set("Content-Type", "application/json")
	gr := httptest.NewRecorder()
	mux.ServeHTTP(gr, gen)
	if gr.Code != http.StatusOK {
		t.Fatalf("gen %d %s", gr.Code, gr.Body.String())
	}
	var codeResp map[string]any
	_ = json.Unmarshal(gr.Body.Bytes(), &codeResp)
	code, _ := codeResp["code"].(string)

	con := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(`{"code":"`+code+`"}`))
	con.Header.Set("Content-Type", "application/json")
	con.Header.Set("Authorization", "Bearer phone")
	cr := httptest.NewRecorder()
	mux.ServeHTTP(cr, con)
	if cr.Code != http.StatusOK {
		t.Fatalf("consume %d %s", cr.Code, cr.Body.String())
	}
}

func TestHandleMessages(t *testing.T) {
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	database := testDB(t)
	engine := NewCore(database, "")
	mux := http.NewServeMux()
	engine.RegisterRoutes(mux)
	reg := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"t"}`))
	reg.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, reg)
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	id := out["device_id"].(string)
	if err := database.SaveMessage(id, "sess1", &protocol.SessionMessage{
		ID: "m1", Role: "user", Content: "hi", Type: "text", Timestamp: 1,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/messages?device_id="+id+"&session_id=sess1&limit=10", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "hi") {
		t.Fatalf("%d %s", res.Code, res.Body.String())
	}
}

func TestHealthReportsConfiguredTransportMode(t *testing.T) {
	engine := NewCore(testDB(t), "")
	if err := engine.SetTransportMode(protocol.TransportOpen); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	engine.RegisterRoutes(mux)
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

func TestDeviceRegistrationLogNormalizesOSWithoutChangingPersistence(t *testing.T) {
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	database := testDB(t)
	engine := NewCore(database, "")
	mux := http.NewServeMux()
	engine.RegisterRoutes(mux)
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
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", response.Code, response.Body.String())
	}
	devices, err := database.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].OS != hostileOS {
		t.Fatalf("registration persistence changed: %#v", devices)
	}
}
