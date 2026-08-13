package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/config"
	"github.com/nekonest/daemon/internal/connection"
)

func TestRegistrationHTTPClientRejectsRedirectWithoutForwardingBootstrap(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	req, err := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Neko-Bootstrap", "one-time-secret")
	resp, err := newRegistrationHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || targetCalls.Load() != 0 {
		t.Fatalf("redirect status=%d target calls=%d", resp.StatusCode, targetCalls.Load())
	}
}

func TestDecodeRegistrationResponseIsBounded(t *testing.T) {
	oversized := strings.Repeat("x", maxRegistrationResponseBytes+1)
	if _, err := decodeRegistrationResponse(strings.NewReader(oversized)); err == nil {
		t.Fatal("oversized registration response accepted")
	}
}

func TestRegistrationResponseErrorUsesStructuredError(t *testing.T) {
	err := registrationResponseError(http.StatusServiceUnavailable, strings.NewReader(`{"error_code":"service_provisioning","message":"starting","retryable":true,"retry_after_seconds":5}`))
	remote, ok := err.(*connection.RemoteError)
	if !ok || !remote.Retryable() || remote.Code != "service_provisioning" {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestRegistrationConfigPersistsOriginalStableEndpoint(t *testing.T) {
	result := registrationResponse{
		DeviceID:        "device-a",
		Token:           "device-token",
		TransportMode:   config.TransportSealed,
		ConnectionState: registrationConnectionProvisioning,
	}
	cfg, state, err := registrationConfig("wss://connect.example", result, config.TransportSealed)
	if err != nil {
		t.Fatal(err)
	}
	if state != registrationConnectionProvisioning || cfg.ServerURL != "wss://connect.example" {
		t.Fatalf("config=%#v state=%q", cfg, state)
	}
}

func TestRegistrationConfigDefaultsConnectionStateToReady(t *testing.T) {
	cfg, state, err := registrationConfig("wss://self.example", registrationResponse{
		DeviceID: "device-a", Token: "device-token", TransportMode: config.TransportOpen,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state != registrationConnectionReady || cfg.ServerURL != "wss://self.example" {
		t.Fatalf("config=%#v state=%q", cfg, state)
	}
}

func TestCompleteDeviceRegistrationRetriesApprovedServiceErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error_code":"service_provisioning","message":"starting","retryable":true,"retry_after_seconds":1}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_id":"device-a","token":"tok","name":"Host","transport_mode":"sealed","connection_state":"ready"}`))
	}))
	defer server.Close()

	var waited time.Duration
	result, err := completeDeviceRegistration(newRegistrationHTTPClient(), server.URL, []byte(`{}`), "", func(delay time.Duration) bool {
		waited = delay
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || result.DeviceID != "device-a" || waited != time.Second {
		t.Fatalf("calls=%d result=%#v waited=%s", calls.Load(), result, waited)
	}
}

func TestCompleteDeviceRegistrationStopsOnTerminalError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error_code":"device_credential_invalid","message":"bad","retryable":false}`))
	}))
	defer server.Close()

	_, err := completeDeviceRegistration(newRegistrationHTTPClient(), server.URL, []byte(`{}`), "", func(time.Duration) bool { return true })
	if err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestRegistrationConfigRejectsUnknownConnectionState(t *testing.T) {
	_, _, err := registrationConfig("wss://connect.example", registrationResponse{
		DeviceID: "device-a", Token: "device-token", TransportMode: config.TransportSealed, ConnectionState: "tenant_redirect",
	}, "")
	if err == nil || !strings.Contains(err.Error(), "connection_state") {
		t.Fatalf("error=%v", err)
	}
}
