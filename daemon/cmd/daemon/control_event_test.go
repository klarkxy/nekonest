package main

import (
	"testing"

	"github.com/nekonest/daemon/internal/adapters"
)

func TestControlEventWirePlanSuppressesLifecycleOnlyNoise(t *testing.T) {
	update, attention := controlEventWirePlan(adapters.ControlEvent{
		SessionID: "session", Lifecycle: adapters.TurnAccepted, Class: "accepted",
	})
	if update || attention {
		t.Fatalf("lifecycle-only event planned update=%v attention=%v", update, attention)
	}
	update, attention = controlEventWirePlan(adapters.ControlEvent{
		SessionID: "session", Status: adapters.StatusWaitingApproval,
	})
	if !update || attention {
		t.Fatalf("status event planned update=%v attention=%v", update, attention)
	}
	update, attention = controlEventWirePlan(adapters.ControlEvent{
		SessionID: "session", EventID: "native-request", Class: "approval_required",
	})
	if update || !attention {
		t.Fatalf("attention event planned update=%v attention=%v", update, attention)
	}
	update, attention = controlEventWirePlan(adapters.ControlEvent{
		SessionID: "session", ClearActiveTurn: true,
	})
	if !update || attention {
		t.Fatalf("active-turn clear planned update=%v attention=%v", update, attention)
	}
}

func TestDaemonInboundApplicationTypesIncludeQueueSkip(t *testing.T) {
	if !daemonInboundApplicationType("skip_prompt_queue_item") {
		t.Fatal("skip_prompt_queue_item is not decoded as an application command")
	}
}

func TestRefreshSessionsRemainsRoutingOnly(t *testing.T) {
	if daemonInboundApplicationType("refresh_sessions") {
		t.Fatal("refresh_sessions must remain a routing-only frame in sealed mode")
	}
}
