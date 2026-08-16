package adapters

import "testing"

func TestDefaultCapabilities(t *testing.T) {
	codex := DefaultCapabilities(AgentCodex)
	if codex.ControlMode != ControlExecResume {
		t.Fatalf("codex mode=%s", codex.ControlMode)
	}
	if codex.Approve || codex.Spawn || codex.Steer {
		t.Fatal("codex must not advertise app-server-only controls until live")
	}
	if !codex.Interrupt {
		t.Fatal("codex interrupt")
	}
	if codex.AttachmentMode != AttachNativeImage {
		t.Fatalf("codex attach=%s", codex.AttachmentMode)
	}

	claude := DefaultCapabilities(AgentClaudeCode)
	if claude.ControlMode != ControlCompatibility || claude.Approve {
		t.Fatalf("%#v", claude)
	}
	if claude.AttachmentMode != AttachPathBestEffort {
		t.Fatalf("claude attach=%s", claude.AttachmentMode)
	}

	zcode := DefaultCapabilities(AgentZCode)
	if zcode.ControlMode != ControlCompatibility || zcode.Approve || zcode.Steer {
		t.Fatalf("zcode %#v", zcode)
	}
	if zcode.AttachmentMode != AttachPathBestEffort {
		t.Fatalf("zcode attach=%s", zcode.AttachmentMode)
	}
	cursor := DefaultCapabilities(AgentCursor)
	if cursor.ControlMode != ControlCompatibility || cursor.Approve {
		t.Fatalf("cursor %#v", cursor)
	}
	if cursor.AttachmentMode != AttachPathBestEffort {
		t.Fatalf("cursor attach=%s", cursor.AttachmentMode)
	}
}
