package ws

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
)

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
	cm.OnDeviceUp(func(id string) {
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
