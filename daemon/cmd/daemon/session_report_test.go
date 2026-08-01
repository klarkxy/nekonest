package main

import (
	"testing"

	"github.com/nekonest/daemon/internal/adapters"
)

func TestSessionCapabilitiesChanged(t *testing.T) {
	base := &adapters.SessionInfo{Capabilities: &adapters.SessionCapabilities{
		ControlMode:    adapters.ControlExecResume,
		Interrupt:      true,
		AttachmentMode: adapters.AttachPathBestEffort,
	}}
	same := &adapters.SessionInfo{Capabilities: &adapters.SessionCapabilities{
		ControlMode:    adapters.ControlExecResume,
		Interrupt:      true,
		AttachmentMode: adapters.AttachPathBestEffort,
	}}
	if sessionCapabilitiesChanged(base, same) {
		t.Fatal("identical capabilities reported as changed")
	}
	fullControl := &adapters.SessionInfo{Capabilities: &adapters.SessionCapabilities{
		ControlMode:    adapters.ControlAppServer,
		Approve:        true,
		Deny:           true,
		Interrupt:      true,
		Steer:          true,
		Spawn:          true,
		AttachmentMode: adapters.AttachNativeImageAndFile,
	}}
	if !sessionCapabilitiesChanged(base, fullControl) {
		t.Fatal("app-server capability promotion was not detected")
	}
}
