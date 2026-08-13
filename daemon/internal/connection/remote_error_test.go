package connection

import (
	"testing"
	"time"
)

func TestDecodeRemoteErrorOnlyRetriesApprovedCodes(t *testing.T) {
	data := []byte(`{"error_code":"service_provisioning","message":"starting","retryable":true,"retry_after_seconds":12}`)
	remote, ok := DecodeRemoteError(data, 503)
	if !ok || !remote.Retryable() || remote.RetryAfter != 12*time.Second || remote.StatusCode != 503 {
		t.Fatalf("remote=%#v ok=%v", remote, ok)
	}
	if remote, ok = DecodeRemoteError([]byte(`{"error_code":"device_capacity_exceeded","retryable":true}`), 409); !ok || remote.Retryable() {
		t.Fatalf("capacity remote=%#v ok=%v", remote, ok)
	}
	if remote, ok = DecodeRemoteError([]byte(`{"error_code":"device_already_connected","retryable":false}`), 409); !ok || !remote.Retryable() || remote.RetryAfter != 15*time.Second {
		t.Fatalf("already-connected remote=%#v ok=%v", remote, ok)
	}
}

func TestDecodeRemoteErrorFailsClosedForUnknownCodes(t *testing.T) {
	remote, ok := DecodeRemoteError([]byte(`{"error_code":"future_retry","retryable":true,"retry_after_seconds":1}`), 503)
	if !ok || remote.Code != "unknown_remote_error" || remote.Retryable() || remote.RetryAfter != 0 {
		t.Fatalf("remote=%#v ok=%v", remote, ok)
	}
}
