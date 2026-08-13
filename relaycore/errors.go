package relaycore

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/klarkxy/nekonest/relaycore/protocol"
)

var (
	ErrDeviceOffline          = errors.New("device is offline")
	ErrInvalidToken           = errors.New("invalid device token")
	ErrDeviceAlreadyConnected = errors.New("device identity is already connected")
)

const (
	ErrCodeDeviceCredentialInvalid = "device_credential_invalid"
	ErrCodePhoneCredentialInvalid  = "phone_credential_invalid"
	ErrCodeAccessSuspended         = "access_suspended"
	ErrCodeRegistrationDisabled    = "registration_disabled"
	ErrCodeDeviceCapacityExceeded  = "device_capacity_exceeded"
	ErrCodeDeviceIdentityConflict  = "device_identity_conflict"
	ErrCodeDeviceAlreadyConnected  = "device_already_connected"
	ErrCodeProtocolUpgradeRequired = "protocol_upgrade_required"
	ErrCodeRegistrationRateLimited = "registration_rate_limited"
	ErrCodeServiceProvisioning     = "service_provisioning"
	ErrCodeRouteUnavailable        = "route_unavailable"
	ErrCodeRegionUnavailable       = "region_unavailable"
)

// APIError is the stable deployment-neutral error envelope shared by HTTP and
// WebSocket paths. HTTPStatus is transport metadata and is not serialized.
type APIError struct {
	protocol.ServiceErrorPayload
	HTTPStatus int `json:"-"`
}

func (e APIError) Error() string { return e.Message }

func WriteAPIError(w http.ResponseWriter, apiErr APIError) {
	status := apiErr.HTTPStatus
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErr)
}

func (s *Engine) writeWSAPIError(conn interface{ WriteJSON(any) error }, apiErr APIError) {
	payload := map[string]any{
		"error_code": apiErr.ErrorCode,
		"message":    apiErr.Message,
		"retryable":  apiErr.Retryable,
	}
	if apiErr.RetryAfterSeconds > 0 {
		payload["retry_after_seconds"] = apiErr.RetryAfterSeconds
	}
	if apiErr.ActionURL != "" {
		payload["action_url"] = apiErr.ActionURL
	}
	_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
		Type:      protocol.MsgError,
		Timestamp: s.clock.Now().Unix(),
		Payload:   payload,
	}))
}

func stableError(code protocol.ServiceErrorCode, message string, retryable bool, status int) APIError {
	return APIError{
		ServiceErrorPayload: protocol.ServiceErrorPayload{
			ErrorCode: code,
			Message:   message,
			Retryable: retryable,
		},
		HTTPStatus: status,
	}
}
