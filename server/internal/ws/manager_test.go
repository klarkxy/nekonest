package ws

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
)

func TestThreadStartRelayFailureIsFailClosedAfterWriteAttempt(t *testing.T) {
	if typ, _ := threadStartRelayFailure(ErrDeviceOffline); typ != protocol.MsgThreadFailed {
		t.Fatalf("offline result = %s", typ)
	}
	if typ, message := threadStartRelayFailure(errors.New("write: broken pipe")); typ != protocol.MsgThreadIndeterminate || message == "" {
		t.Fatalf("write failure result = %s, %q", typ, message)
	}
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.New(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestConnectionManagerDaemonIdentity(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)

	var up, down int
	cm.OnDeviceUp(func(id, _ string) {
		if id == "dev1" {
			up++
		}
	})
	cm.OnDeviceDown(func(id string) {
		if id == "dev1" {
			down++
		}
	})

	var c1 *websocket.Conn
	c2 := &websocket.Conn{}

	if cm.IsDaemonOnline("dev1") {
		t.Fatal("offline")
	}
	if err := cm.SendToDaemon("dev1", protocol.NewMessage(protocol.MsgHeartbeat, "dev1")); err != ErrDeviceOffline {
		t.Fatalf("offline send: %v", err)
	}

	dc1 := cm.AddDaemon("dev1", c1)
	if up != 1 {
		t.Fatalf("onDeviceUp=%d", up)
	}
	if !cm.IsDaemonOnline("dev1") || !cm.IsLiveDaemon(dc1) {
		t.Fatal("online")
	}
	ids := cm.GetOnlineDevices()
	if len(ids) != 1 || ids[0] != "dev1" {
		t.Fatalf("%v", ids)
	}

	// Stale remove must not wipe current
	cm.RemoveDaemon("dev1", c2)
	if !cm.IsLiveDaemon(dc1) {
		t.Fatal("stale remove wiped live")
	}

	// Stale UpdateSessionsFrom
	stale := &DaemonConn{DeviceID: "dev1", Conn: c2, Sessions: map[string]*protocol.AgentSession{}}
	cm.UpdateSessionsFrom(stale, []*protocol.AgentSession{{ID: "s-bad"}})
	if len(cm.GetDeviceSessions("dev1")) != 0 {
		t.Fatal("stale session list applied")
	}

	cm.UpdateSessionsFrom(dc1, []*protocol.AgentSession{
		nil,
		{ID: "", Summary: "empty id"},
		{ID: "s1", Summary: "ok", DeviceID: ""},
	})
	sess := cm.GetDeviceSessions("dev1")
	if len(sess) != 1 || sess[0].ID != "s1" {
		t.Fatalf("%#v", sess)
	}

	// Identity-aware remove: wrong conn pointer is a no-op (already covered above).
	// Full replace path closes the old websocket — covered by integration, not empty Conn stubs.

	cm.RemoveDaemon("dev1", c1)
	if down != 1 || cm.IsDaemonOnline("dev1") {
		t.Fatalf("down=%d online=%v", down, cm.IsDaemonOnline("dev1"))
	}
	_ = c2
}

func TestInitialDaemonVersionNotificationPrecedesReplacement(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)

	initialEntered := make(chan struct{})
	releaseInitial := make(chan struct{})
	var mu sync.Mutex
	var versions []string
	recordVersion := func(_ string, version string) {
		if version == "0.2.0" {
			close(initialEntered)
			<-releaseInitial
		}
		mu.Lock()
		versions = append(versions, version)
		mu.Unlock()
	}
	cm.OnDeviceUp(recordVersion)
	cm.OnDeviceVersionChange(recordVersion)

	initialDone := make(chan struct{})
	go func() {
		cm.AddDaemonVersioned("dev1", nil, "0.2.0")
		close(initialDone)
	}()
	select {
	case <-initialEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("initial online callback did not start")
	}

	replacementDone := make(chan struct{})
	go func() {
		cm.AddDaemonVersioned("dev1", nil, "0.2.1")
		close(replacementDone)
	}()
	select {
	case <-replacementDone:
		t.Fatal("replacement bypassed the initial generation notification")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseInitial)
	select {
	case <-initialDone:
	case <-time.After(2 * time.Second):
		t.Fatal("initial connection did not finish")
	}
	select {
	case <-replacementDone:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(versions) != 2 || versions[0] != "0.2.0" || versions[1] != "0.2.1" {
		t.Fatalf("version notification order = %v", versions)
	}
}

func TestSameVersionReplacementCannotSkipInitialNotification(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)

	initialPublished := make(chan struct{})
	releaseInitial := make(chan struct{})
	cm.afterDaemonPublished = func(dc *DaemonConn) {
		if dc.AppVersion == "0.2.1" {
			close(initialPublished)
			<-releaseInitial
		}
	}

	var mu sync.Mutex
	var versions []string
	cm.OnDeviceUp(func(_ string, version string) {
		mu.Lock()
		versions = append(versions, version)
		mu.Unlock()
	})
	cm.OnDeviceVersionChange(func(_ string, version string) {
		mu.Lock()
		versions = append(versions, version)
		mu.Unlock()
	})

	initialDone := make(chan struct{})
	go func() {
		cm.AddDaemonVersioned("dev1", nil, "0.2.1")
		close(initialDone)
	}()
	select {
	case <-initialPublished:
	case <-time.After(2 * time.Second):
		t.Fatal("initial generation was not published")
	}

	replacementDone := make(chan struct{})
	go func() {
		cm.AddDaemonVersioned("dev1", nil, "0.2.1")
		close(replacementDone)
	}()
	select {
	case <-replacementDone:
		t.Fatal("same-version replacement bypassed the unpublished online notification")
	case <-time.After(100 * time.Millisecond):
	}

	cm.afterDaemonPublished = nil
	close(releaseInitial)
	select {
	case <-initialDone:
	case <-time.After(2 * time.Second):
		t.Fatal("initial connection did not finish")
	}
	select {
	case <-replacementDone:
	case <-time.After(2 * time.Second):
		t.Fatal("same-version replacement did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(versions) != 1 || versions[0] != "0.2.1" {
		t.Fatalf("version notifications = %v, want one initial online event", versions)
	}
}

func TestDaemonVersionReplacementNotificationsStayGenerationOrdered(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)
	cm.AddDaemonVersioned("dev1", nil, "0.2.0")

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	var versions []string
	cm.OnDeviceVersionChange(func(_ string, version string) {
		if version == "0.2.1-a" {
			close(firstEntered)
			<-releaseFirst
		}
		mu.Lock()
		versions = append(versions, version)
		mu.Unlock()
	})

	firstDone := make(chan struct{})
	go func() {
		cm.AddDaemonVersioned("dev1", nil, "0.2.1-a")
		close(firstDone)
	}()
	select {
	case <-firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first replacement callback did not start")
	}

	secondDone := make(chan struct{})
	go func() {
		cm.AddDaemonVersioned("dev1", nil, "0.2.1-b")
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Fatal("newer replacement bypassed the current generation notification")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first replacement did not finish")
	}
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second replacement did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(versions) != 2 || versions[0] != "0.2.1-a" || versions[1] != "0.2.1-b" {
		t.Fatalf("version notification order: %#v", versions)
	}
}

