package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
	pushsub "github.com/nekonest/server/internal/push"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (daemon)
		}
		return isAllowedOrigin(r, origin)
	},
}

const (
	pongWait                 = 60 * time.Second
	pingPeriod               = (pongWait * 9) / 10
	unauthenticatedReadLimit = 16 << 10 // first auth/subscribe frame only
	authenticatedReadLimit   = 4 << 20  // session history can contain CJK text
)

// rateLimiter provides simple per-IP rate limiting.
type rateLimiter struct {
	mu       sync.Mutex
	requests map[string]rateLimitEntry
	limit    int
	window   time.Duration
	maxKeys  int
}

type rateLimitEntry struct {
	windowStart time.Time
	count       int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string]rateLimitEntry),
		limit:    limit,
		window:   window,
		maxKeys:  4096,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.requests[key]
	if exists {
		if now.Sub(entry.windowStart) >= rl.window {
			entry = rateLimitEntry{windowStart: now}
		}
		if entry.count >= rl.limit {
			rl.requests[key] = entry
			return false
		}
		entry.count++
		rl.requests[key] = entry
		return true
	}

	// Keep memory and lock work bounded even if proxy headers are spoofed.
	// At capacity, examine at most a small fixed batch rather than scanning the
	// whole map on every subsequent request.
	if len(rl.requests) >= rl.maxKeys {
		checked := 0
		for candidate, value := range rl.requests {
			if now.Sub(value.windowStart) >= rl.window {
				delete(rl.requests, candidate)
			}
			checked++
			if checked >= 64 {
				break
			}
		}
		if len(rl.requests) >= rl.maxKeys {
			return false
		}
	}
	rl.requests[key] = rateLimitEntry{windowStart: now, count: 1}
	return true
}

// Global rate limiter: 60 requests per minute per IP
var wsRateLimiter = newRateLimiter(60, time.Minute)

// HandleDaemonWS handles WebSocket connections from Windows daemons.
func (s *Server) HandleDaemonWS(w http.ResponseWriter, r *http.Request) {
	// P2-F: Rate limiting (per IP host, not host:ephemeral-port)
	clientIP := clientIPKey(r)
	if !wsRateLimiter.allow(clientIP) {
		log.Printf("[ws] rate limit exceeded for daemon: %s", clientIP)
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] daemon upgrade error: %v", err)
		return
	}
	conn.SetReadLimit(unauthenticatedReadLimit)

	// First message must be auth
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	var authMsg protocol.NekoMessage
	if err := json.Unmarshal(data, &authMsg); err != nil || authMsg.Type != protocol.MsgRegisterDevice {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "expected auth message"},
		}))
		conn.Close()
		return
	}

	if hs := s.negotiateFirstFrame(&authMsg); hs.ErrorCode != "" {
		s.writeHandshakeError(conn, hs)
		return
	}

	deviceID, _ := authMsg.Payload["device_id"].(string)
	token, _ := authMsg.Payload["token"].(string)

	if !s.db.ValidateDeviceToken(deviceID, token) {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "invalid token"},
		}))
		conn.Close()
		return
	}
	conn.SetReadLimit(authenticatedReadLimit)

	// Send auth success with negotiated protocol metadata.
	hs := protocol.NegotiateHandshake(
		authMsg.ProtocolVersion,
		string(authMsg.TransportMode),
		s.TransportMode(),
		0,
	)
	_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
		Type:      protocol.MsgAuthResponse,
		DeviceID:  deviceID,
		Timestamp: time.Now().Unix(),
		Payload: map[string]any{
			"status":           "authenticated",
			"protocol_version": hs.NegotiatedVersion,
			"transport_mode":   string(hs.TransportMode),
			"server_version":   protocol.CurrentProtocolVersion,
		},
	}))

	// Set read deadline + pong handler for daemon connection
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	dc := s.connMgr.AddDaemon(deviceID, conn)
	s.replayPromptCommits(dc)

	// Start heartbeat
	go s.daemonPingLoop(dc)

	// Read loop
	s.daemonReadLoop(dc)
}

func (s *Server) daemonReadLoop(dc *DaemonConn) {
	defer func() {
		s.connMgr.RemoveDaemon(dc.DeviceID, dc.Conn)
	}()

	for {
		_, data, err := dc.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] daemon read error: %v", err)
			}
			return
		}

		var msg protocol.NekoMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[ws] daemon unmarshal error: %v", err)
			continue
		}

		dc.mu.Lock()
		dc.LastPing = time.Now()
		dc.mu.Unlock()

		s.handleDaemonMessage(dc, &msg)
	}
}

