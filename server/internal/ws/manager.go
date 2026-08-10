package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/opslog"
	"github.com/nekonest/server/internal/protocol"
)

// ConnectionManager handles all WebSocket connections.
type ConnectionManager struct {
	mu                    sync.RWMutex
	daemonConns           map[string]*DaemonConn // device_id -> daemon connection
	nextDaemonGeneration  uint64
	phoneConns            map[string][]*websocket.Conn // device_id -> phone connections (subscribed to that device)
	phoneOutbounds        map[*websocket.Conn]*phoneOutbound
	database              *db.DB
	onDeviceUp            func(deviceID, appVersion string)
	onDeviceDown          func(deviceID string)
	onDeviceVersionChange func(deviceID, appVersion string)
	afterDaemonPublished  func(*DaemonConn) // test synchronization hook; nil in production
}

// DaemonConn wraps a daemon's WebSocket connection with metadata.
type DaemonConn struct {
	Conn                   *websocket.Conn
	DeviceID               string
	AppVersion             string
	LastPing               time.Time
	Sessions               map[string]*protocol.AgentSession
	AgentStartCapabilities []protocol.AgentStartCapability
	CatalogProtocolVersion string
	ProtocolVersion        string
	SealedCatalog          *protocol.NekoMessage
	generation             uint64
	closed                 bool
	mu                     sync.RWMutex
}

type phoneOutbound struct {
	writeMu       sync.Mutex
	stateMu       sync.Mutex
	queue         chan []byte
	done          chan struct{}
	closed        bool
	reservedBytes int64
}

const (
	phoneOutboundQueueSize = 64
	maxPhoneOutboundBytes  = int64(8 << 20)
)

func newPhoneOutbound() *phoneOutbound {
	return &phoneOutbound{
		queue: make(chan []byte, phoneOutboundQueueSize),
		done:  make(chan struct{}),
	}
}

func (out *phoneOutbound) close() {
	if out == nil {
		return
	}
	out.stateMu.Lock()
	if out.closed {
		out.stateMu.Unlock()
		return
	}
	// closed and enqueue reservation share stateMu. Once closed becomes true,
	// no producer can reserve or send another queue item.
	out.closed = true
	close(out.done)
	// Release every frame that will never be written. A frame already received
	// by phoneWriter remains reserved until its WriteMessage call returns.
	for {
		select {
		case data := <-out.queue:
			out.releaseBytesLocked(int64(len(data)))
		default:
			out.stateMu.Unlock()
			return
		}
	}
}

func (out *phoneOutbound) releaseBytes(frameBytes int64) {
	if out == nil || frameBytes <= 0 {
		return
	}
	out.stateMu.Lock()
	out.releaseBytesLocked(frameBytes)
	out.stateMu.Unlock()
}

func (out *phoneOutbound) releaseBytesLocked(frameBytes int64) {
	if frameBytes <= 0 {
		return
	}
	if frameBytes >= out.reservedBytes {
		out.reservedBytes = 0
		return
	}
	out.reservedBytes -= frameBytes
}

func (out *phoneOutbound) byteUsage() int64 {
	if out == nil {
		return 0
	}
	out.stateMu.Lock()
	defer out.stateMu.Unlock()
	return out.reservedBytes
}