func TestConnectionManagerPhoneSubscribe(t *testing.T) {
	d := testDB(t)
	cm := NewConnectionManager(d)
	p1 := &websocket.Conn{}
	p2 := &websocket.Conn{}

	cm.AddPhone("devA", p1)
	cm.AddPhone("devA", p2)
	cm.ResubscribePhone("devB", p1)
	if len(cm.phoneConns["devA"]) != 1 || len(cm.phoneConns["devB"]) != 1 {
		t.Fatalf("unexpected subscriptions: %#v", cm.phoneConns)
	}

	// p1 only on B; p2 still on A
	cm.RemovePhone("devA", p1) // no-op list-wise if already moved
	cm.RemovePhone("devB", p1)
	cm.RemovePhone("devA", p2)
	if len(cm.phoneConns) != 0 {
		t.Fatalf("empty subscription keys leaked: %#v", cm.phoneConns)
	}

	// SafeWrite without registration is no-op
	cm.SafeWritePhone(p1, protocol.NewMessage(protocol.MsgHeartbeat, "x"))
	cm.BroadcastToPhones("nobody", protocol.NewMessage(protocol.MsgHeartbeat, "nobody"))
}

func TestUpdateSessionsConvenience(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("d", "n")
	cm := NewConnectionManager(d)
	c := &websocket.Conn{}
	cm.AddDaemon("d", c)
	cm.UpdateSessions("d", []*protocol.AgentSession{{ID: "a"}})
	if len(cm.GetDeviceSessions("d")) != 1 {
		t.Fatal("update")
	}
	cm.UpdateSessions("missing", []*protocol.AgentSession{{ID: "x"}})
	cm.UpdateSessionsFrom(nil, nil)
}