func (s *Server) handleDaemonMessage(dc *DaemonConn, msg *protocol.NekoMessage) {
	// Hold this generation's lifecycle lock through every side effect. Device
	// replacement waits on the same lock, so stale frames can never mutate DB
	// or enqueue phone events after the new generation is installed.
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if !s.connMgr.isLiveDaemonLocked(dc) {
		return
	}
	// The authenticated connection, never daemon-supplied JSON, is the
	// authoritative device identity for persistence and phone filtering.
	msg.DeviceID = dc.DeviceID
	switch msg.Type {
	case protocol.MsgSessionList:
		// Daemon reported session list
		sessionsData, ok := msg.Payload["sessions"]
		if !ok {
			return
		}
		var sessions []*protocol.AgentSession
		data, _ := json.Marshal(sessionsData)
		json.Unmarshal(data, &sessions)
		// Normalize wire contract: always stamp device_id; tolerate missing fields from older daemons.
		for _, sess := range sessions {
			if sess == nil {
				continue
			}
			sess.DeviceID = dc.DeviceID
		}
		s.connMgr.updateSessionsFromLocked(dc, sessions)

		// Also persist to database for offline querying
		n := 0
		for _, sess := range sessions {
			if sess != nil {
				n++
			}
		}
		s.db.UpdateDeviceSessions(dc.DeviceID, n)

	case protocol.MsgSessionUpdate:
		// Single session update (status change, new approval, etc.)
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)
		if msg.SealedPayload == nil && needsApprovalPush(msg.Payload) {
			s.sendPushNotification(dc.DeviceID, msg.SessionID, "有会话需要处理", "打开 NekoNest 查看")
		}

	case protocol.MsgAttentionEvent:
		// Generic sealed-safe push: no application plaintext in notification body.
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)
		className := ""
		if msg.Payload != nil {
			if c, ok := msg.Payload["class"].(string); ok {
				className = c
			}
			if c, ok := msg.Payload["event_class"].(string); ok && c != "" {
				className = c
			}
		}
		title := "NekoNest"
		body := "有一个会话需要处理"
		switch className {
		case "waiting_approval", "approval":
			body = "有会话等待审批"
		case "waiting_user", "needs_you":
			body = "有会话在等你"
		case "failed", "error":
			body = "有会话运行失败"
		case "completed", "success":
			body = "有会话已完成"
		case "device_offline":
			body = "设备已离线"
		}
		s.sendPushNotification(dc.DeviceID, msg.SessionID, title, body)

	case protocol.MsgSessionMessage:
		// Open mode: persist plaintext message. Sealed mode: persist opaque
		// ciphertext only (no application plaintext on the nest).
		if msg.SealedPayload != nil {
			if err := s.db.SaveSealedMessage(dc.DeviceID, msg.SessionID, msg); err != nil {
				log.Printf("[ws] save sealed message error: %v", err)
			}
		} else if msg.Payload != nil {
			if msgData, ok := msg.Payload["message"]; ok {
				data, _ := json.Marshal(msgData)
				var sessionMsg protocol.SessionMessage
				if err := json.Unmarshal(data, &sessionMsg); err == nil {
					if err := s.db.SaveMessage(dc.DeviceID, msg.SessionID, &sessionMsg); err != nil {
						log.Printf("[ws] save message error: %v", err)
					}
				}
			}
		}
		// Forward to all phones
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

		// A tool_call is informational unless the daemon explicitly marks it as
		// a permission request or includes pending approval state.
		// Sealed mode: only daemon-originated attention_event should push details.
		if msg.SealedPayload == nil && needsApprovalPush(msg.Payload) {
			s.sendPushNotification(dc.DeviceID, msg.SessionID, "⚠️ 操作需要审批", "点击查看详情")
		}

	case protocol.MsgSessionHistory:
		// Imported PC transcript — persist (best-effort) then forward
		if raw, ok := msg.Payload["messages"].([]any); ok {
			for _, item := range raw {
				data, err := json.Marshal(item)
				if err != nil {
					continue
				}
				var sm protocol.SessionMessage
				if err := json.Unmarshal(data, &sm); err != nil || sm.ID == "" {
					continue
				}
				if sm.Type == "" {
					sm.Type = "text"
				}
				// Ignore duplicate insert errors
				_ = s.db.SaveMessage(dc.DeviceID, msg.SessionID, &sm)
			}
		}
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

	case protocol.MsgPromptNotSeen:
		clientMsgID := promptClientMsgID(msg)
		if clientMsgID == "" {
			log.Printf("[ws] prompt_not_seen without valid client_msg_id from %s", dc.DeviceID)
			return
		}
		cmd, err := s.db.GetPromptCommand(dc.DeviceID, clientMsgID)
		if err != nil {
			log.Printf("[ws] prompt_not_seen unknown command %s/%s: %v", dc.DeviceID, clientMsgID, err)
			return
		}
		if cmd.Status != db.PromptPending {
			return
		}
		retry := promptCommandMessage(cmd)
		if err := s.connMgr.sendToDaemonLocked(dc, retry); err != nil {
			// Keep pending. The next phone outbox flush will query again.
			log.Printf("[ws] resend not-seen prompt %s/%s: %v", dc.DeviceID, clientMsgID, err)
		}

	case protocol.MsgPromptAccepted:
		clientMsgID := promptClientMsgID(msg)
		if clientMsgID == "" {
			log.Printf("[ws] prompt_accepted without valid client_msg_id from %s", dc.DeviceID)
			return
		}
		cmd, _, err := s.db.MarkPromptAccepted(dc.DeviceID, clientMsgID)
		if err != nil {
			log.Printf("[ws] accept unknown prompt %s/%s: %v", dc.DeviceID, clientMsgID, err)
			return
		}
		if cmd.Status != db.PromptAccepted {
			// Only a durably accepted command may become a phone ACK.
			return
		}
		// Upsert every time so a crash between the durable status transition
		// and message persistence is healed by a duplicate daemon ACK.
		if err := s.persistAcceptedPrompt(cmd); err != nil {
			log.Printf("[ws] save accepted user prompt: %v", err)
			return
		}
		s.sendPromptCommittedLocked(dc, cmd)
		// ACK delivery is intentionally at-least-once. A duplicate daemon
		// acceptance can be the recovery signal after a crash between the
		// durable accept and the original phone broadcast.
		s.broadcastPromptSent(cmd)

	case protocol.MsgPromptFailed:
		clientMsgID := promptClientMsgID(msg)
		if clientMsgID == "" {
			log.Printf("[ws] prompt_failed without valid client_msg_id from %s", dc.DeviceID)
			return
		}
		failure := "daemon rejected prompt"
		outcome := db.PromptFailed
		retryAllowed := true
		if msg.Payload != nil {
			if value, ok := msg.Payload["error"].(string); ok && strings.TrimSpace(value) != "" {
				failure = strings.TrimSpace(value)
			} else if value, ok := msg.Payload["message"].(string); ok && strings.TrimSpace(value) != "" {
				failure = strings.TrimSpace(value)
			}
			if value, ok := msg.Payload["outcome"].(string); ok && strings.TrimSpace(value) != "" {
				outcome = strings.TrimSpace(value)
			}
			if value, ok := msg.Payload["retry_allowed"].(bool); ok {
				retryAllowed = value
			}
		}
		if outcome == db.PromptIndeterminate {
			retryAllowed = false
		}
		cmd, _, err := s.db.MarkPromptFailed(
			dc.DeviceID,
			clientMsgID,
			failure,
			outcome,
			retryAllowed,
		)
		if err != nil {
			log.Printf("[ws] fail unknown prompt %s/%s: %v", dc.DeviceID, clientMsgID, err)
			return
		}
		if cmd.Status == db.PromptFailed || cmd.Status == db.PromptIndeterminate {
			s.broadcastPromptFailed(cmd)
		}

	case protocol.MsgError:
		// Legacy daemon errors cannot be promoted to a prompt ACK because they
		// do not establish durable acceptance. Forward for compatibility only.
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

	case protocol.MsgThreadStarting,
		protocol.MsgThreadOwned,
		protocol.MsgThreadFailed,
		protocol.MsgThreadIndeterminate,
		protocol.MsgKeyPackage,
		protocol.MsgPairReady:
		// Lifecycle / crypto control frames — fan out to phones as-is.
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

	case protocol.MsgHeartbeat:
		// Just keep-alive, already updated LastPing

	default:
		log.Printf("[ws] unexpected message type from daemon: %s", msg.Type)
	}
}

