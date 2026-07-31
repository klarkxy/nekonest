package db

import (
	"testing"
)

func TestPhoneIdentityLifecycle(t *testing.T) {
	d := openTestDB(t)
	if v := d.GetSchemaVersion(); v != SchemaVersion {
		t.Fatalf("schema version=%q want %q", v, SchemaVersion)
	}

	devTok, err := d.RegisterDevice("dev1", "PC", "windows")
	if err != nil || devTok == "" {
		t.Fatalf("register device: %v", err)
	}

	phoneID, phoneTok, err := d.CreatePhoneIdentity("Pixel")
	if err != nil || phoneID == "" || phoneTok == "" {
		t.Fatalf("create phone: %v %q %q", err, phoneID, phoneTok)
	}

	auth, err := d.ValidatePhoneToken(phoneTok)
	if err != nil || auth.PhoneID != phoneID || auth.AdminBypass {
		t.Fatalf("validate: %#v err=%v", auth, err)
	}
	if _, err := d.ValidatePhoneToken("wrong"); err != ErrPhoneTokenInvalid {
		t.Fatalf("wrong token: %v", err)
	}

	if err := d.GrantPhoneDevice(phoneID, "dev1"); err != nil {
		t.Fatal(err)
	}
	if !d.PhoneHasDeviceGrant(phoneID, "dev1") {
		t.Fatal("expected grant")
	}
	ids, err := d.ListPhoneDeviceIDs(phoneID)
	if err != nil || len(ids) != 1 || ids[0] != "dev1" {
		t.Fatalf("list grants: %#v err=%v", ids, err)
	}

	// Re-grant is idempotent.
	if err := d.GrantPhoneDevice(phoneID, "dev1"); err != nil {
		t.Fatal(err)
	}

	if err := d.RevokePhoneDeviceGrant(phoneID, "dev1"); err != nil {
		t.Fatal(err)
	}
	if d.PhoneHasDeviceGrant(phoneID, "dev1") {
		t.Fatal("grant should be revoked")
	}

	// Re-grant after revoke.
	if err := d.GrantPhoneDevice(phoneID, "dev1"); err != nil {
		t.Fatal(err)
	}
	if !d.PhoneHasDeviceGrant(phoneID, "dev1") {
		t.Fatal("re-grant failed")
	}

	if err := d.RevokePhone(phoneID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ValidatePhoneToken(phoneTok); err != ErrPhoneRevoked {
		t.Fatalf("revoked token: %v", err)
	}
	if d.PhoneHasDeviceGrant(phoneID, "dev1") {
		t.Fatal("grants cleared on phone revoke")
	}
}

func TestPhoneKeyPackages(t *testing.T) {
	d := openTestDB(t)
	_, _ = d.RegisterDevice("dev1", "PC", "linux")
	phoneID, _, err := d.CreatePhoneIdentity("iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.GrantPhoneDevice(phoneID, "dev1"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertKeyPackage(phoneID, "dev1", "device_catalog", "", 1, "wk", "n"); err != nil {
		t.Fatal(err)
	}
	pkgs, err := d.ListKeyPackages(phoneID, "dev1")
	if err != nil || len(pkgs) != 1 {
		t.Fatalf("%#v err=%v", pkgs, err)
	}
	if pkgs[0]["epoch"].(uint64) != 1 || pkgs[0]["wrapped_key"] != "wk" {
		t.Fatalf("%#v", pkgs[0])
	}
	// Upsert same epoch replaces.
	if err := d.UpsertKeyPackage(phoneID, "dev1", "device_catalog", "", 1, "wk2", "n2"); err != nil {
		t.Fatal(err)
	}
	pkgs, _ = d.ListKeyPackages(phoneID, "dev1")
	if len(pkgs) != 1 || pkgs[0]["wrapped_key"] != "wk2" {
		t.Fatalf("%#v", pkgs)
	}
}

func TestRegisterDeviceOS(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.RegisterDevice("w", "Win", "windows"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RegisterDevice("l", "Linux", "linux"); err != nil {
		t.Fatal(err)
	}
	wd, err := d.GetDevice("w")
	if err != nil || wd.OS != "windows" {
		t.Fatalf("%#v %v", wd, err)
	}
	ld, err := d.GetDevice("l")
	if err != nil || ld.OS != "linux" {
		t.Fatalf("%#v %v", ld, err)
	}
}