func (out *phoneOutbound) isClosed() bool {
	if out == nil {
		return true
	}
	out.stateMu.Lock()
	defer out.stateMu.Unlock()
	return out.closed
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(database *db.DB) *ConnectionManager {
	return &ConnectionManager{
		daemonConns:    make(map[string]*DaemonConn),
		phoneConns:     make(map[string][]*websocket.Conn),
		phoneOutbounds: make(map[*websocket.Conn]*phoneOutbound),
		database:       database,
	}
}

// OnDeviceUp sets the callback for when a device comes online.
func (cm *ConnectionManager) OnDeviceUp(fn func(string, string)) {
	cm.onDeviceUp = fn
}

// OnDeviceDown sets the callback for when a device goes offline.
func (cm *ConnectionManager) OnDeviceDown(fn func(string)) {
	cm.onDeviceDown = fn
}

// OnDeviceVersionChange sets the callback for when a live daemon connection
// is replaced by one reporting a different application release.
func (cm *ConnectionManager) OnDeviceVersionChange(fn func(string, string)) {
	cm.onDeviceVersionChange = fn
}

// AddDaemon registers a daemon WebSocket connection.
// If a previous connection exists for the same device, it is closed and replaced.
// onDeviceUp only fires when the device was previously offline.
func (cm *ConnectionManager) AddDaemon(deviceID string, conn *websocket.Conn) *DaemonConn {
	return cm.AddDaemonVersioned(deviceID, conn, "")
}

// AddDaemonVersioned registers a daemon and keeps its reported application
// release with the live connection. It is intentionally not persisted: an
// offline daemon cannot prove which binary will start next.
func (cm *ConnectionManager) AddDaemonVersioned(deviceID string, conn *websocket.Conn, appVersion string) *DaemonConn {
	dc := &DaemonConn{
		Conn:       conn,
		DeviceID:   deviceID,
		AppVersion: appVersion,
		LastPing:   time.Now(),
		Sessions:   make(map[string]*protocol.AgentSession),
	}
	// Establish the new generation's lifecycle lease before publishing it.
	// Otherwise a same-version replacement could supersede this connection
	// before its initial online notification, causing both paths to stay silent.
	dc.mu.Lock()
	defer dc.mu.Unlock()
	wasOnline := false
	previousVersion := ""
	for {
		// Observe without holding the global map lock while waiting for a slow
		// write on the old device connection.
		cm.mu.RLock()
		old := cm.daemonConns[deviceID]
		cm.mu.RUnlock()
		if old != nil {
			old.mu.Lock()
		}

		cm.mu.Lock()
		if cm.daemonConns[deviceID] != old {
			cm.mu.Unlock()
			if old != nil {
				old.mu.Unlock()
			}
			continue
		}
		wasOnline = old != nil && !old.closed
		if old != nil {
			previousVersion = old.AppVersion
		}
		cm.nextDaemonGeneration++
		dc.generation = cm.nextDaemonGeneration
		cm.daemonConns[deviceID] = dc
		if old != nil {
			old.closed = true
		}
		cm.mu.Unlock()

		if old != nil {
			opslog.Info("server.ws", "daemon_connection_replaced", "daemon connection replaced", "device_id", deviceID)
			if old.Conn != nil {
				_ = old.Conn.Close()
			}
			old.mu.Unlock()
		}
		break
	}
	if cm.afterDaemonPublished != nil {
		cm.afterDaemonPublished(dc)
	}

	opslog.Info("server.ws", "daemon_connected", "daemon connected", "device_id", deviceID)
	cm.database.UpdateDeviceLastSeen(deviceID)
	if !wasOnline && cm.onDeviceUp != nil {
		// Hold the initial generation's lifecycle lease through notification
		// enqueue. A concurrent replacement must wait on dc.mu, so the first
		// online event cannot be published after a newer version event.
		if cm.isLiveDaemonLocked(dc) {
			cm.onDeviceUp(deviceID, appVersion)
		}
	} else if wasOnline && previousVersion != appVersion && cm.onDeviceVersionChange != nil {
		// Hold this generation's lifecycle lease through notification enqueue.
		// A subsequent replacement must wait on dc.mu, so an older version event
		// cannot be published after the newer generation's event.
		if cm.isLiveDaemonLocked(dc) {
			cm.onDeviceVersionChange(deviceID, appVersion)
		}
	}
	return dc
}

// GetDaemonVersion returns the application release reported by the current
// live daemon connection. Empty means offline or an older unreporting daemon.
func (cm *ConnectionManager) GetDaemonVersion(deviceID string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	dc := cm.daemonConns[deviceID]
	if dc == nil {
		return ""
	}
	return dc.AppVersion
}

// RemoveDaemon removes a daemon connection only if conn is still the registered one.
// This prevents a stale reconnect teardown from deleting a newer connection.
func (cm *ConnectionManager) RemoveDaemon(deviceID string, conn *websocket.Conn) {
	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	cm.mu.RUnlock()
	if !ok || dc.Conn != conn {
		return
	}

	// Linearize close against in-flight writes for this device only.
	dc.mu.Lock()
	cm.mu.Lock()
	cur, ok := cm.daemonConns[deviceID]
	if !ok || cur != dc || dc.closed {
		cm.mu.Unlock()
		dc.mu.Unlock()
		return
	}
	dc.closed = true
	delete(cm.daemonConns, deviceID)
	cm.mu.Unlock()
	if dc.Conn != nil {
		_ = dc.Conn.Close()
	}
	dc.mu.Unlock()

	opslog.Info("server.ws", "daemon_disconnected", "daemon disconnected", "device_id", deviceID)
	if cm.onDeviceDown != nil {
		cm.onDeviceDown(deviceID)
	}
}

// AddPhone adds a phone WebSocket connection subscribed to a device.
func (cm *ConnectionManager) AddPhone(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	cm.phoneConns[deviceID] = append(cm.phoneConns[deviceID], conn)
	out, exists := cm.phoneOutbounds[conn]
	if !exists {
		out = newPhoneOutbound()
		cm.phoneOutbounds[conn] = out
	}
	cm.mu.Unlock()
	if !exists {
		go cm.phoneWriter(conn, out)
	}
}

// RemovePhone removes a phone WebSocket connection from a specific device subscription.
func (cm *ConnectionManager) RemovePhone(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	conns := cm.phoneConns[deviceID]
	filtered := withoutPhoneConn(conns, conn)
	if len(filtered) == 0 {
		delete(cm.phoneConns, deviceID)
	} else {
		cm.phoneConns[deviceID] = filtered
	}
	// Only drop write mutex if conn is not subscribed to any other device
	stillUsed := false
	for _, list := range cm.phoneConns {
		for _, c := range list {
			if c == conn {
				stillUsed = true
				break
			}
		}
		if stillUsed {
			break
		}
	}
	var stopped *phoneOutbound
	if !stillUsed {
		stopped = cm.phoneOutbounds[conn]
		delete(cm.phoneOutbounds, conn)
	}
	cm.mu.Unlock()
	stopped.close()
}

// ResubscribePhone moves a phone connection from any previous device to deviceID.
func (cm *ConnectionManager) ResubscribePhone(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	// Remove from all device lists first
	for id, conns := range cm.phoneConns {
		filtered := withoutPhoneConn(conns, conn)
		if len(filtered) == 0 {
			delete(cm.phoneConns, id)
		} else {
			cm.phoneConns[id] = filtered
		}
	}
	cm.phoneConns[deviceID] = append(cm.phoneConns[deviceID], conn)
	out, exists := cm.phoneOutbounds[conn]
	if !exists {
		out = newPhoneOutbound()
		cm.phoneOutbounds[conn] = out
	}
	cm.mu.Unlock()
	if !exists {
		go cm.phoneWriter(conn, out)
	}
}

func withoutPhoneConn(conns []*websocket.Conn, target *websocket.Conn) []*websocket.Conn {
	filtered := conns[:0]
	for _, conn := range conns {
		if conn != target {
			filtered = append(filtered, conn)
		}
	}
	return filtered
}

// SafeWritePhone writes a JSON message to a phone connection with mutex protection.
func (cm *ConnectionManager) SafeWritePhone(conn *websocket.Conn, msg *protocol.NekoMessage) {
	cm.mu.RLock()
	out, ok := cm.phoneOutbounds[conn]
	cm.mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		opslog.Error("server.ws", "phone_message_marshal_failed", "phone message marshal failed", err)
		return
	}

	if !enqueuePhone(out, data) {
		opslog.Warn("server.ws", "phone_outbound_queue_saturated", "phone outbound queue saturated")
		cm.dropPhone(conn)
	}
}