func (s *Server) daemonPingLoop(dc *DaemonConn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dc.mu.RLock()
			lastPing := dc.LastPing
			dc.mu.RUnlock()

			if time.Since(lastPing) > pongWait {
				s.connMgr.RemoveDaemon(dc.DeviceID, dc.Conn)
				return
			}

			if err := s.connMgr.SafeWriteDaemon(dc, websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// HandlePhoneWS handles WebSocket connections from PWA phones.
func (s *Server) HandlePhoneWS(w http.ResponseWriter, r *http.Request) {
	// P2-F: Rate limiting (per IP host, not host:ephemeral-port)
	clientIP := clientIPKey(r)
	if !wsRateLimiter.allow(clientIP) {
		log.Printf("[ws] rate limit exceeded for phone: %s", clientIP)
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}

	// Optional secret via query before upgrade (some clients prefer this)
	if s.phoneSecret != "" {
		if phoneSecretFromRequest(r) != s.phoneSecret {
			// Allow secret in first WS message instead
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] phone upgrade error: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(unauthenticatedReadLimit)

	// First message: subscribe { type, device_id, payload.secret? }
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	var subscribeMsg protocol.NekoMessage
	if err := json.Unmarshal(data, &subscribeMsg); err != nil {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "invalid json"},
		}))
		conn.Close()
		return
	}
	if subscribeMsg.Type != protocol.MsgSubscribe {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "first message must be subscribe"},
		}))
		return
	}

	if hs := s.negotiateFirstFrame(&subscribeMsg); hs.ErrorCode != "" {
		s.writeHandshakeError(conn, hs)
		return
	}

	// Phone auth: admin secret or independent phone token from query/header/payload.
	var phoneAuth *db.PhoneAuth
	if s.phoneSecret != "" {
		secret := phoneSecretFromRequest(r)
		if secret == "" && subscribeMsg.Payload != nil {
			if p, ok := subscribeMsg.Payload["secret"].(string); ok {
				secret = p
			}
			if secret == "" {
				if p, ok := subscribeMsg.Payload["phone_token"].(string); ok {
					secret = p
				}
			}
		}
		if secureEqual(secret, s.phoneSecret) {
			phoneAuth = &db.PhoneAuth{AdminBypass: true, Name: "admin"}
		} else if auth, err := s.db.ValidatePhoneToken(secret); err == nil {
			phoneAuth = auth
		} else {
			_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
				Type:      protocol.MsgError,
				Timestamp: time.Now().Unix(),
				Payload:   map[string]any{"message": "unauthorized"},
			}))
			conn.Close()
			return
		}
	}

	deviceID := subscribeMsg.DeviceID
	if deviceID == "" && subscribeMsg.Payload != nil {
		if id, ok := subscribeMsg.Payload["device_id"].(string); ok {
			deviceID = id
		}
	}
	if deviceID == "" {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "device_id required in first message (type=subscribe)"},
		}))
		conn.Close()
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if len(deviceID) > 256 {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "invalid device_id"},
		}))
		return
	}
	if !s.db.DeviceExists(deviceID) {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "unknown device"},
		}))
		return
	}
	if !s.phoneMayAccessDevice(phoneAuth, deviceID) {
		_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "forbidden"},
		}))
		conn.Close()
		return
	}
	subscriptionID, validSubscriptionID := subscribeRequestID(subscribeMsg.Payload)
	if !validSubscriptionID {
		conn.WriteJSON(protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "invalid subscription_id"},
		})
		return
	}
	conn.SetReadLimit(authenticatedReadLimit)

	s.connMgr.AddPhone(deviceID, conn)
	currentDevice := deviceID
	defer func() {
		s.connMgr.RemovePhone(currentDevice, conn)
	}()

	s.writeSubscribeAck(conn, deviceID, subscriptionID)
	s.pushPhoneSnapshot(conn, deviceID)

	// Set read deadline + pong handler for phone connection
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Read loop for phone commands
	currentDevice = s.phoneReadLoop(conn, currentDevice)
}