func TestAgentStartCapabilitiesStayWithLiveDaemonGeneration(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)
	first := cm.AddDaemonVersioned("dev1", nil, "0.2.3")

	cm.UpdateSessionListFrom(first, []*protocol.AgentSession{{ID: "session-1"}}, []protocol.AgentStartCapability{
		{AgentType: protocol.AgentCodex, Available: true},
		{AgentType: protocol.AgentKimiCLI, Available: false, Reason: "native start not verified"},
		{AgentType: protocol.AgentKilo, Available: true},
		{AgentType: "unknown", Available: true},
		{AgentType: protocol.AgentCodex, Available: false, Reason: "duplicate ignored"},
	})
	sessions, capabilities := cm.GetDeviceSessionSnapshot("dev1")
	if len(sessions) != 1 || sessions[0].ID != "session-1" {
		t.Fatalf("sessions=%#v", sessions)
	}
	if len(capabilities) != 2 || !capabilities[0].Available || capabilities[1].Reason != "native start not verified" {
		t.Fatalf("capabilities=%#v", capabilities)
	}

	second := cm.AddDaemonVersioned("dev1", nil, "0.2.3")
	cm.UpdateSessionListFrom(first, []*protocol.AgentSession{{ID: "stale-session"}}, []protocol.AgentStartCapability{
		{AgentType: protocol.AgentGrokBuild, Available: true},
	})
	sessions, capabilities = cm.GetDeviceSessionSnapshot("dev1")
	if len(sessions) != 0 || len(capabilities) != 0 {
		t.Fatalf("stale generation updated snapshot: sessions=%#v capabilities=%#v", sessions, capabilities)
	}

	cm.UpdateSessionListFrom(second, []*protocol.AgentSession{{ID: "session-2"}}, []protocol.AgentStartCapability{
		{AgentType: protocol.AgentGrokBuild, Available: true},
	})
	sessions, capabilities = cm.GetDeviceSessionSnapshot("dev1")
	if len(sessions) != 1 || sessions[0].ID != "session-2" || len(capabilities) != 1 || capabilities[0].AgentType != protocol.AgentGrokBuild {
		t.Fatalf("live generation snapshot: sessions=%#v capabilities=%#v", sessions, capabilities)
	}
}

func TestRetiredKiloSessionsAreNotCachedOrBroadcast(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)
	dc := cm.AddDaemonVersioned("dev1", nil, "0.2.3")

	cm.UpdateSessionListFrom(dc, []*protocol.AgentSession{
		{ID: "legacy-kilo", AgentType: protocol.AgentKilo},
		{ID: "active-codex", AgentType: protocol.AgentCodex},
	}, []protocol.AgentStartCapability{
		{AgentType: protocol.AgentKilo, Available: true, Spawn: true},
		{AgentType: protocol.AgentCodex, Available: true, Spawn: true},
	})

	sessions, capabilities := cm.GetDeviceSessionSnapshot("dev1")
	if len(sessions) != 1 || sessions[0].ID != "active-codex" {
		t.Fatalf("retired session leaked into catalog: %#v", sessions)
	}
	if len(capabilities) != 1 || capabilities[0].AgentType != protocol.AgentCodex {
		t.Fatalf("retired start capability leaked into catalog: %#v", capabilities)
	}
}

func TestAgentStartCapabilitiesPreserveExplicitEmptyCatalog(t *testing.T) {
	d := testDB(t)
	_, _ = d.RegisterDevice("dev1", "PC")
	cm := NewConnectionManager(d)
	dc := cm.AddDaemonVersioned("dev1", nil, "0.2.3")

	cm.UpdateSessionListFrom(dc, nil, []protocol.AgentStartCapability{})
	_, capabilities := cm.GetDeviceSessionSnapshot("dev1")
	if capabilities == nil || len(capabilities) != 0 {
		t.Fatalf("explicit empty catalog collapsed to legacy absence: %#v", capabilities)
	}
	if filtered := cleanAgentStartCapabilities([]protocol.AgentStartCapability{{
		AgentType: "unknown", Available: true, Spawn: true,
	}}); filtered == nil || len(filtered) != 0 {
		t.Fatalf("present catalog with only unknown agents did not fail closed: %#v", filtered)
	}

	cm.UpdateSessionListFrom(dc, nil, nil)
	_, capabilities = cm.GetDeviceSessionSnapshot("dev1")
	if capabilities != nil {
		t.Fatalf("legacy absent catalog became explicit: %#v", capabilities)
	}
}

func TestPhoneOutboundQueueIsBounded(t *testing.T) {
	out := newPhoneOutbound()
	for i := 0; i < cap(out.queue); i++ {
		if !enqueuePhone(out, []byte("x")) {
			t.Fatalf("enqueue %d rejected", i)
		}
	}
	if enqueuePhone(out, []byte("overflow")) {
		t.Fatal("full phone queue accepted another frame")
	}
	out.close()
	if usage := out.byteUsage(); usage != 0 {
		t.Fatalf("closed queue retained %d reserved bytes", usage)
	}
	if enqueuePhone(out, []byte("closed")) {
		t.Fatal("closed phone queue accepted a frame")
	}
}