// SafeWritePing sends a ping to a phone connection with mutex protection.
func (cm *ConnectionManager) SafeWritePing(conn *websocket.Conn) error {
	cm.mu.RLock()
	out, ok := cm.phoneOutbounds[conn]
	cm.mu.RUnlock()

	if !ok {
		return ErrDeviceOffline
	}

	out.writeMu.Lock()
	defer out.writeMu.Unlock()
	// Re-check after waiting for an in-flight text write. close() never takes
	// writeMu, so this lock order cannot deadlock with outbound state changes.
	if out.isClosed() {
		return ErrDeviceOffline
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := conn.WriteMessage(websocket.PingMessage, nil)
	conn.SetWriteDeadline(time.Time{})
	return err
}

// SafeWriteDaemon writes a WebSocket frame to a daemon connection under its write lock.
// All daemon writers (app messages, pings) must go through this to avoid concurrent writes.
func (cm *ConnectionManager) SafeWriteDaemon(dc *DaemonConn, messageType int, data []byte) error {
	if dc == nil || dc.Conn == nil {
		return ErrDeviceOffline
	}
	return cm.writeDaemonGeneration(dc, dc.generation, messageType, data)
}

func (cm *ConnectionManager) writeDaemonGeneration(dc *DaemonConn, generation uint64, messageType int, data []byte) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.closed || dc.generation != generation || dc.Conn == nil {
		return ErrDeviceOffline
	}
	dc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := dc.Conn.WriteMessage(messageType, data)
	dc.Conn.SetWriteDeadline(time.Time{})
	return err
}

