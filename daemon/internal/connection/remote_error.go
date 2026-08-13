package connection

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RemoteError is the stable, fail-closed error envelope returned by an HTTP or
// WebSocket NekoNest service endpoint. Unknown error codes are terminal: a
// daemon must never keep retrying an unrecognized authorization failure.
type RemoteError struct {
	Code       string
	Message    string
	StatusCode int
	RetryAfter time.Duration
	ActionURL  string
	retryable  bool
}

func (e *RemoteError) Error() string {
	if e == nil {
		return "remote error"
	}
	if e.Message != "" {
		return fmt.Sprintf("remote %s: %s", e.Code, e.Message)
	}
	return "remote " + e.Code
}

func (e *RemoteError) Retryable() bool { return e != nil && e.retryable }

type remoteErrorEnvelope struct {
	ErrorCode         string `json:"error_code"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds *int64 `json:"retry_after_seconds"`
	ActionURL         string `json:"action_url"`
}

var approvedRetryableCodes = map[string]struct{}{
	"registration_rate_limited": {},
	"service_provisioning":      {},
	"route_unavailable":         {},
	"region_unavailable":        {},
	// Same-credential reconnect while the previous lease is still live.
	// Shared relays reject the second socket; the daemon must keep the
	// original endpoint and wait for the stale generation to expire.
	"device_already_connected": {},
}

var approvedTerminalCodes = map[string]struct{}{
	"device_credential_invalid": {},
	"phone_credential_invalid":  {},
	"access_suspended":          {},
	"registration_disabled":     {},
	"device_capacity_exceeded":  {},
	"device_identity_conflict":  {},
	"protocol_upgrade_required": {},
}

// DecodeRemoteError decodes the uniform error fields from either an HTTP body
// or a WebSocket error payload. It returns false when no structured envelope
// is present, allowing callers to retain a useful generic error.
func DecodeRemoteError(data []byte, statusCode int) (*RemoteError, bool) {
	var envelope remoteErrorEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil || strings.TrimSpace(envelope.ErrorCode) == "" {
		return nil, false
	}
	code := strings.TrimSpace(envelope.ErrorCode)
	_, approvedRetryable := approvedRetryableCodes[code]
	retryable := approvedRetryable && envelope.Retryable
	// A stale live lease must not kill the daemon. Older relays advertised
	// this code as non-retryable; keep retrying the same endpoint anyway.
	if code == "device_already_connected" {
		retryable = true
	}
	if _, knownTerminal := approvedTerminalCodes[code]; !approvedRetryable && !knownTerminal {
		code = "unknown_remote_error"
		retryable = false
	}
	retryAfter := time.Duration(0)
	if retryable && envelope.RetryAfterSeconds != nil && *envelope.RetryAfterSeconds > 0 {
		retryAfter = time.Duration(*envelope.RetryAfterSeconds) * time.Second
	} else if retryable && code == "device_already_connected" {
		retryAfter = 15 * time.Second
	}
	return &RemoteError{
		Code:       code,
		Message:    strings.TrimSpace(envelope.Message),
		StatusCode: statusCode,
		RetryAfter: retryAfter,
		ActionURL:  strings.TrimSpace(envelope.ActionURL),
		retryable:  retryable,
	}, true
}

// IsRetryableRemoteError only accepts an explicitly approved structured code.
func IsRetryableRemoteError(err error) bool {
	var remote *RemoteError
	return AsRemoteError(err, &remote) && remote.Retryable()
}

// AsRemoteError keeps errors.As hidden behind a small package API for callers
// that only need retry classification or retry-after data.
func AsRemoteError(err error, target **RemoteError) bool {
	for err != nil {
		if remote, ok := err.(*RemoteError); ok {
			*target = remote
			return true
		}
		type unwrapper interface{ Unwrap() error }
		next, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = next.Unwrap()
	}
	return false
}
