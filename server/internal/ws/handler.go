package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/server/internal/protocol"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (daemon)
		}
		return isAllowedOrigin(origin)
	},
}

const (
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// rateLimiter provides simple per-IP rate limiting.
type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Clean old entries
	times := rl.requests[key]
	var valid []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[key] = valid
		return false
	}

	rl.requests[key] = append(valid, now)
	return true
}

// Global rate limiter: 60 requests per minute per IP
var wsRateLimiter = newRateLimiter(60, time.Minute)

// HandleDaemonWS handles WebSocket connections from Windows daemons.
func (s *Server) HandleDaemonWS(w http.ResponseWriter, r *http.Request) {
	// P2-F: Rate limiting
	clientIP := r.RemoteAddr
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

	// First message must be auth
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	var authMsg protocol.NekoMessage
	if err := json.Unmarshal(data, &authMsg); err != nil || authMsg.Type != protocol.MsgRegisterDevice {
		conn.WriteJSON(protocol.NekoMessage{Type: protocol.MsgError, Payload: map[string]any{"message": "expected auth message"}})
		conn.Close()
		return
	}

	deviceID, _ := authMsg.Payload["device_id"].(string)
	token, _ := authMsg.Payload["token"].(string)

	if !s.db.ValidateDeviceToken(deviceID, token) {
		conn.WriteJSON(protocol.NekoMessage{Type: protocol.MsgError, Payload: map[string]any{"message": "invalid token"}})
		conn.Close()
		return
	}

	// Send auth success
	conn.WriteJSON(protocol.NekoMessage{
		Type:      protocol.MsgAuthResponse,
		DeviceID:  deviceID,
		Timestamp: time.Now().Unix(),
		Payload:   map[string]any{"status": "authenticated"},
	})

	// Set read deadline + pong handler for daemon connection
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	dc := s.connMgr.AddDaemon(deviceID, conn)

	// Start heartbeat
	go s.daemonPingLoop(dc)

	// Read loop
	s.daemonReadLoop(dc)
}