// pushPhoneSnapshot sends session_list + device_list for the subscribed device.
func (s *Server) pushPhoneSnapshot(conn *websocket.Conn, deviceID string) {
	sessions := s.connMgr.GetDeviceSessions(deviceID)
	if sessions == nil {
		sessions = []*protocol.AgentSession{}
	}
	sessionMsg := protocol.NewMessage(protocol.MsgSessionList, deviceID)
	sessionMsg.Payload = map[string]any{"sessions": sessions}
	s.connMgr.SafeWritePhone(conn, sessionMsg)

	devices, _ := s.db.ListDevices()
	onlineDevices := s.connMgr.GetOnlineDevices()
	onlineSet := make(map[string]bool)
	for _, id := range onlineDevices {
		onlineSet[id] = true
	}
	for _, d := range devices {
		if onlineSet[d.ID] {
			d.Status = "online"
		}
	}

	statusMsg := protocol.NewMessage(protocol.MsgDeviceList, "")
	statusMsg.Payload = map[string]any{"devices": devices}
	s.connMgr.SafeWritePhone(conn, statusMsg)
}

func (s *Server) phoneReadLoop(conn *websocket.Conn, deviceID string) string {
	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.connMgr.SafeWritePing(conn); err != nil {
					_ = conn.Close()
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	var switches []time.Time
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return deviceID
		}

		var msg protocol.NekoMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		if msg.Type == protocol.MsgSubscribe {
			newID := msg.DeviceID
			if newID == "" && msg.Payload != nil {
				if id, ok := msg.Payload["device_id"].(string); ok {
					newID = id
				}
			}
			subscriptionID, validSubscriptionID := subscribeRequestID(msg.Payload)
			if newID == "" {
				s.writeSubscribeError(conn, deviceID, subscriptionID, "device_id required")
				continue
			}
			newID = strings.TrimSpace(newID)
			if len(newID) > 256 {
				s.writeSubscribeError(conn, deviceID, subscriptionID, "invalid device_id")
				continue
			}
			if !validSubscriptionID {
				s.writeSubscribeError(conn, newID, "", "invalid subscription_id")
				continue
			}
			if newID == deviceID {
				s.writeSubscribeAck(conn, deviceID, subscriptionID)
				continue
			}
			if !allowDeviceSwitch(&switches, time.Now()) {
				s.writeSubscribeError(conn, newID, subscriptionID, "too many device switches")
				continue
			}
			if !s.db.DeviceExists(newID) {
				s.writeSubscribeError(conn, newID, subscriptionID, "unknown device")
				continue
			}
			s.connMgr.ResubscribePhone(newID, conn)
			deviceID = newID
			s.writeSubscribeAck(conn, newID, subscriptionID)
			s.pushPhoneSnapshot(conn, newID)
			continue
		}

		frameDeviceID := strings.TrimSpace(msg.DeviceID)
		if msg.Type == protocol.MsgHeartbeat {
			if frameDeviceID != "" && frameDeviceID != deviceID {
				s.writePhoneError(conn, deviceID, msg.SessionID, "device_id does not match subscription")
				continue
			}
		} else if frameDeviceID != deviceID {
			s.writePhoneError(conn, deviceID, msg.SessionID, "device_id does not match subscription")
			continue
		}
		msg.DeviceID = deviceID
		s.handlePhoneMessage(deviceID, &msg)
	}
}

func allowDeviceSwitch(switches *[]time.Time, now time.Time) bool {
	const (
		switchWindow = time.Minute
		switchLimit  = 6
	)
	cutoff := now.Add(-switchWindow)
	valid := (*switches)[:0]
	for _, changedAt := range *switches {
		if changedAt.After(cutoff) {
			valid = append(valid, changedAt)
		}
	}
	if len(valid) >= switchLimit {
		*switches = valid
		return false
	}
	*switches = append(valid, now)
	return true
}

func (s *Server) writePhoneError(conn *websocket.Conn, deviceID, sessionID, message string) {
	errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, sessionID)
	errMsg.Payload = map[string]any{"message": message}
	s.connMgr.SafeWritePhone(conn, errMsg)
}

func (s *Server) writeSubscribeError(
	conn *websocket.Conn,
	requestedDeviceID, subscriptionID, message string,
) {
	errMsg := protocol.NewMessage(protocol.MsgError, requestedDeviceID)
	errMsg.Payload = map[string]any{
		"message":         message,
		"device_id":       requestedDeviceID,
		"subscription_id": subscriptionID,
	}
	s.connMgr.SafeWritePhone(conn, errMsg)
}

func subscribeRequestID(payload map[string]any) (string, bool) {
	if payload == nil {
		return "", true
	}
	raw, exists := payload["subscription_id"]
	if !exists {
		return "", true
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		return "", false
	}
	return value, true
}

func (s *Server) writeSubscribeAck(conn *websocket.Conn, deviceID, subscriptionID string) {
	ack := s.stampEnvelope(protocol.NewMessage(protocol.MsgSubscribeAck, deviceID))
	ack.Payload = map[string]any{
		"status":           "subscribed",
		"device_id":        deviceID,
		"subscription_id":  subscriptionID,
		"protocol_version": protocol.CurrentProtocolVersion,
		"transport_mode":   string(s.TransportMode()),
		"server_version":   protocol.CurrentProtocolVersion,
	}
	s.connMgr.SafeWritePhone(conn, ack)
}

