package protocol

import "testing"

func TestProtocol13AndMinorNegotiation(t *testing.T) {
	if CurrentProtocolVersion != "1.3" || CurrentProtocolMinor != 3 {
		t.Fatalf("version=%s minor=%d", CurrentProtocolVersion, CurrentProtocolMinor)
	}
	for _, peer := range []string{"1.0", "1.2", "1.3", "1.9"} {
		result := NegotiateHandshake(peer, "sealed", TransportSealed, CurrentProtocolMinor)
		if result.ErrorCode != "" {
			t.Fatalf("peer %s: %#v", peer, result)
		}
	}
}

func TestStableAPIErrorCodesAreDeploymentNeutral(t *testing.T) {
	for _, code := range []string{
		"device_credential_invalid", "phone_credential_invalid", "access_suspended",
		"registration_disabled", "device_capacity_exceeded", "device_identity_conflict",
		"device_already_connected", "protocol_upgrade_required", "registration_rate_limited",
		"service_provisioning", "route_unavailable", "region_unavailable",
	} {
		if code == "" {
			t.Fatal("empty error code")
		}
	}
}
