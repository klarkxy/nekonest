package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
)

// ConnectionManager handles all WebSocket connections.
type ConnectionManager struct {
	mu           sync.RWMutex
	daemonConns  map[string]*DaemonConn    // device_id -> daemon connection
	phoneConns   map[string][]*websocket.Conn // device_id -> phone connections (subscribed to that device)
	phoneWrites  map[*websocket.Conn]*sync.Mutex // per-phone write mutex
	database     *db.DB
	onDeviceUp   func(deviceID string)
	onDeviceDown func(deviceID string)
}

// DaemonConn wraps a daemon's WebSocket connection with metadata.
type DaemonConn struct {
	Conn      *websocket.Conn
	DeviceID  string
	LastPing  time.Time
	Sessions  map[string]*protocol.AgentSession
	mu        sync.RWMutex
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(database *db.DB) *ConnectionManager {
	return &ConnectionManager{
		daemonConns: make(map[string]*DaemonConn),
		phoneConns:  make(map[string][]*websocket.Conn),
		phoneWrites: make(map[*websocket.Conn]*sync.Mutex),
		database:    database,
	}
}

// OnDeviceUp sets the callback for when a device comes online.
func (cm *ConnectionManager) OnDeviceUp(fn func(string)) {
	cm.onDeviceUp = fn
}

// OnDeviceDown sets the callback for when a device goes offline.
func (cm *ConnectionManager) OnDeviceDown(fn func(string)) {
	cm.onDeviceDown = fn
}

// AddDaemon registers a daemon WebSocket connection.
// If a previous connection exists for the same device, it is closed and replaced.
// onDeviceUp only fires when the device was previously offline.
func (cm *ConnectionManager) AddDaemon(deviceID string, conn *websocket.Conn) *DaemonConn {
	cm.mu.Lock()
	var stale *websocket.Conn
	wasOnline := false
	if old, ok := cm.daemonConns[deviceID]; ok {
		wasOnline = true
		if old.Conn != conn {
			stale = old.Conn
		}
	}
	dc := &DaemonConn{
		Conn:     conn,
		DeviceID: deviceID,
		LastPing: time.Now(),
		Sessions: make(map[string]*protocol.AgentSession),
	}
	cm.daemonConns[deviceID] = dc
	cm.mu.Unlock()

	// Close previous socket outside the map lock so its read loop can exit cleanly.
	// RemoveDaemon is identity-aware, so the old loop will not wipe the new entry.
	if stale != nil {
		log.Printf("[ws] replacing daemon connection: %s", deviceID)
		_ = stale.Close()
	}

	log.Printf("[ws] daemon connected: %s", deviceID)
	cm.database.UpdateDeviceLastSeen(deviceID)
	if !wasOnline && cm.onDeviceUp != nil {
		cm.onDeviceUp(deviceID)
	}
	return dc
}

// RemoveDaemon removes a daemon connection only if conn is still the registered one.
// This prevents a stale reconnect teardown from deleting a newer connection.
func (cm *ConnectionManager) RemoveDaemon(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	cur, ok := cm.daemonConns[deviceID]
	if !ok || cur.Conn != conn {
		cm.mu.Unlock()
		return
	}
	delete(cm.daemonConns, deviceID)
	cm.mu.Unlock()

	log.Printf("[ws] daemon disconnected: %s", deviceID)
	if cm.onDeviceDown != nil {
		cm.onDeviceDown(deviceID)
	}
}

// AddPhone adds a phone WebSocket connection subscribed to a device.
func (cm *ConnectionManager) AddPhone(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	cm.phoneConns[deviceID] = append(cm.phoneConns[deviceID], conn)
	// Create per-phone write mutex to prevent concurrent writes
	if _, ok := cm.phoneWrites[conn]; !ok {
		cm.phoneWrites[conn] = &sync.Mutex{}
	}
	cm.mu.Unlock()
}

// RemovePhone removes a phone WebSocket connection from a specific device subscription.
func (cm *ConnectionManager) RemovePhone(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	conns := cm.phoneConns[deviceID]
	for i, c := range conns {
		if c == conn {
			cm.phoneConns[deviceID] = append(conns[:i], conns[i+1:]...)
			break
		}
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
	if !stillUsed {
		delete(cm.phoneWrites, conn)
	}
	cm.mu.Unlock()
}

// ResubscribePhone moves a phone connection from any previous device to deviceID.
func (cm *ConnectionManager) ResubscribePhone(deviceID string, conn *websocket.Conn) {
	cm.mu.Lock()
	// Remove from all device lists first
	for id, conns := range cm.phoneConns {
		for i, c := range conns {
			if c == conn {
				cm.phoneConns[id] = append(conns[:i], conns[i+1:]...)
				break
			}
		}
	}
	cm.phoneConns[deviceID] = append(cm.phoneConns[deviceID], conn)
	if _, ok := cm.phoneWrites[conn]; !ok {
		cm.phoneWrites[conn] = &sync.Mutex{}
	}
	cm.mu.Unlock()
}

// SafeWritePhone writes a JSON message to a phone connection with mutex protection.
func (cm *ConnectionManager) SafeWritePhone(conn *websocket.Conn, msg *protocol.NekoMessage) {
	cm.mu.RLock()
	writeMu, ok := cm.phoneWrites[conn]
	cm.mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ws] marshal error: %v", err)
		return
	}

	writeMu.Lock()
	defer writeMu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("[ws] phone write error: %v", err)
	}
	conn.SetWriteDeadline(time.Time{})
}