func TestPhoneOutboundByteBudgetIncludesInFlightFrames(t *testing.T) {
	out := newPhoneOutbound()
	halfBudget := make([]byte, maxPhoneOutboundBytes/2)
	if !enqueuePhone(out, halfBudget) || !enqueuePhone(out, halfBudget) {
		t.Fatal("frames totaling the exact byte budget were rejected")
	}
	if usage := out.byteUsage(); usage != maxPhoneOutboundBytes {
		t.Fatalf("reserved bytes=%d, want %d", usage, maxPhoneOutboundBytes)
	}

	// Dequeueing does not release the reservation: the frame remains retained
	// and potentially blocked inside WriteMessage until that call returns.
	inFlight := <-out.queue
	if usage := out.byteUsage(); usage != maxPhoneOutboundBytes {
		t.Fatalf("in-flight frame was released early: usage=%d", usage)
	}
	if enqueuePhone(out, []byte{1}) {
		t.Fatal("byte budget ignored an in-flight frame")
	}
	out.releaseBytes(int64(len(inFlight)))
	if usage := out.byteUsage(); usage != maxPhoneOutboundBytes/2 {
		t.Fatalf("completed frame did not release budget: usage=%d", usage)
	}
	if !enqueuePhone(out, halfBudget) {
		t.Fatal("released byte budget could not be reused")
	}

	before := out.byteUsage()
	if enqueuePhone(out, make([]byte, maxPhoneOutboundBytes+1)) {
		t.Fatal("single frame larger than byte budget was accepted")
	}
	if usage := out.byteUsage(); usage != before {
		t.Fatalf("rejected frame changed reservation: before=%d after=%d", before, usage)
	}

	out.close()
	if usage := out.byteUsage(); usage != 0 {
		t.Fatalf("close leaked queued byte reservations: %d", usage)
	}
}

func TestPhoneOutboundConcurrentCloseAndEnqueue(t *testing.T) {
	const (
		iterations = 40
		producers  = 12
		attempts   = 200
	)
	payload := make([]byte, 64<<10)
	for iteration := 0; iteration < iterations; iteration++ {
		out := newPhoneOutbound()
		start := make(chan struct{})
		var wg sync.WaitGroup
		for producer := 0; producer < producers; producer++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for attempt := 0; attempt < attempts; attempt++ {
					_ = enqueuePhone(out, payload)
				}
			}()
		}
		// Multiple concurrent closes also exercise idempotence of closed/done.
		for closer := 0; closer < 3; closer++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				out.close()
			}()
		}
		close(start)
		wg.Wait()
		out.close()

		if !out.isClosed() {
			t.Fatalf("iteration %d: outbound remained open", iteration)
		}
		if usage := out.byteUsage(); usage != 0 {
			t.Fatalf("iteration %d: leaked or negative-normalized reservation %d", iteration, usage)
		}
		if queued := len(out.queue); queued != 0 {
			t.Fatalf("iteration %d: closed outbound retained %d frames", iteration, queued)
		}
		for attempt := 0; attempt < 100; attempt++ {
			if enqueuePhone(out, payload) {
				t.Fatalf("iteration %d: enqueue succeeded after close", iteration)
			}
		}
	}
}

func TestDaemonReplacementDoesNotBlockOtherDevices(t *testing.T) {
	d := testDB(t)
	cm := NewConnectionManager(d)
	first := cm.AddDaemon("dev-a", nil)

	first.mu.Lock()
	replaceDone := make(chan struct{})
	go func() {
		cm.AddDaemon("dev-a", nil)
		close(replaceDone)
	}()
	// Give replacement time to wait on dev-a's per-connection lock.
	time.Sleep(20 * time.Millisecond)

	otherDone := make(chan struct{})
	go func() {
		cm.AddDaemon("dev-b", nil)
		close(otherDone)
	}()
	select {
	case <-otherDone:
	case <-time.After(500 * time.Millisecond):
		first.mu.Unlock()
		t.Fatal("dev-a replacement blocked unrelated dev-b")
	}
	first.mu.Unlock()
	select {
	case <-replaceDone:
	case <-time.After(time.Second):
		t.Fatal("dev-a replacement did not finish")
	}

	cm.RemoveDaemon("dev-a", nil)
	cm.RemoveDaemon("dev-b", nil)
}
