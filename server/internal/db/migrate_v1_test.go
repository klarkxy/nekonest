package db

import (
	"path/filepath"
	"testing"

	"github.com/nekonest/server/internal/protocol"
)

func TestClearPlaintextContentForV1PreservesDevices(t *testing.T) {
	d := openTestDB(t)
	tok, err := d.RegisterDevice("dev1", "PC", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !d.ValidateDeviceToken("dev1", tok) {
		t.Fatal("token")
	}
	phoneID, phoneTok, err := d.CreatePhoneIdentity("p")
	if err != nil {
		t.Fatal(err)
	}
	_ = d.GrantPhoneDevice(phoneID, "dev1")
	_ = d.SaveMessage("dev1", "s1", &protocol.SessionMessage{
		ID: "m1", Role: "user", Content: "secret", Timestamp: 1,
	})

	if err := d.ClearPlaintextContentForV1(); err != nil {
		t.Fatal(err)
	}
	if !d.ValidateDeviceToken("dev1", tok) {
		t.Fatal("device token must survive migration")
	}
	if _, err := d.ValidatePhoneToken(phoneTok); err == nil {
		t.Fatal("phone identities must be cleared")
	}
	msgs, err := d.GetMessages("dev1", "s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages remain: %d", len(msgs))
	}
	// Schema version still set
	if d.GetSchemaVersion() == "" {
		t.Fatal("schema version")
	}
	_ = filepath.Separator
}