// SafeWritePing sends a ping to a phone connection with mutex protection.
func (cm *ConnectionManager) SafeWritePing(conn *websocket.Conn) error {
	cm.mu.RLock()
	writeMu, ok := cm.phoneWrites[conn]
	cm.mu.RUnlock()

	if !ok {
		return ErrDeviceOffline
	}

	writeMu.Lock()
	defer writeMu.Unlock()
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
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := dc.Conn.WriteMessage(messageType, data)
	dc.Conn.SetWriteDeadline(time.Time{})
	return err
}

// SendToDaemon sends a message to a specific daemon.
func (cm *ConnectionManager) SendToDaemon(deviceID string, msg *protocol.NekoMessage) error {
	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	cm.mu.RUnlock()

	if !ok {
		return ErrDeviceOffline
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return cm.SafeWriteDaemon(dc, websocket.TextMessage, data)
}

// BroadcastToPhones sends a message to all phones subscribed to a device.
func (cm *ConnectionManager) BroadcastToPhones(deviceID string, msg *protocol.NekoMessage) {
	cm.mu.RLock()
	conns := make([]*websocket.Conn, len(cm.phoneConns[deviceID]))
	copy(conns, cm.phoneConns[deviceID])
	cm.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ws] marshal error: %v", err)
		return
	}

	for _, conn := range conns {
		cm.mu.RLock()
		writeMu, ok := cm.phoneWrites[conn]
		cm.mu.RUnlock()

		if !ok {
			continue
		}

		writeMu.Lock()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[ws] phone send error: %v", err)
		}
		conn.SetWriteDeadline(time.Time{})
		writeMu.Unlock()
	}
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

	dc.mu.Lock()
	dc.Sessions = make(map[string]*protocol.AgentSession)
	for _, s := range sessions {
		dc.Sessions[s.ID] = s
	}
	dc.mu.Unlock()

	// Notify subscribed phones
	msg := protocol.NewMessage(protocol.MsgSessionList, deviceID)
	msg.Payload = map[string]any{"sessions": sessions}
	cm.BroadcastToPhones(deviceID, msg)
}

// GetDeviceSessions returns the cached sessions for a device.
func (cm *ConnectionManager) GetDeviceSessions(deviceID string) []*protocol.AgentSession {
	cm.mu.RLock()
	dc, ok := cm.daemonConns[deviceID]
	cm.mu.RUnlock()

	if !ok {
		return nil
	}

	dc.mu.RLock()
	defer dc.mu.RUnlock()

	sessions := make([]*protocol.AgentSession, 0, len(dc.Sessions))
	for _, s := range dc.Sessions {
		sessions = append(sessions, s)
	}
	return sessions
}
