package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
	"github.com/nekonest/server/internal/push"
)

// Server is the main NekoNest server.
type Server struct {
	db          *db.DB
	connMgr     *ConnectionManager
	phoneSecret string // if non-empty, required for phone REST + phone WS
	dataDir     string // SQLite + attachments root
}

// New creates a new NekoNest server.
func New(database *db.DB) *Server {
	return NewWithSecret(database, "")
}

// NewWithSecret creates a server with an optional phone shared secret.
func NewWithSecret(database *db.DB, phoneSecret string) *Server {
	s := &Server{
		db:          database,
		connMgr:     NewConnectionManager(database),
		phoneSecret: phoneSecret,
	}

	// Set up device online/offline callbacks
	s.connMgr.OnDeviceUp(func(deviceID string) {
		msg := protocol.NewMessage(protocol.MsgDeviceOnline, deviceID)
		msg.Payload = map[string]any{"device_id": deviceID}
		s.connMgr.BroadcastToPhones(deviceID, msg)
	})

	s.connMgr.OnDeviceDown(func(deviceID string) {
		msg := protocol.NewMessage(protocol.MsgDeviceOffline, deviceID)
		msg.Payload = map[string]any{"device_id": deviceID}
		s.connMgr.BroadcastToPhones(deviceID, msg)
	})

	return s
}

// SetDataDir sets the root directory for attachments (and related files).
func (s *Server) SetDataDir(dir string) {
	s.dataDir = dir
}

// RegisterRoutes sets up HTTP routes.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/daemon", s.HandleDaemonWS)
	mux.HandleFunc("/ws/phone", s.HandlePhoneWS)

	// Device APIs
	mux.HandleFunc("/api/devices", s.handleListDevices)
	mux.HandleFunc("/api/devices/register", s.handleRegisterDevice)
	mux.HandleFunc("/api/devices/sessions", s.handleDeviceSessions)

	// P2-A: Message history API
	mux.HandleFunc("/api/messages", s.handleMessages)

	// Attachments (upload + download)
	mux.HandleFunc("/api/attachments", s.handleAttachments)
	mux.HandleFunc("/api/attachments/", s.handleAttachments)

	// P2-C: Push subscription API
	mux.HandleFunc("/api/push/subscribe", s.handlePushSubscribe)
	mux.HandleFunc("/api/push/vapid-public-key", s.handleVAPIDPublicKey)

	// Pairing APIs
	mux.HandleFunc("/api/pair/generate", s.handleGeneratePairCode)
	mux.HandleFunc("/api/pair/consume", s.handleConsumePairCode)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"nyan~"}`))
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requirePhoneAuth(w, r) {
		return
	}

	devices, err := s.db.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update online status
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"devices": devices})
}

func (s *Server) handleDeviceSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requirePhoneAuth(w, r) {
		return
	}

	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
		return
	}

	sessions := s.connMgr.GetDeviceSessions(deviceID)
	if sessions == nil {
		sessions = []*protocol.AgentSession{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}

// P2-A: Message history endpoint
// GET /api/messages?device_id=xxx&session_id=yyy&limit=100
// (auth required when NEKONEST_PHONE_SECRET is set)
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if !s.requirePhoneAuth(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		deviceID := r.URL.Query().Get("device_id")
		sessionID := r.URL.Query().Get("session_id")
		if deviceID == "" || sessionID == "" {
			http.Error(w, "device_id and session_id required", http.StatusBadRequest)
			return
		}

		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		messages, err := s.db.GetMessages(deviceID, sessionID, limit+1)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if messages == nil {
			messages = []*protocol.SessionMessage{}
		}
		messages, truncated := truncateHistory(messages, limit)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"messages":  messages,
			"limit":     limit,
			"truncated": truncated,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// P2-C: Push subscription endpoint
// POST /api/push/subscribe { device_id, endpoint, p256dh, auth }
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requirePhoneAuth(w, r) {
		return
	}

	var req struct {
		DeviceID string `json:"device_id"`
		Endpoint string `json:"endpoint"`
		P256DH   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.P256DH = strings.TrimSpace(req.P256DH)
	req.Auth = strings.TrimSpace(req.Auth)
	if req.DeviceID == "" || req.Endpoint == "" || req.P256DH == "" || req.Auth == "" {
		http.Error(w, "device_id, endpoint, p256dh and auth required", http.StatusBadRequest)
		return
	}
	if len(req.DeviceID) > 256 || len(req.Endpoint) > 4096 ||
		len(req.P256DH) > 512 || len(req.Auth) > 256 {
		http.Error(w, "push subscription field too long", http.StatusBadRequest)
		return
	}
	if !s.db.DeviceExists(req.DeviceID) {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	if err := push.ValidateEndpoint(req.Endpoint); err != nil {
		http.Error(w, "invalid push endpoint", http.StatusBadRequest)
		return
	}
	if err := push.ValidateKeys(req.P256DH, req.Auth); err != nil {
		http.Error(w, "invalid push subscription keys", http.StatusBadRequest)
		return
	}

	sub := &db.PushSubscription{
		DeviceID: req.DeviceID,
		Endpoint: req.Endpoint,
		P256DH:   req.P256DH,
		Auth:     req.Auth,
	}
	if err := s.db.SavePushSubscription(sub); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[push] subscription registered for device %s", req.DeviceID)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{"status": "subscribed"})
}

// GET /api/push/vapid-public-key — public key for PushManager.subscribe
func (s *Server) handleVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requirePhoneAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !push.Enabled() {
		writeJSON(w, map[string]any{"enabled": false, "public_key": ""})
		return
	}
	writeJSON(w, map[string]any{"enabled": true, "public_key": push.PublicKey()})
}

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// When phone secret is configured (production-like), require bootstrap token
	// so /api/devices/register is not open to the internet.
	bootstrap := strings.TrimSpace(os.Getenv("NEKONEST_BOOTSTRAP_TOKEN"))
	if s.phoneSecret != "" && bootstrap == "" {
		http.Error(w, "server misconfigured: set NEKONEST_BOOTSTRAP_TOKEN", http.StatusServiceUnavailable)
		return
	}
	if bootstrap != "" {
		got := r.Header.Get("X-Neko-Bootstrap")
		if got == "" {
			got = r.URL.Query().Get("bootstrap")
		}
		if got != bootstrap {
			http.Error(w, "bootstrap token required", http.StatusUnauthorized)
			return
		}
	}

	var req struct {
		DeviceID string `json:"device_id"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = "Windows PC"
	}
	if req.DeviceID == "" {
		b := make([]byte, 8)
		rand.Read(b)
		req.DeviceID = "device_" + hex.EncodeToString(b)
	}

	token, err := s.db.RegisterDevice(req.DeviceID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[register] device %s (%s)", req.DeviceID, req.Name)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"device_id": req.DeviceID,
		"token":     token,
		"name":      req.Name,
	})
}