// SendToDaemon sends a message to a specific daemon.
func (cm *ConnectionManager) SendToDaemon(deviceID string, msg *protocol.NekoMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	var generation uint64
	if ok {
		generation = dc.generation
	}
	cm.mu.RUnlock()
	if !ok {
		return ErrDeviceOffline
	}
	return cm.writeDaemonGeneration(dc, generation, websocket.TextMessage, data)
}

// sendToDaemonLocked writes a response while the daemon read handler already
// holds dc.mu for its generation lease.
func (cm *ConnectionManager) sendToDaemonLocked(dc *DaemonConn, msg *protocol.NekoMessage) error {
	if dc == nil || dc.closed || dc.Conn == nil {
		return ErrDeviceOffline
	}
	if !cm.isLiveDaemonLocked(dc) {
		return ErrDeviceOffline
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	dc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = dc.Conn.WriteMessage(websocket.TextMessage, data)
	dc.Conn.SetWriteDeadline(time.Time{})
	return err
}

// BroadcastToPhones sends a message to all phones subscribed to a device.
func (cm *ConnectionManager) BroadcastToPhones(deviceID string, msg *protocol.NekoMessage) {
	cm.mu.RLock()
	conns := make([]*websocket.Conn, len(cm.phoneConns[deviceID]))
	copy(conns, cm.phoneConns[deviceID])
	cm.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		opslog.Error("server.ws", "phone_message_marshal_failed", "phone message marshal failed", err)
		return
	}

	for _, conn := range conns {
		cm.mu.RLock()
		out, ok := cm.phoneOutbounds[conn]
		cm.mu.RUnlock()

		if !ok {
			continue
		}
		if !enqueuePhone(out, data) {
			opslog.Warn("server.ws", "phone_outbound_queue_saturated", "phone outbound queue saturated")
			cm.dropPhone(conn)
		}
	}
}

func enqueuePhone(out *phoneOutbound, data []byte) bool {
	if out == nil {
		return false
	}
	frameBytes := int64(len(data))
	if frameBytes > maxPhoneOutboundBytes {
		return false
	}
	out.stateMu.Lock()
	defer out.stateMu.Unlock()
	if out.closed {
		return false
	}
	if frameBytes > maxPhoneOutboundBytes-out.reservedBytes {
		return false
	}
	// Reserve before publishing the frame. phoneWriter releases this only after
	// WriteMessage completes, so the budget includes queued and in-flight data.
	out.reservedBytes += frameBytes
	select {
	case out.queue <- data:
		return true
	default:
		out.releaseBytesLocked(frameBytes)
		return false
	}
}

func (cm *ConnectionManager) phoneWriter(conn *websocket.Conn, out *phoneOutbound) {
	for {
		select {
		case data := <-out.queue:
			if out.isClosed() {
				out.releaseBytes(int64(len(data)))
				return
			}
			out.writeMu.Lock()
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := conn.WriteMessage(websocket.TextMessage, data)
			conn.SetWriteDeadline(time.Time{})
			out.writeMu.Unlock()
			out.releaseBytes(int64(len(data)))
			if err != nil {
				opslog.Error("server.ws", "phone_write_failed", "phone websocket write failed", err)
				cm.dropPhone(conn)
				return
			}
		case <-out.done:
			return
		}
	}
}

func (cm *ConnectionManager) dropPhone(conn *websocket.Conn) {
	cm.mu.Lock()
	for id, conns := range cm.phoneConns {
		filtered := withoutPhoneConn(conns, conn)
		if len(filtered) == 0 {
			delete(cm.phoneConns, id)
		} else {
			cm.phoneConns[id] = filtered
		}
	}
	out := cm.phoneOutbounds[conn]
	delete(cm.phoneOutbounds, conn)
	cm.mu.Unlock()

	out.close()
	_ = conn.Close()
}

