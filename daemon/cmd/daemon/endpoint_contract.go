package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nekonest/daemon/internal/config"
	"github.com/nekonest/daemon/internal/connection"
)

const maxRegistrationResponseBytes = 64 << 10

const (
	registrationConnectionReady        = "ready"
	registrationConnectionProvisioning = "provisioning"
)

// registrationResponse is the stable device registration contract. The
// endpoint which accepted registration remains the daemon's only service
// endpoint; provisioning never provides a tenant-specific relay URL.
type registrationResponse struct {
	DeviceID          string `json:"device_id"`
	Token             string `json:"token"`
	Name              string `json:"name"`
	TransportMode     string `json:"transport_mode"`
	ConnectionState   string `json:"connection_state"`
	RetryAfterSeconds *int64 `json:"retry_after_seconds"`
}

const maxRegistrationAttempts = 20

func newRegistrationHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// X-Neko-Bootstrap is a one-time credential. Never let a redirect
			// move it to another request or make registration origin ambiguous.
			return http.ErrUseLastResponse
		},
	}
}

func decodeRegistrationResponse(body io.Reader) (registrationResponse, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxRegistrationResponseBytes+1))
	if err != nil {
		return registrationResponse{}, err
	}
	if len(data) > maxRegistrationResponseBytes {
		return registrationResponse{}, fmt.Errorf("registration response is too large")
	}
	var result registrationResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return registrationResponse{}, err
	}
	return result, nil
}

func postDeviceRegistration(client *http.Client, registerURL string, body []byte, bootstrapToken string) (registrationResponse, error) {
	req, err := http.NewRequest(http.MethodPost, registerURL, bytes.NewReader(body))
	if err != nil {
		return registrationResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bootstrapToken != "" {
		req.Header.Set("X-Neko-Bootstrap", bootstrapToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return registrationResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return registrationResponse{}, registrationResponseError(resp.StatusCode, resp.Body)
	}
	return decodeRegistrationResponse(resp.Body)
}

func completeDeviceRegistration(client *http.Client, registerURL string, body []byte, bootstrapToken string, wait func(time.Duration) bool) (registrationResponse, error) {
	if client == nil {
		client = newRegistrationHTTPClient()
	}
	if wait == nil {
		wait = func(delay time.Duration) bool {
			time.Sleep(delay)
			return true
		}
	}
	var last error
	for attempt := 0; attempt < maxRegistrationAttempts; attempt++ {
		result, err := postDeviceRegistration(client, registerURL, body, bootstrapToken)
		if err == nil {
			return result, nil
		}
		remote, ok := err.(*connection.RemoteError)
		if !ok || !remote.Retryable() {
			return registrationResponse{}, err
		}
		last = err
		delay := remote.RetryAfter
		if delay <= 0 {
			delay = time.Duration(attempt+1) * 2 * time.Second
		}
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
		if !wait(delay) {
			return registrationResponse{}, last
		}
	}
	if last == nil {
		last = fmt.Errorf("registration retry budget exhausted")
	}
	return registrationResponse{}, last
}

func registrationResponseError(statusCode int, body io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(body, maxRegistrationResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxRegistrationResponseBytes {
		return fmt.Errorf("registration error response is too large")
	}
	if remote, ok := connection.DecodeRemoteError(data, statusCode); ok {
		return remote
	}
	return fmt.Errorf("registration returned status %d", statusCode)
}

func registrationConfig(wsURL string, result registrationResponse, requestedMode string) (*config.Config, string, error) {
	mode, err := registrationTransportMode(result.TransportMode, requestedMode)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(result.DeviceID) == "" || strings.TrimSpace(result.Token) == "" {
		return nil, "", fmt.Errorf("registration response is missing credentials")
	}
	state := strings.TrimSpace(result.ConnectionState)
	if state == "" {
		state = registrationConnectionReady
	}
	if state != registrationConnectionReady && state != registrationConnectionProvisioning {
		return nil, "", fmt.Errorf("unsupported connection_state %q", state)
	}
	if result.RetryAfterSeconds != nil && *result.RetryAfterSeconds < 0 {
		return nil, "", fmt.Errorf("retry_after_seconds must not be negative")
	}
	return &config.Config{
		ServerURL:     wsURL,
		DeviceID:      result.DeviceID,
		Token:         result.Token,
		TransportMode: mode,
	}, state, nil
}