func (s *Server) daemonReadLoop(dc *DaemonConn) {
	defer func() {
		dc.Conn.Close()
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
			if sess.DeviceID == "" {
				sess.DeviceID = dc.DeviceID
			}
		}
		s.connMgr.UpdateSessions(dc.DeviceID, sessions)

		// Also persist to database for offline querying
		s.db.UpdateDeviceSessions(dc.DeviceID, len(sessions))

	case protocol.MsgSessionUpdate:
		// Single session update (status change, new approval, etc.)
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

	case protocol.MsgSessionMessage:
		// P2-A: Persist message to database
		if msgData, ok := msg.Payload["message"]; ok {
			data, _ := json.Marshal(msgData)
			var sessionMsg protocol.SessionMessage
			if err := json.Unmarshal(data, &sessionMsg); err == nil {
				if err := s.db.SaveMessage(dc.DeviceID, msg.SessionID, &sessionMsg); err != nil {
					log.Printf("[ws] save message error: %v", err)
				}
			}
		}
		// Forward to all phones
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

		// P2-C: Trigger push notification if session needs approval
		// (checked via session_update, but session_message with tool_call type also triggers)
		if msgData, ok := msg.Payload["message"]; ok {
			if m, ok := msgData.(map[string]interface{}); ok {
				if msgType, _ := m["type"].(string); msgType == "tool_call" {
					s.sendPushNotification(dc.DeviceID, msg.SessionID, "⚠️ 工具调用需要审批", "点击查看详情")
				}
			}
		}

	case protocol.MsgSessionCreated:
		// Daemon confirmed new session creation — forward to phones
		s.connMgr.BroadcastToPhones(dc.DeviceID, msg)

	case protocol.MsgError:
		// Daemon-side errors (send_prompt failed, etc.) → phones
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
				dc.Conn.Close()
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
	// P2-F: Rate limiting
	clientIP := r.RemoteAddr
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

	// First message: subscribe { type, device_id, payload.secret? }
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}

	var subscribeMsg protocol.NekoMessage
	if err := json.Unmarshal(data, &subscribeMsg); err != nil {
		conn.WriteJSON(protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "invalid json"},
		})
		conn.Close()
		return
	}

	// Phone auth: secret from query/header or first-message payload
	if s.phoneSecret != "" {
		secret := phoneSecretFromRequest(r)
		if secret == "" && subscribeMsg.Payload != nil {
			if p, ok := subscribeMsg.Payload["secret"].(string); ok {
				secret = p
			}
		}
		if secret != s.phoneSecret {
			conn.WriteJSON(protocol.NekoMessage{
				Type:      protocol.MsgError,
				Timestamp: time.Now().Unix(),
				Payload:   map[string]any{"message": "unauthorized"},
			})
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
		conn.WriteJSON(protocol.NekoMessage{
			Type:      protocol.MsgError,
			Timestamp: time.Now().Unix(),
			Payload:   map[string]any{"message": "device_id required in first message (type=subscribe)"},
		})
		conn.Close()
		return
	}

	s.connMgr.AddPhone(deviceID, conn)
	currentDevice := deviceID
	defer func() {
		s.connMgr.RemovePhone(currentDevice, conn)
	}()

	s.pushPhoneSnapshot(conn, deviceID)

	// Set read deadline + pong handler for phone connection
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Read loop for phone commands
	s.phoneReadLoop(conn, &currentDevice)
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

func (s *Server) phoneReadLoop(conn *websocket.Conn, deviceID *string) {
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg protocol.NekoMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			// Allow switching subscribed device mid-connection
			if msg.Type == protocol.MsgSubscribe || (msg.Type == protocol.MsgHeartbeat && msg.DeviceID != "" && msg.DeviceID != *deviceID) {
				newID := msg.DeviceID
				if newID == "" && msg.Payload != nil {
					if id, ok := msg.Payload["device_id"].(string); ok {
						newID = id
					}
				}
				if newID != "" && newID != *deviceID {
					s.connMgr.ResubscribePhone(newID, conn)
					*deviceID = newID
					s.pushPhoneSnapshot(conn, newID)
					continue
				}
			}

			s.handlePhoneMessage(*deviceID, &msg)
		}
	}()

	for {
		select {
		case <-pingTicker.C:
			if err := s.connMgr.SafeWritePing(conn); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func (s *Server) handlePhoneMessage(deviceID string, msg *protocol.NekoMessage) {
	switch msg.Type {
	case protocol.MsgSubscribe, protocol.MsgHeartbeat:
		// Subscription handled in read loop; heartbeat is keep-alive
		return

	case protocol.MsgSendPrompt:
		// Forward prompt to daemon
		msg.DeviceID = deviceID
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, msg.SessionID)
			errMsg.Payload = map[string]any{"message": "device offline"}
			s.connMgr.BroadcastToPhones(deviceID, errMsg)
		} else {
			// Send ack back to phone
			ack := protocol.NewMessageWithSession(protocol.MsgPromptSent, deviceID, msg.SessionID)
			ack.Payload = map[string]any{"prompt": ""}
			if p, ok := msg.Payload["prompt"].(string); ok {
				ack.Payload = map[string]any{"prompt": p}
			}
			s.connMgr.BroadcastToPhones(deviceID, ack)
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

	case protocol.MsgCreateSession:
		// P2-B: Forward create session request to daemon
		msg.DeviceID = deviceID
		if err := s.connMgr.SendToDaemon(deviceID, msg); err != nil {
			errMsg := protocol.NewMessageWithSession(protocol.MsgError, deviceID, "")
			errMsg.Payload = map[string]any{"message": "device offline"}
			s.connMgr.BroadcastToPhones(deviceID, errMsg)
		}

	default:
		log.Printf("[ws] unexpected message type from phone: %s", msg.Type)
	}
}

// sendPushNotification sends a web push notification.
func (s *Server) sendPushNotification(deviceID, sessionID, title, body string) {
	subs, err := s.db.GetPushSubscriptions(deviceID)
	if err != nil || len(subs) == 0 {
		return
	}

	// In a full implementation, this would use the web-push library to send notifications
	// For MVP, we log the intent — actual push requires VAPID keys and HTTP client
	log.Printf("[push] notification for device=%s session=%s: %s - %s (to %d subscribers)",
		deviceID, sessionID, title, body, len(subs))

	// TODO: Implement actual Web Push sending with VAPID signing
	// This requires: github.com/SherClockHolmes/webpush-go package
	// and VAPID key pair generation
}

// CORSMiddleware adds CORS headers.
// When NEKONEST_ALLOWED_ORIGINS is set, only listed origins are accepted.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" {
			if !isAllowedOrigin(origin) {
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

// isAllowedOrigin checks Origin against NEKONEST_ALLOWED_ORIGINS (comma-separated).
// Empty env = allow all (local dev).
func isAllowedOrigin(origin string) bool {
	raw := strings.TrimSpace(os.Getenv("NEKONEST_ALLOWED_ORIGINS"))
	if raw == "" || raw == "*" {
		return true
	}
	for _, o := range strings.Split(raw, ",") {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
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