// IsDaemonOnline checks if a daemon is currently connected.
func (cm *ConnectionManager) IsDaemonOnline(deviceID string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	_, ok := cm.daemonConns[deviceID]
	return ok
}

// GetOnlineDevices returns a list of online device IDs.
func (cm *ConnectionManager) GetOnlineDevices() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	ids := make([]string, 0, len(cm.daemonConns))
	for id := range cm.daemonConns {
		ids = append(ids, id)
	}
	return ids
}

// UpdateSessions updates the sessions for a device and notifies phones.
func (cm *ConnectionManager) UpdateSessions(deviceID string, sessions []*protocol.AgentSession) {
	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	cm.mu.RUnlock()
	if !ok {
		return
	}
	cm.UpdateSessionsFrom(dc, sessions)
}

// UpdateSessionListFrom applies a daemon session_list snapshot, including its
// optional device-scoped thread-start capability catalog. The catalog belongs
// to the current daemon generation just like the session cache.
func (cm *ConnectionManager) UpdateSessionListFrom(
	dc *DaemonConn,
	sessions []*protocol.AgentSession,
	capabilities []protocol.AgentStartCapability,
) {
	cm.UpdateSessionListFromVersion(dc, sessions, capabilities, protocol.CurrentProtocolVersion)
}

// UpdateSessionListFromVersion preserves the producer daemon's negotiated
// protocol version. Open-mode snapshots must not be restamped as if an older
// daemon had emitted new explicit capability fields.
func (cm *ConnectionManager) UpdateSessionListFromVersion(
	dc *DaemonConn,
	sessions []*protocol.AgentSession,
	capabilities []protocol.AgentStartCapability,
	producerVersion string,
) {
	if dc == nil {
		return
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	cm.updateSessionListFromLocked(dc, sessions, capabilities, producerVersion)
}

// IsLiveDaemon reports whether dc is still the registered connection for its device.
func (cm *ConnectionManager) IsLiveDaemon(dc *DaemonConn) bool {
	if dc == nil {
		return false
	}
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return cm.isLiveDaemonLocked(dc)
}

// isLiveDaemonLocked requires dc.mu to be held by the caller.
func (cm *ConnectionManager) isLiveDaemonLocked(dc *DaemonConn) bool {
	if dc == nil || dc.closed {
		return false
	}
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cur, ok := cm.daemonConns[dc.DeviceID]
	return ok && cur == dc
}

// UpdateSessionsFrom applies a session list only if dc is still the live daemon
// connection for its device (stale reconnect frames must not overwrite the new cache).
func (cm *ConnectionManager) UpdateSessionsFrom(dc *DaemonConn, sessions []*protocol.AgentSession) {
	cm.UpdateSessionListFrom(dc, sessions, nil)
}

// updateSessionListFromLocked requires dc.mu and orders the cache/broadcast
// before any replacement of this daemon generation.
func (cm *ConnectionManager) updateSessionListFromLocked(
	dc *DaemonConn,
	sessions []*protocol.AgentSession,
	capabilities []protocol.AgentStartCapability,
	producerVersion string,
) {
	if dc == nil || dc.closed {
		return
	}
	cm.mu.RLock()
	cur, ok := cm.daemonConns[dc.DeviceID]
	cm.mu.RUnlock()
	if !ok || cur != dc {
		return
	}

	clean := make([]*protocol.AgentSession, 0, len(sessions))
	dc.Sessions = make(map[string]*protocol.AgentSession)
	for _, s := range sessions {
		if s == nil || s.ID == "" || s.AgentType == protocol.AgentKilo {
			continue
		}
		dc.Sessions[s.ID] = s
		clean = append(clean, s)
	}
	dc.AgentStartCapabilities = cleanAgentStartCapabilities(capabilities)
	if _, err := protocol.ParseProtocolVersion(producerVersion); err != nil {
		producerVersion = ""
	}
	dc.CatalogProtocolVersion = producerVersion

	// Replacement waits on dc.mu, so this final broadcast is ordered before
	// the old generation is marked closed.
	msg := protocol.NewMessage(protocol.MsgSessionList, dc.DeviceID)
	msg.ProtocolVersion = producerVersion
	msg.Payload = map[string]any{"sessions": clean}
	if dc.AgentStartCapabilities != nil {
		msg.Payload["start_capabilities"] = dc.AgentStartCapabilities
	}
	cm.BroadcastToPhones(dc.DeviceID, msg)
}

// updateSealedCatalogFromLocked caches and relays an opaque catalog frame for
// the current daemon generation. The relay never decodes catalog ciphertext.
func (cm *ConnectionManager) updateSealedCatalogFromLocked(dc *DaemonConn, msg *protocol.NekoMessage) {
	if dc == nil || dc.closed || msg == nil || msg.SealedPayload == nil {
		return
	}
	cm.mu.RLock()
	cur, ok := cm.daemonConns[dc.DeviceID]
	cm.mu.RUnlock()
	if !ok || cur != dc {
		return
	}
	clone := cloneOpaqueMessage(msg)
	clone.DeviceID = dc.DeviceID
	dc.SealedCatalog = clone
	cm.BroadcastToPhones(dc.DeviceID, clone)
}

func cloneOpaqueMessage(msg *protocol.NekoMessage) *protocol.NekoMessage {
	if msg == nil {
		return nil
	}
	clone := *msg
	clone.Payload = nil
	if msg.SealedPayload != nil {
		sealedPayload := *msg.SealedPayload
		clone.SealedPayload = &sealedPayload
	}
	return &clone
}

func cleanAgentStartCapabilities(in []protocol.AgentStartCapability) []protocol.AgentStartCapability {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return []protocol.AgentStartCapability{}
	}
	known := map[protocol.AgentType]bool{
		protocol.AgentClaudeCode: true,
		protocol.AgentCodex:      true,
		protocol.AgentKimiCLI:    true,
		protocol.AgentGrokBuild:  true,
	}
	seen := make(map[protocol.AgentType]bool, len(in))
	clean := make([]protocol.AgentStartCapability, 0, len(in))
	for _, capability := range in {
		if !known[capability.AgentType] || seen[capability.AgentType] {
			continue
		}
		seen[capability.AgentType] = true
		clean = append(clean, capability)
	}
	return clean
}