func (s *Server) handlePhoneMessage(deviceID string, msg *protocol.NekoMessage) {
	switch msg.Type {
	case protocol.MsgSubscribe, protocol.MsgHeartbeat:
		// Subscription handled in read loop; heartbeat is keep-alive
		return

	case protocol.MsgFetchHistory:
		msg.DeviceID = deviceID
		if msg.Payload == nil {
			msg.Payload = map[string]any{}
		}
		limit := historyLimit(msg.Payload)
		msg.Payload["limit"] = limit
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			// Fallback: whatever NekoNest already persisted
			rows, dbErr := s.db.GetMessages(deviceID, msg.SessionID, limit+1)
			if dbErr != nil {
				errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, msg.SessionID)
				errMsg.Payload = map[string]any{"message": "device offline"}
				s.connMgr.BroadcastToPhones(deviceID, errMsg)
				return
			}
			rows, truncated := truncateHistory(rows, limit)
			msgs := make([]any, 0, len(rows))
			for _, m := range rows {
				msgs = append(msgs, m)
			}
			out := protocol.NewMessageWithSession(protocol.MsgSessionHistory, deviceID, msg.SessionID)
			out.Payload = map[string]any{
				"source":    "server_db",
				"truncated": truncated,
				"limit":     limit,
				"messages":  msgs,
			}
			s.connMgr.BroadcastToPhones(deviceID, out)
		}

	case protocol.MsgSendPrompt:
		msg.DeviceID = deviceID
		promptText := ""
		if msg.Payload == nil {
			msg.Payload = map[string]any{}
		}
		if p, ok := msg.Payload["prompt"].(string); ok {
			promptText = p
		}
		atts := normalizeAttachments(msg.Payload["attachments"])
		if promptText == "" && len(atts) == 0 {
			s.broadcastPromptFailure(deviceID, msg.SessionID, stringPayload(msg.Payload, "client_msg_id"), "empty prompt")
			return
		}
		if msg.SessionID == "" {
			s.broadcastPromptFailure(deviceID, "", stringPayload(msg.Payload, "client_msg_id"), "session_id required")
			return
		}
		if promptText == "" {
			promptText = fmt.Sprintf("(sent %d attachment(s))", len(atts))
			msg.Payload["prompt"] = promptText
		}
		if len(atts) > 0 {
			msg.Payload["attachments"] = atts
		}

		rawClientMsgID := stringPayload(msg.Payload, "client_msg_id")
		clientMsgID := sanitizeClientMsgID(rawClientMsgID)
		if rawClientMsgID != "" && clientMsgID == "" {
			s.broadcastPromptFailure(deviceID, msg.SessionID, rawClientMsgID, "invalid client_msg_id")
			return
		}
		if clientMsgID == "" {
			randomID, err := randomHex(16)
			if err != nil {
				s.broadcastPromptFailure(deviceID, msg.SessionID, "", "failed to allocate prompt id")
				return
			}
			clientMsgID = "msg_" + randomID
		}
		msg.Payload["client_msg_id"] = clientMsgID

		attachmentsJSON, err := json.Marshal(atts)
		if err != nil {
			s.broadcastPromptFailure(deviceID, msg.SessionID, clientMsgID, "invalid attachments")
			return
		}
		retryFailed, _ := msg.Payload["retry"].(bool)
		cmd, shouldForward, err := s.db.RegisterPromptCommand(&db.PromptCommand{
			DeviceID:        deviceID,
			ClientMsgID:     clientMsgID,
			SessionID:       msg.SessionID,
			Prompt:          promptText,
			AttachmentsJSON: string(attachmentsJSON),
		}, retryFailed)
		if err != nil {
			message := "failed to register prompt"
			if errors.Is(err, db.ErrPromptCommandConflict) {
				message = "client_msg_id already used for another prompt"
			}
			s.broadcastPromptFailure(deviceID, msg.SessionID, clientMsgID, message)
			return
		}
		if !shouldForward {
			if cmd.Status == db.PromptAccepted {
				// Also heals a crash after acceptance but before message save.
				if err := s.persistAcceptedPrompt(cmd); err != nil {
					log.Printf("[ws] heal accepted user prompt: %v", err)
					return
				}
				s.sendPromptCommitted(cmd)
				s.broadcastPromptSent(cmd)
			} else if cmd.Status == db.PromptFailed || cmd.Status == db.PromptIndeterminate {
				s.broadcastPromptFailed(cmd)
			} else if cmd.Status == db.PromptPending {
				// Do not replay an ambiguously delivered prompt. Query the
				// daemon's acceptance cache to recover a lost ACK instead.
				query := protocol.NewMessageWithSession(
					protocol.MsgPromptStatusQuery,
					cmd.DeviceID,
					cmd.SessionID,
				)
				query.Payload = map[string]any{"client_msg_id": cmd.ClientMsgID}
				if err := s.connMgr.SendToDaemon(deviceID, query); err != nil {
					// Keep pending; a later outbox flush can query again.
					log.Printf("[ws] prompt status query %s/%s: %v", deviceID, clientMsgID, err)
				}
			}
			return
		}

		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			outcome := "transport_error"
			retryAllowed := true
			failureMessage := "device offline"
			if !errors.Is(err, ErrDeviceOffline) {
				// A websocket write can report an error after the complete
				// frame reached the daemon. Never make that ambiguous command
				// retryable unless the daemon later proves it was rejected.
				outcome = db.PromptIndeterminate
				retryAllowed = false
				failureMessage = "prompt delivery outcome is indeterminate"
			}
			failed, _, markErr := s.db.MarkPromptFailed(
				deviceID,
				clientMsgID,
				failureMessage,
				outcome,
				retryAllowed,
			)
			if markErr != nil {
				log.Printf("[ws] mark prompt forward failure: %v", markErr)
				failed = cmd
				failed.Status = db.PromptFailed
				if outcome == db.PromptIndeterminate {
					failed.Status = db.PromptIndeterminate
				}
				failed.Error = failureMessage
				failed.Outcome = outcome
				failed.RetryAllowed = retryAllowed
			}
			if failed.Status == db.PromptAccepted {
				if err := s.persistAcceptedPrompt(failed); err != nil {
					log.Printf("[ws] save concurrently accepted user prompt: %v", err)
				} else {
					s.broadcastPromptSent(failed)
				}
			} else {
				s.broadcastPromptFailed(failed)
			}
		} else {
			if _, _, err := s.db.MarkPromptForwarded(deviceID, clientMsgID); err != nil {
				// Leave registered on persistence failure. Replaying the same
				// ID is safe against the daemon's durable execution journal.
				log.Printf("[ws] mark prompt forwarded %s/%s: %v", deviceID, clientMsgID, err)
			}
		}

	case protocol.MsgApprove, protocol.MsgDeny:
		msg.DeviceID = deviceID
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, msg.SessionID)
			errMsg.Payload = map[string]any{"message": "device offline"}
			s.connMgr.BroadcastToPhones(deviceID, errMsg)
		}

	case protocol.MsgInterrupt:
		msg.DeviceID = deviceID
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, msg.SessionID)
			errMsg.Payload = map[string]any{"message": "device offline"}
			s.connMgr.BroadcastToPhones(deviceID, errMsg)
		}

	case protocol.MsgSteer:
		msg.DeviceID = deviceID
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, msg.SessionID)
			errMsg.Payload = map[string]any{"message": "device offline"}
			s.connMgr.BroadcastToPhones(deviceID, errMsg)
		}

	case protocol.MsgStartThread:
		// Codex-only spawn into discovered project dirs (daemon enforces policy).
		msg.DeviceID = deviceID
		if msg.Payload == nil {
			msg.Payload = map[string]any{}
		}
		opID := stringPayload(msg.Payload, "operation_id")
		if opID == "" {
			opID = sanitizeClientMsgID(msg.ClientMsgID)
		}
		if opID == "" {
			if id, err := randomHex(12); err == nil {
				opID = "local_start_" + id
			}
		}
		msg.Payload["operation_id"] = opID
		msg.ClientMsgID = opID
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			fail := s.stampEnvelope(protocol.NewMessage(protocol.MsgThreadFailed, deviceID))
			fail.Payload = map[string]any{
				"operation_id": opID,
				"error":        "device offline",
				"message":      "device offline",
			}
			s.connMgr.BroadcastToPhones(deviceID, fail)
		}

	default:
		log.Printf("[ws] unexpected message type from phone: %s", msg.Type)
	}
}

