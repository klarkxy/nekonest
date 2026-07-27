package push

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateEndpoint(t *testing.T) {
	ok := []string{
		"https://fcm.googleapis.com/fcm/send/abc",
		"https://updates.push.services.mozilla.com/wpush/v2/xxx",
		"https://wns2-pn1p.notify.windows.com/w/?token=1",
	}
	for _, u := range ok {
		if err := ValidateEndpoint(u); err != nil {
			t.Errorf("want ok %s: %v", u, err)
		}
	}
	bad := []string{
		"",
		"http://fcm.googleapis.com/x", // not https
		"https://localhost/push",
		"https://127.0.0.1/push",
		"https://10.0.0.1/push",
		"https://192.168.1.1/push",
		"https://[::1]/push",
		"https://metadata.google.internal/",
		"https://foo.local/bar",
		"ftp://example.com/x",
		"not-a-url",
	}
	for _, u := range bad {
		if err := ValidateEndpoint(u); err == nil {
			t.Errorf("want reject %s", u)
		}
	}
}

func TestValidateKeys(t *testing.T) {
	publicKey := make([]byte, 65)
	publicKey[0] = 0x04
	auth := make([]byte, 16)
	validPublic := base64.RawURLEncoding.EncodeToString(publicKey)
	validAuth := base64.RawURLEncoding.EncodeToString(auth)
	if err := ValidateKeys(validPublic, validAuth); err != nil {
		t.Fatalf("valid keys: %v", err)
	}
	if err := ValidateKeys(
		base64.URLEncoding.EncodeToString(publicKey),
		base64.URLEncoding.EncodeToString(auth),
	); err != nil {
		t.Fatalf("valid padded keys: %v", err)
	}
	for _, test := range [][2]string{
		{"not-base64!", validAuth},
		{base64.RawURLEncoding.EncodeToString(make([]byte, 65)), validAuth},
		{base64.RawURLEncoding.EncodeToString(make([]byte, 64)), validAuth},
		{base64.RawURLEncoding.EncodeToString(make([]byte, 66)), validAuth},
		{validPublic, base64.RawURLEncoding.EncodeToString(make([]byte, 15))},
		{validPublic, base64.RawURLEncoding.EncodeToString(make([]byte, 17))},
	} {
		if err := ValidateKeys(test[0], test[1]); err == nil {
			t.Fatalf("accepted invalid keys: %#v", test)
		}
	}
}

func TestEnabledAndPublicKey(t *testing.T) {
	// reset sync.Once is hard; test via env before first load in this package process.
	// These may already be loaded — just exercise the API.
	_ = Enabled()
	_ = PublicKey()
}

func TestSendNoopEmptyAndInvalid(t *testing.T) {
	// Must not panic; async path with no VAPID / invalid endpoints.
	Send(nil, "t", "b", "/", "device", "session", nil)
	Send([]Subscription{{Endpoint: "http://evil", P256DH: "x", Auth: "y"}}, "t", "b", "/", "device", "session", nil)
	Send([]Subscription{{Endpoint: "https://fcm.googleapis.com/fcm/send/x", P256DH: "x", Auth: "y"}}, "t", "b", "/", "device", "session", nil)
}

func TestBoundedNotificationQueue(t *testing.T) {
	queue := make(chan notificationJob, 1)
	if !enqueueNotification(queue, notificationJob{}) {
		t.Fatal("first enqueue rejected")
	}
	if enqueueNotification(queue, notificationJob{}) {
		t.Fatal("full queue accepted another job")
	}
}

func TestGoneStatus(t *testing.T) {
	if !isGoneStatus(404) || !isGoneStatus(410) {
		t.Fatal("permanent push status not detected")
	}
	if isGoneStatus(400) || isGoneStatus(500) {
		t.Fatal("transient status treated as gone")
	}
	var deleted string
	job := notificationJob{onGone: func(endpoint string) { deleted = endpoint }}
	handleDeliveryStatus(job, "https://push.example/gone", 410)
	if deleted != "https://push.example/gone" {
		t.Fatalf("gone callback endpoint=%q", deleted)
	}
	deleted = ""
	handleDeliveryStatus(job, "https://push.example/retry", 500)
	if deleted != "" {
		t.Fatalf("transient failure deleted endpoint=%q", deleted)
	}
}

func TestMarshalNotificationIncludesRoutingAndStableTag(t *testing.T) {
	first, err := marshalNotification("Approval", "Open", "/device/d/session/s", "d", "s")
	if err != nil {
		t.Fatal(err)
	}
	second, err := marshalNotification("Approval", "Open", "/device/d/session/s", "d", "s")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("payload is not stable:\n%s\n%s", first, second)
	}
	var payload notificationPayload
	if err := json.Unmarshal(first, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DeviceID != "d" || payload.SessionID != "s" ||
		payload.Tag != "nekonest:d:s" || payload.URL != "/device/d/session/s" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestPushErrorString(t *testing.T) {
	if !strings.Contains(errInvalidEndpoint.Error(), "invalid") {
		t.Fatal(errInvalidEndpoint)
	}
}