// GetDeviceSessions returns the cached sessions for a device.
func (cm *ConnectionManager) GetDeviceSessions(deviceID string) []*protocol.AgentSession {
	sessions, _ := cm.GetDeviceSessionSnapshot(deviceID)
	return sessions
}

// GetDeviceSessionSnapshot returns the sessions and thread-start capability
// catalog from one live daemon generation. Absent catalog entries are
// deliberately not synthesized: consumers must default them to unavailable.
func (cm *ConnectionManager) GetDeviceSessionSnapshot(deviceID string) ([]*protocol.AgentSession, []protocol.AgentStartCapability) {
	sessions, capabilities, _ := cm.GetDeviceSessionSnapshotVersion(deviceID)
	return sessions, capabilities
}

// GetDeviceSessionSnapshotVersion returns the daemon producer version together
// with its cached open-mode catalog.
func (cm *ConnectionManager) GetDeviceSessionSnapshotVersion(deviceID string) ([]*protocol.AgentSession, []protocol.AgentStartCapability, string) {
	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	cm.mu.RUnlock()

	if !ok {
		return nil, nil, ""
	}

	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.closed {
		return nil, nil, ""
	}
	cm.mu.RLock()
	cur, live := cm.daemonConns[deviceID]
	cm.mu.RUnlock()
	if !live || cur != dc {
		return nil, nil, ""
	}

	sessions := make([]*protocol.AgentSession, 0, len(dc.Sessions))
	for _, s := range dc.Sessions {
		sessions = append(sessions, s)
	}
	var capabilities []protocol.AgentStartCapability
	if dc.AgentStartCapabilities != nil {
		capabilities = append([]protocol.AgentStartCapability{}, dc.AgentStartCapabilities...)
	}
	return sessions, capabilities, dc.CatalogProtocolVersion
}

// GetSealedCatalogSnapshot returns an opaque copy of the latest live catalog.
func (cm *ConnectionManager) GetSealedCatalogSnapshot(deviceID string) *protocol.NekoMessage {
	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	cm.mu.RUnlock()
	if !ok {
		return nil
	}
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	if dc.closed || dc.SealedCatalog == nil {
		return nil
	}
	cm.mu.RLock()
	cur, live := cm.daemonConns[deviceID]
	cm.mu.RUnlock()
	if !live || cur != dc {
		return nil
	}
	return cloneOpaqueMessage(dc.SealedCatalog)
}