func historyLimit(payload map[string]any) int {
	const defaultLimit = 50
	if payload == nil {
		return defaultLimit
	}
	raw, ok := payload["limit"]
	if !ok {
		return defaultLimit
	}
	var value float64
	switch n := raw.(type) {
	case float64:
		value = n
	case float32:
		value = float64(n)
	case int:
		value = float64(n)
	case int64:
		value = float64(n)
	default:
		return defaultLimit
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
		return defaultLimit
	}
	if value < 1 {
		return 1
	}
	if value > 500 {
		return 500
	}
	return int(value)
}

func truncateHistory(
	rows []*protocol.SessionMessage,
	limit int,
) ([]*protocol.SessionMessage, bool) {
	if limit < 1 || len(rows) <= limit {
		return rows, false
	}
	// GetMessages returns chronological order. Keep the newest requested rows.
	return rows[len(rows)-limit:], true
}

func needsApprovalPush(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if explicitApprovalValue(payload["pending_approval"]) ||
		explicitApprovalValue(payload["permission_request"]) ||
		isWaitingApprovalStatus(payload["status"]) {
		return true
	}
	for _, key := range []string{"session", "message"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if explicitApprovalValue(nested["pending_approval"]) ||
			explicitApprovalValue(nested["permission_request"]) {
			return true
		}
		if key == "session" && isWaitingApprovalStatus(nested["status"]) {
			return true
		}
		if kind, _ := nested["type"].(string); kind == "permission_request" {
			return true
		}
		if metadata, ok := nested["metadata"].(map[string]any); ok {
			if explicitApprovalValue(metadata["pending_approval"]) ||
				explicitApprovalValue(metadata["permission_request"]) {
				return true
			}
		}
	}
	return false
}

func isWaitingApprovalStatus(value any) bool {
	status, ok := value.(string)
	return ok && strings.TrimSpace(status) == string(protocol.AgentWaitingApproval)
}

func explicitApprovalValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func promptClientMsgID(msg *protocol.NekoMessage) string {
	if msg == nil {
		return ""
	}
	return sanitizeClientMsgID(stringPayload(msg.Payload, "client_msg_id"))
}

func stringPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func promptAttachments(cmd *db.PromptCommand) []map[string]any {
	if cmd == nil || cmd.AttachmentsJSON == "" {
		return nil
	}
	var attachments []map[string]any
	if err := json.Unmarshal([]byte(cmd.AttachmentsJSON), &attachments); err != nil {
		log.Printf("[ws] decode prompt attachments %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
		return nil
	}
	return attachments
}

func promptCommandMessage(cmd *db.PromptCommand) *protocol.NekoMessage {
	msg := protocol.NewMessageWithSession(protocol.MsgSendPrompt, cmd.DeviceID, cmd.SessionID)
	msg.Payload = map[string]any{
		"prompt":        cmd.Prompt,
		"client_msg_id": cmd.ClientMsgID,
	}
	if attachments := promptAttachments(cmd); len(attachments) > 0 {
		msg.Payload["attachments"] = attachments
	}
	return msg
}

func (s *Server) persistAcceptedPrompt(cmd *db.PromptCommand) error {
	if cmd == nil {
		return errors.New("nil accepted prompt")
	}
	attachments := promptAttachments(cmd)
	userMsg := protocol.SessionMessage{
		ID:        cmd.ClientMsgID,
		Role:      "user",
		Content:   cmd.Prompt,
		Type:      "text",
		Timestamp: cmd.CreatedAt,
	}
	if len(attachments) > 0 {
		userMsg.Metadata = map[string]any{"attachments": attachments}
	}
	// The daemon has already accepted this command, so callers must not convert
	// an error here into a retryable execution failure. They leave the daemon
	// journal uncommitted and let a later replay heal the database instead.
	return s.db.SaveMessage(cmd.DeviceID, cmd.SessionID, &userMsg)
}

func (s *Server) broadcastPromptSent(cmd *db.PromptCommand) {
	if cmd == nil {
		return
	}
	ack := protocol.NewMessageWithSession(protocol.MsgPromptSent, cmd.DeviceID, cmd.SessionID)
	ack.Payload = map[string]any{
		"client_msg_id": cmd.ClientMsgID,
		"message_id":    cmd.ClientMsgID,
		"prompt":        cmd.Prompt,
		"attachments":   promptAttachments(cmd),
	}
	s.connMgr.BroadcastToPhones(cmd.DeviceID, ack)
}

func promptCommittedMessage(cmd *db.PromptCommand) *protocol.NekoMessage {
	msg := protocol.NewMessageWithSession(
		protocol.MsgPromptCommitted,
		cmd.DeviceID,
		cmd.SessionID,
	)
	msg.Payload = map[string]any{"client_msg_id": cmd.ClientMsgID}
	return msg
}

func (s *Server) sendPromptCommittedLocked(dc *DaemonConn, cmd *db.PromptCommand) {
	if err := s.connMgr.sendToDaemonLocked(dc, promptCommittedMessage(cmd)); err != nil {
		log.Printf("[ws] send prompt_committed %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
		return
	}
	if err := s.db.MarkPromptCommitted(cmd.DeviceID, cmd.ClientMsgID); err != nil {
		log.Printf("[ws] persist prompt_committed %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
	}
}

func (s *Server) sendPromptCommitted(cmd *db.PromptCommand) {
	if err := s.connMgr.SendToDaemon(cmd.DeviceID, promptCommittedMessage(cmd)); err != nil {
		log.Printf("[ws] resend prompt_committed %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
		return
	}
	if err := s.db.MarkPromptCommitted(cmd.DeviceID, cmd.ClientMsgID); err != nil {
		log.Printf("[ws] persist prompt_committed %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
	}
}

func (s *Server) replayPromptCommits(dc *DaemonConn) {
	for {
		commands, err := s.db.ListUncommittedAcceptedPrompts(dc.DeviceID, 100)
		if err != nil {
			log.Printf("[ws] list uncommitted prompts for %s: %v", dc.DeviceID, err)
			return
		}
		if len(commands) == 0 {
			return
		}
		for _, cmd := range commands {
			// An accepted DB row can survive a crash that happened before its
			// user-facing history row was written. Heal that first; only then
			// tell the daemon it may discard its durable execution journal.
			if err := s.persistAcceptedPrompt(cmd); err != nil {
				log.Printf("[ws] heal accepted prompt before commit %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
				return
			}
			if err := s.connMgr.SendToDaemon(
				dc.DeviceID,
				promptCommittedMessage(cmd),
			); err != nil {
				log.Printf("[ws] replay prompt_committed %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
				return
			}
			if err := s.db.MarkPromptCommitted(cmd.DeviceID, cmd.ClientMsgID); err != nil {
				log.Printf("[ws] persist replayed prompt_committed %s/%s: %v", cmd.DeviceID, cmd.ClientMsgID, err)
				return
			}
		}
	}
}

func (s *Server) broadcastPromptFailure(deviceID, sessionID, clientMsgID, message string) {
	s.broadcastPromptFailed(&db.PromptCommand{
		DeviceID:     deviceID,
		ClientMsgID:  strings.TrimSpace(clientMsgID),
		SessionID:    sessionID,
		Status:       db.PromptFailed,
		Error:        message,
		Outcome:      "validation_error",
		RetryAllowed: false,
	})
}

func (s *Server) broadcastPromptFailed(cmd *db.PromptCommand) {
	if cmd == nil {
		return
	}
	message := strings.TrimSpace(cmd.Error)
	if message == "" {
		message = "prompt failed"
	}
	failed := protocol.NewMessageWithSession(protocol.MsgPromptFailed, cmd.DeviceID, cmd.SessionID)
	failed.Payload = map[string]any{
		"client_msg_id": cmd.ClientMsgID,
		"message":       message,
		"error":         message,
		"outcome":       cmd.Outcome,
		"retry_allowed": cmd.RetryAllowed && cmd.Status == db.PromptFailed,
	}
	s.connMgr.BroadcastToPhones(cmd.DeviceID, failed)
}

// sendPushNotification sends a web push notification (requires VAPID env keys).
func (s *Server) sendPushNotification(deviceID, sessionID, title, body string) {
	subs, err := s.db.GetPushSubscriptions(deviceID)
	if err != nil || len(subs) == 0 {
		return
	}
	url := "/"
	if sessionID != "" {
		url = "/device/" + deviceID + "/session/" + sessionID
	}
	payload := make([]pushsub.Subscription, 0, len(subs))
	for _, sub := range subs {
		payload = append(payload, pushsub.Subscription{
			Endpoint: sub.Endpoint,
			P256DH:   sub.P256DH,
			Auth:     sub.Auth,
		})
	}
	log.Printf("[push] device=%s session=%s title=%q subs=%d", deviceID, sessionID, title, len(payload))
	pushsub.Send(payload, title, body, url, deviceID, sessionID, func(endpoint string) {
		if err := s.db.DeletePushSubscription(endpoint); err != nil {
			log.Printf("[push] delete expired endpoint: %v", err)
			return
		}
		log.Printf("[push] deleted expired endpoint %s", endpoint)
	})
}

// CORSMiddleware adds CORS headers.
// When NEKONEST_ALLOWED_ORIGINS is set, only listed origins are accepted.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" {
			if !isAllowedOrigin(r, origin) {
				log.Printf("[cors] rejected origin: %s", origin)
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Neko-Secret")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin checks Origin against NEKONEST_ALLOWED_ORIGINS
// (comma-separated). Without an explicit list, browser requests must be
// same-origin; local development is not an implicit cross-origin wildcard.
func isAllowedOrigin(r *http.Request, origin string) bool {
	raw := strings.TrimSpace(os.Getenv("NEKONEST_ALLOWED_ORIGINS"))
	if raw == "" {
		return isSameOrigin(r, origin)
	}
	if raw == "*" {
		return true
	}
	for _, o := range strings.Split(raw, ",") {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

func isSameOrigin(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if trustedProxyRequest(r) {
		parts := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			forwarded := strings.TrimSpace(parts[i])
			if forwarded == "http" || forwarded == "https" {
				scheme = forwarded
				break
			}
		}
	}
	if u.Scheme != scheme {
		return false
	}
	return equalOriginHost(u.Host, r.Host, scheme)
}

func equalOriginHost(a, b, scheme string) bool {
	normalize := func(hostport string) (string, string) {
		host := hostport
		port := ""
		if h, p, err := net.SplitHostPort(hostport); err == nil {
			host, port = h, p
		} else {
			host = strings.Trim(hostport, "[]")
		}
		if port == "" {
			if scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		return strings.ToLower(host), port
	}
	ah, ap := normalize(a)
	bh, bp := normalize(b)
	return ah == bh && ap == bp
}

// normalizeAttachments extracts attachment refs from send_prompt payload.
// Only relative /api/attachments/{id} URLs (optional ?k=) are accepted — blocks SSRF via daemon fetch.
func normalizeAttachments(raw any) []map[string]any {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for i, item := range arr {
		if i >= 5 {
			break
		}
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rawURL, _ := m["url"].(string)
		safe := sanitizeAttachmentURL(rawURL)
		if safe == "" {
			continue
		}
		name, _ := m["name"].(string)
		mime, _ := m["mime"].(string)
		id, _ := m["id"].(string)
		entry := map[string]any{"url": safe}
		if name != "" {
			entry["name"] = name
		}
		if mime != "" {
			entry["mime"] = mime
		}
		if id != "" {
			entry["id"] = id
		}
		if sz, ok := m["size"].(float64); ok {
			entry["size"] = int64(sz)
		}
		out = append(out, entry)
	}
	return out
}

// sanitizeAttachmentURL allows only /api/attachments/{hex}?k=... (relative or same-path absolute stripped).
func sanitizeAttachmentURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Reject scheme-relative and obvious SSRF bait early.
	if strings.HasPrefix(raw, "//") {
		return ""
	}
	path := raw
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		path = u.RequestURI()
	}
	if !strings.HasPrefix(path, "/api/attachments/") {
		return ""
	}
	u, err := url.Parse(path)
	if err != nil {
		return ""
	}
	rest := strings.TrimPrefix(u.EscapedPath(), "/api/attachments/")
	if rest == "" || strings.Contains(rest, "/") || strings.Contains(rest, "..") {
		return ""
	}
	// Keep only id path + optional k query.
	out := "/api/attachments/" + rest
	if k := u.Query().Get("k"); k != "" {
		// key is hex from our uploader
		for _, c := range k {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return ""
			}
		}
		out += "?k=" + k
	}
	return out
}

// sanitizeClientMsgID allows phone-generated ids (local_*/user_*) for history alignment.
func sanitizeClientMsgID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) < 4 || len(id) > 80 {
		return ""
	}
	if !strings.HasPrefix(id, "local_") && !strings.HasPrefix(id, "user_") && !strings.HasPrefix(id, "msg_") {
		return ""
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return ""
	}
	return id
}

// LoggingMiddleware logs HTTP requests.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)

		// Skip logging for static files and health checks
		if !strings.HasPrefix(r.URL.Path, "/ws/") && r.URL.Path != "/health" {
			log.Printf("[http] %s %s %v", r.Method, r.URL.Path, duration)
		}
	})
}