func (s *Server) handleGeneratePairCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DeviceID string `json:"device_id"`
		Token    string `json:"token"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
		return
	}
	// Pair codes mint device binding capability — require the device's own token.
	if req.Token == "" || !s.db.ValidateDeviceToken(req.DeviceID, req.Token) {
		http.Error(w, "invalid device token", http.StatusUnauthorized)
		return
	}

	// Generate 6-char pairing code
	b := make([]byte, 3)
	rand.Read(b)
	code := hex.EncodeToString(b)[:6]

	expiresAt := time.Now().Add(5 * time.Minute)
	if err := s.db.CreatePairCode(code, req.DeviceID, expiresAt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[pair] code generated: %s for device %s", code, req.DeviceID)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{
		"code":       code,
		"expires_at": expiresAt.Unix(),
	})
}

func (s *Server) handleConsumePairCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requirePhoneAuth(w, r) {
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	deviceID, err := s.db.ConsumePairCode(req.Code)
	if err != nil {
		http.Error(w, "invalid or expired code", http.StatusNotFound)
		return
	}

	// Look up device name for the phone UI
	name := deviceID
	if d, err := s.db.GetDevice(deviceID); err == nil && d != nil {
		name = d.Name
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"device_id": deviceID,
		"name":      name,
		"status":    "paired",
	})
}

// requirePhoneAuth checks the phone shared secret when configured.
// Accepts Authorization: Bearer <secret> or X-Neko-Secret: <secret>.
func (s *Server) requirePhoneAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.phoneSecret == "" {
		return true
	}
	if phoneSecretFromRequest(r) == s.phoneSecret {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func phoneSecretFromRequest(r *http.Request) string {
	if h := r.Header.Get("X-Neko-Secret"); h != "" {
		return h
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if q := r.URL.Query().Get("secret"); q != "" {
		return q
	}
	return ""
}
