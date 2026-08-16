package relaycore

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/klarkxy/nekonest/relaycore/internal/opslog"
	"github.com/klarkxy/nekonest/relaycore/protocol"
	db "github.com/klarkxy/nekonest/relaycore/store"
	push "github.com/klarkxy/nekonest/relaycore/webpush"
)

// DuplicateDaemonPolicy controls how a second live connection using the same
// device identity is handled. Standalone deployments preserve their historic
// replacement behavior; shared relay deployments use RejectNew so one identity
// cannot occupy two concurrent machines.
type DuplicateDaemonPolicy uint8

const (
	ReplaceExisting DuplicateDaemonPolicy = iota
	RejectNew
)

// Config contains only single-nest relay concerns. Placement and admission
// policy belong to the deployment shell.
type Config struct {
	Store           db.Store
	Ports           Ports
	PhoneSecret     string
	DataDir         string
	AppVersion      string
	TransportMode   protocol.TransportMode
	DuplicateDaemon DuplicateDaemonPolicy
	AllowedOrigins  []string
}

// Engine owns the data plane for exactly one nest.
type Engine struct {
	db             db.Store
	connMgr        *ConnectionManager
	phoneSecret    string // if non-empty, required for phone REST + phone WS
	dataDir        string // SQLite + attachments root
	transportMode  protocol.TransportMode
	appVersion     string
	allowedOrigins map[string]struct{}
	attachments    AttachmentStore
	attachmentURLs AttachmentURLBuilder
	pushSink       PushSink
	audit          AuditSink
	clock          Clock
	synchronizer   PrincipalSynchronizer
}

func (s *Engine) auditEvent(component, name, message string, err error, attributes ...any) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.Emit(context.Background(), AuditEvent{Component: component, Name: name, Message: message, Err: err, Attributes: attributes})
}

// Server is kept as a source-compatible name for the standalone wrapper.
type Server = Engine

// New creates a new NekoNest server.
func New(database db.Store) *Engine {
	return NewWithSecret(database, "")
}

// NewWithSecret creates a server with an optional phone shared secret.
func NewWithSecret(database db.Store, phoneSecret string) *Engine {
	engine, err := NewEngine(Config{Store: database, PhoneSecret: phoneSecret})
	if err != nil {
		panic(err)
	}
	return engine
}

// NewEngine constructs one isolated relay engine.
func NewEngine(config Config) (*Engine, error) {
	store := config.Store
	if store == nil {
		p := config.Ports
		if p.PrincipalAuthenticator == nil || p.DeviceStore == nil || p.PhoneGrantStore == nil || p.PromptJournal == nil || p.MessageStore == nil || p.KeyPackageStore == nil {
			return nil, errors.New("relaycore: durable ports are required")
		}
		store = composedStore{p.PrincipalAuthenticator, p.DeviceStore, p.PhoneGrantStore, p.PromptJournal, p.MessageStore, p.KeyPackageStore}
	}
	clock := config.Ports.Clock
	if clock == nil {
		clock = realClock{}
	}
	audit := config.Ports.AuditSink
	if audit == nil {
		audit = nopAudit{}
	}
	mode := config.TransportMode
	if mode == "" {
		mode = protocol.TransportSealed
	}
	parsed, err := protocol.ParseTransportMode(string(mode))
	if err != nil {
		return nil, err
	}
	if config.DuplicateDaemon != ReplaceExisting && config.DuplicateDaemon != RejectNew {
		return nil, errors.New("relaycore: invalid duplicate daemon policy")
	}
	s := &Engine{
		db:             store,
		connMgr:        NewConnectionManagerWithPolicy(store, config.DuplicateDaemon),
		phoneSecret:    config.PhoneSecret,
		dataDir:        config.DataDir,
		transportMode:  parsed,
		appVersion:     strings.TrimSpace(config.AppVersion),
		allowedOrigins: normalizeAllowedOrigins(config.AllowedOrigins),
		attachments:    config.Ports.AttachmentStore,
		attachmentURLs: config.Ports.AttachmentURLBuilder,
		pushSink:       config.Ports.PushSink,
		audit:          audit,
		clock:          clock,
		synchronizer:   config.Ports.PrincipalSynchronizer,
	}

	// Set up device online/offline callbacks. Replacements with a different
	// daemon release reuse the online event so subscribed phones refresh the
	// live version without treating the device as offline first.
	broadcastDeviceOnline := func(deviceID, daemonVersion string) {
		msg := s.stampEnvelope(protocol.NewMessage(protocol.MsgDeviceOnline, deviceID))
		msg.Payload = map[string]any{
			"device_id":      deviceID,
			"daemon_version": daemonVersion,
		}
		s.connMgr.BroadcastToPhones(deviceID, msg)
	}
	s.connMgr.OnDeviceUp(broadcastDeviceOnline)
	s.connMgr.OnDeviceVersionChange(broadcastDeviceOnline)

	s.connMgr.OnDeviceDown(func(deviceID string) {
		msg := s.stampEnvelope(protocol.NewMessage(protocol.MsgDeviceOffline, deviceID))
		msg.Payload = map[string]any{"device_id": deviceID}
		s.connMgr.BroadcastToPhones(deviceID, msg)
	})

	return s, nil
}

// AppVersion returns the deployment shell's release identity.
func (s *Engine) AppVersion() string {
	if s == nil || s.appVersion == "" {
		return "dev"
	}
	return s.appVersion
}

// SetAppVersion supplies the deployment shell release identity.
func (s *Engine) SetAppVersion(version string) { s.appVersion = strings.TrimSpace(version) }

// Connections exposes the live connection manager. Tests and deployment
// shells use it to observe daemon/phone membership without reaching into
// unexported fields.
func (s *Engine) Connections() *ConnectionManager {
	if s == nil {
		return nil
	}
	return s.connMgr
}

// Handler returns a standalone-compatible handler with bootstrap routes.
func (s *Engine) Handler() http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	return s.CORSMiddleware(mux)
}

// Close disconnects all data-plane principals. The Store lifecycle remains
// owned by the deployment shell so it can sequence database shutdown safely.
func (s *Engine) Close() error {
	if s != nil && s.connMgr != nil {
		s.connMgr.Close()
	}
	return nil
}

// SetDataDir sets the root directory for attachments (and related files).
func (s *Server) SetDataDir(dir string) {
	s.dataDir = dir
}

// SetTransportMode configures the nest-wide transport mode (sealed|open).
// One nest has one fixed mode; clients must match.
func (s *Server) SetTransportMode(mode protocol.TransportMode) error {
	parsed, err := protocol.ParseTransportMode(string(mode))
	if err != nil {
		return err
	}
	s.transportMode = parsed
	return nil
}

// TransportMode returns the configured nest transport mode.
func (s *Server) TransportMode() protocol.TransportMode {
	if s.transportMode == "" {
		return protocol.TransportSealed
	}
	return s.transportMode
}

// stampEnvelope fills protocol_version and transport_mode on outbound frames.
func (s *Server) stampEnvelope(msg *protocol.NekoMessage) *protocol.NekoMessage {
	if msg == nil {
		return nil
	}
	if msg.ProtocolVersion == "" {
		msg.ProtocolVersion = protocol.CurrentProtocolVersion
	}
	if msg.TransportMode == "" {
		msg.TransportMode = s.TransportMode()
	}
	return msg
}

// writeHandshakeError sends a version/mode negotiation failure and closes.
func (s *Server) writeHandshakeError(conn interface {
	WriteJSON(v any) error
	Close() error
}, result protocol.HandshakeResult) {
	payload := protocol.HandshakeErrorPayload(result)
	// Protocol 1.3 exposes a deployment-neutral action code while retaining the
	// old negotiation detail for diagnostics.
	if result.ErrorCode == protocol.ErrCodeVersionMismatch {
		payload["legacy_error_code"] = result.ErrorCode
		payload["error_code"] = protocol.ServiceErrorProtocolUpgradeRequired
	}
	payload["retryable"] = false
	payload["transport_mode"] = string(s.TransportMode())
	payload["server_version"] = s.AppVersion()
	_ = conn.WriteJSON(s.stampEnvelope(&protocol.NekoMessage{
		Type:      protocol.MsgError,
		Timestamp: s.clock.Now().Unix(),
		Payload:   payload,
	}))
	_ = conn.Close()
}

// negotiateFirstFrame validates envelope form + version/mode on a first frame.
func (s *Server) negotiateFirstFrame(msg *protocol.NekoMessage) protocol.HandshakeResult {
	if err := protocol.ValidateEnvelopeForm(msg); err != nil {
		return protocol.HandshakeResult{
			ErrorCode: protocol.ErrCodeInvalidEnvelope,
			Message:   err.Error(),
		}
	}
	return protocol.NegotiateHandshake(
		msg.ProtocolVersion,
		string(msg.TransportMode),
		s.TransportMode(),
		protocol.CurrentProtocolMinor,
	)
}

// RegisterRoutes sets up HTTP routes.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.RegisterDataPlaneRoutes(mux)
	mux.HandleFunc("/api/devices/register", s.handleRegisterDevice)
	mux.HandleFunc("/api/phones/bootstrap", s.handlePhoneBootstrap)
	mux.HandleFunc("/health", s.handleHealth)
}

// RegisterDataPlaneRoutes installs routes safe for a deployment shell that
// performs registration and first-phone handoff itself.
func (s *Server) RegisterDataPlaneRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/daemon", s.HandleDaemonWS)
	mux.HandleFunc("/ws/phone", s.HandlePhoneWS)

	// Device APIs
	mux.HandleFunc("/api/devices", s.handleListDevices)
	mux.HandleFunc("/api/devices/sessions", s.handleDeviceSessions)

	// Phone identity APIs (v1)
	mux.HandleFunc("/api/phones", s.handleListPhones)
	mux.HandleFunc("/api/phones/revoke", s.handleRevokePhone)

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
	mux.HandleFunc("/api/devices/keys", s.handleUploadDeviceKeys)
	mux.HandleFunc("/api/devices/grants", s.handleListDeviceGrants)
	mux.HandleFunc("/api/keys", s.handleKeyPackages)
	mux.HandleFunc("/api/keys/upload", s.handleUploadKeyPackage)

}

func (s *Engine) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":           "nyan~",
		"server_version":   s.AppVersion(),
		"protocol_version": protocol.CurrentProtocolVersion,
		"transport_mode":   string(s.TransportMode()),
	})
}

// TrustedDevice is supplied only after a deployment shell has authorized a
// registration. It deliberately has no deployment-policy fields.
type TrustedDevice struct {
	ID, Name, OS, Ed25519Public, X25519Public, IdentityFingerprint string
}

func (s *Engine) RegisterTrustedDevice(device TrustedDevice) (string, error) {
	token, err := s.db.RegisterDevice(device.ID, device.Name, device.OS)
	if err != nil {
		return "", err
	}
	if device.Ed25519Public != "" || device.X25519Public != "" {
		if err := s.db.SetDevicePublicKeys(device.ID, device.Ed25519Public, device.X25519Public, device.IdentityFingerprint); err != nil {
			return "", err
		}
	}
	return token, nil
}

type TrustedPhone struct {
	Name, Ed25519Public, X25519Public string
}

func (s *Engine) CreateTrustedPhone(phone TrustedPhone) (string, string, error) {
	id, token, err := s.db.CreatePhoneIdentity(phone.Name)
	if err != nil {
		return "", "", err
	}
	if phone.Ed25519Public != "" || phone.X25519Public != "" {
		if err := s.db.SetPhonePublicKeys(id, phone.Ed25519Public, phone.X25519Public); err != nil {
			return "", "", err
		}
	}
	return id, token, nil
}

type ApprovedDevice struct {
	ID, Name, OS string
	Credential   Credential
}
type ApprovedPhone struct {
	ID, Name   string
	Credential Credential
}

func (s *Engine) SyncApprovedDevice(device ApprovedDevice) error {
	if s.synchronizer == nil {
		return errors.New("relaycore: principal synchronization is not configured")
	}
	if err := validateCredential(device.Credential); err != nil {
		return err
	}
	return s.synchronizer.SyncDevice(device.ID, device.Name, device.OS, device.Credential)
}

func (s *Engine) RevokeApprovedDevice(id string) error {
	if s.synchronizer == nil {
		return errors.New("relaycore: principal synchronization is not configured")
	}
	err := s.synchronizer.RevokeDevice(id)
	if dc := s.connMgr.currentDaemon(id); dc != nil && dc.Conn != nil {
		_ = dc.Conn.Close()
	}
	return err
}

func (s *Engine) SyncApprovedPhone(phone ApprovedPhone) error {
	if s.synchronizer == nil {
		return errors.New("relaycore: principal synchronization is not configured")
	}
	if err := validateCredential(phone.Credential); err != nil {
		return err
	}
	return s.synchronizer.SyncPhone(phone.ID, phone.Name, phone.Credential)
}

func validateCredential(credential Credential) error {
	if credential.Value == "" {
		return errors.New("relaycore: credential is required")
	}
	if credential.Kind == CredentialRaw {
		return nil
	}
	if credential.Kind != CredentialSHA256 || len(credential.Value) != 64 {
		return errors.New("relaycore: invalid credential kind or digest")
	}
	for _, r := range credential.Value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return errors.New("relaycore: invalid sha256 credential digest")
		}
	}
	return nil
}

func (s *Engine) RevokeApprovedPhone(id string) error {
	if s.synchronizer == nil {
		return errors.New("relaycore: principal synchronization is not configured")
	}
	err := s.synchronizer.RevokePhone(id)
	s.connMgr.ClosePhonesByPrincipal(id)
	return err
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
		return
	}

	devices, err := s.visibleDevices(auth)
	if err != nil {
		WriteAPIError(w, stableError(protocol.ServiceErrorServiceProvisioning, "device list unavailable", true, http.StatusInternalServerError))
		return
	}
	s.decorateDeviceOnlineStatus(devices)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"devices":        devices,
		"server_version": s.AppVersion(),
	})
}

// reportedComponentVersion accepts bounded ASCII release tokens. NekoNest
// release builds use SemVer; invalid/unreported values remain unknown.
func reportedComponentVersion(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	raw, ok := payload[key].(string)
	if !ok {
		return ""
	}
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			r == '.' || r == '-' || r == '+' {
			continue
		}
		return ""
	}
	return value
}

func (s *Server) handleDeviceSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
		return
	}

	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
		return
	}
	if !s.phoneMayAccessDevice(auth, deviceID) {
		http.Error(w, "forbidden", http.StatusForbidden)
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
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
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
		if !s.phoneMayAccessDevice(auth, deviceID) {
			http.Error(w, "forbidden", http.StatusForbidden)
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
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
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
	if s.pushSink != nil {
		if err := s.pushSink.Validate(req.Endpoint, req.P256DH, req.Auth); err != nil {
			http.Error(w, "invalid push subscription", http.StatusBadRequest)
			return
		}
	} else if err := push.ValidateEndpoint(req.Endpoint); err != nil {
		http.Error(w, "invalid push endpoint", http.StatusBadRequest)
		return
	}
	if s.pushSink == nil {
		if err := push.ValidateKeys(req.P256DH, req.Auth); err != nil {
			http.Error(w, "invalid push subscription keys", http.StatusBadRequest)
			return
		}
	}

	sub := &db.PushSubscription{
		DeviceID: req.DeviceID,
		Endpoint: req.Endpoint,
		P256DH:   req.P256DH,
		Auth:     req.Auth,
	}
	if auth != nil && !auth.AdminBypass {
		if !s.phoneMayAccessDevice(auth, req.DeviceID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		sub.PhoneID = auth.PhoneID
	}
	if err := s.db.SavePushSubscription(sub); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	opslog.Info("server.registration", "push_subscription_registered", "push subscription registered")

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
	if s.pushSink != nil {
		writeJSON(w, map[string]any{"enabled": s.pushSink.Enabled(), "public_key": s.pushSink.PublicKey()})
		return
	}
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
		WriteAPIError(w, stableError(protocol.ServiceErrorRegistrationDisabled, "device registration is disabled", false, http.StatusServiceUnavailable))
		return
	}
	if bootstrap != "" {
		got := r.Header.Get("X-Neko-Bootstrap")
		if got == "" {
			got = r.URL.Query().Get("bootstrap")
		}
		if !secureEqual(got, bootstrap) {
			WriteAPIError(w, stableError(protocol.ServiceErrorDeviceCredentialInvalid, "registration credential invalid", false, http.StatusUnauthorized))
			return
		}
	}

	var req struct {
		DeviceID            string `json:"device_id"`
		Name                string `json:"name"`
		OS                  string `json:"os"`
		Ed25519Public       string `json:"ed25519_public"`
		X25519Public        string `json:"x25519_public"`
		IdentityFingerprint string `json:"identity_fingerprint"`
		TransportMode       string `json:"transport_mode"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.TransportMode != "" && req.TransportMode != string(s.TransportMode()) {
		http.Error(w, "transport_mode mismatch: nest is "+string(s.TransportMode()), http.StatusConflict)
		return
	}

	if req.Name == "" {
		req.Name = "Host PC"
	}
	if req.DeviceID == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			WriteAPIError(w, stableError(protocol.ServiceErrorServiceProvisioning, "device registration unavailable", true, http.StatusInternalServerError))
			return
		}
		req.DeviceID = "device_" + hex.EncodeToString(b)
	}

	token, err := s.db.RegisterDevice(req.DeviceID, req.Name, req.OS)
	if err != nil {
		if s.db.DeviceExists(req.DeviceID) {
			WriteAPIError(w, stableError(protocol.ServiceErrorDeviceIdentityConflict, "device registration failed", false, http.StatusConflict))
			return
		}
		WriteAPIError(w, stableError(protocol.ServiceErrorServiceProvisioning, "device registration unavailable", true, http.StatusInternalServerError))
		return
	}
	if req.Ed25519Public != "" || req.X25519Public != "" {
		_ = s.db.SetDevicePublicKeys(req.DeviceID, req.Ed25519Public, req.X25519Public, req.IdentityFingerprint)
	}

	opslog.Info("server.registration", "device_registered", "device registered", "os", safeDeviceOSForLog(req.OS))

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, protocol.DeviceRegistrationResponse{DeviceID: req.DeviceID, Token: token, Name: req.Name, TransportMode: s.TransportMode(), ConnectionState: protocol.ConnectionReady})
}

func safeDeviceOSForLog(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return "unknown"
	}
}

func (s *Server) handleGeneratePairCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DeviceID            string `json:"device_id"`
		Token               string `json:"token"`
		Ed25519Public       string `json:"ed25519_public"`
		X25519Public        string `json:"x25519_public"`
		IdentityFingerprint string `json:"identity_fingerprint"`
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
		WriteAPIError(w, stableError(protocol.ServiceErrorDeviceCredentialInvalid, "invalid device token", false, http.StatusUnauthorized))
		return
	}
	if req.Ed25519Public != "" && req.X25519Public != "" {
		_ = s.db.SetDevicePublicKeys(req.DeviceID, req.Ed25519Public, req.X25519Public, req.IdentityFingerprint)
	}

	// Generate 6-char pairing code. Fail closed if the random source is unavailable.
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		WriteAPIError(w, stableError(protocol.ServiceErrorServiceProvisioning, "pair code unavailable", true, http.StatusInternalServerError))
		return
	}
	code := hex.EncodeToString(b)[:6]

	expiresAt := s.clock.Now().Add(5 * time.Minute)
	if err := s.db.CreatePairCode(code, req.DeviceID, expiresAt); err != nil {
		WriteAPIError(w, stableError(protocol.ServiceErrorServiceProvisioning, "pair code unavailable", true, http.StatusInternalServerError))
		return
	}

	// Do not log the pair code itself (X7).
	opslog.Info("server.registration", "pair_code_generated", "pair code generated")

	keys, _ := s.db.GetDevicePublicKeys(req.DeviceID)
	resp := map[string]any{
		"code":             code,
		"expires_at":       expiresAt.Unix(),
		"device_id":        req.DeviceID,
		"protocol_version": protocol.CurrentProtocolVersion,
		"transport_mode":   string(s.TransportMode()),
	}
	if keys != nil {
		resp["ed25519_public"] = keys.Ed25519Public
		resp["x25519_public"] = keys.X25519Public
		resp["identity_fingerprint"] = keys.Fingerprint
	}
	if d, err := s.db.GetDevice(req.DeviceID); err == nil && d != nil {
		resp["name"] = d.Name
		resp["os"] = d.OS
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, resp)
}

// handleUploadDeviceKeys lets an authenticated daemon publish/refresh E2E public keys.
func (s *Server) handleUploadDeviceKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DeviceID            string `json:"device_id"`
		Token               string `json:"token"`
		Ed25519Public       string `json:"ed25519_public"`
		X25519Public        string `json:"x25519_public"`
		IdentityFingerprint string `json:"identity_fingerprint"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" || req.Token == "" || !s.db.ValidateDeviceToken(req.DeviceID, req.Token) {
		WriteAPIError(w, stableError(protocol.ServiceErrorDeviceCredentialInvalid, "invalid device token", false, http.StatusUnauthorized))
		return
	}
	if req.Ed25519Public == "" || req.X25519Public == "" || req.IdentityFingerprint == "" {
		http.Error(w, "public keys required", http.StatusBadRequest)
		return
	}
	if err := s.db.SetDevicePublicKeys(req.DeviceID, req.Ed25519Public, req.X25519Public, req.IdentityFingerprint); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{"status": "ok", "device_id": req.DeviceID})
}

// handleListDeviceGrants: daemon lists phones granted to this device (for key wrap).
func (s *Server) handleListDeviceGrants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	token := strings.TrimSpace(r.Header.Get("X-Neko-Device-Token"))
	if token == "" {
		token = phoneSecretFromRequest(r) // Bearer device token also ok
	}
	if deviceID == "" || token == "" || !s.db.ValidateDeviceToken(deviceID, token) {
		WriteAPIError(w, stableError(protocol.ServiceErrorDeviceCredentialInvalid, "invalid device token", false, http.StatusUnauthorized))
		return
	}
	grants, err := s.db.ListPhoneGrantsForDevice(deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		PhoneID       string `json:"phone_id"`
		Ed25519Public string `json:"ed25519_public"`
		X25519Public  string `json:"x25519_public"`
		PairedAt      int64  `json:"paired_at"`
	}
	out := make([]row, 0, len(grants))
	for _, g := range grants {
		out = append(out, row{
			PhoneID: g.PhoneID, Ed25519Public: g.Ed25519Public,
			X25519Public: g.X25519Public, PairedAt: g.PairedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{"grants": out})
}

// handleKeyPackages: phone lists wrapped keys for a device.
func (s *Server) handleKeyPackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
		return
	}
	deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if deviceID == "" {
		http.Error(w, "device_id required", http.StatusBadRequest)
		return
	}
	phoneID := ""
	if auth != nil && !auth.AdminBypass {
		phoneID = auth.PhoneID
		if !s.phoneMayAccessDevice(auth, deviceID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	} else {
		// Admin: optional phone_id query
		phoneID = strings.TrimSpace(r.URL.Query().Get("phone_id"))
		if phoneID == "" {
			http.Error(w, "phone_id required for admin listing", http.StatusBadRequest)
			return
		}
	}
	pkgs, err := s.db.ListKeyPackages(phoneID, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pkgs == nil {
		pkgs = []map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{"packages": pkgs})
}

// handleUploadKeyPackage: daemon publishes a wrapped content key for a phone.
func (s *Server) handleUploadKeyPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DeviceID   string `json:"device_id"`
		Token      string `json:"token"`
		PhoneID    string `json:"phone_id"`
		Scope      string `json:"scope"`
		SessionID  string `json:"session_id"`
		Epoch      uint64 `json:"epoch"`
		WrappedKey string `json:"wrapped_key"`
		Nonce      string `json:"nonce"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" || req.Token == "" || !s.db.ValidateDeviceToken(req.DeviceID, req.Token) {
		WriteAPIError(w, stableError(protocol.ServiceErrorDeviceCredentialInvalid, "invalid device token", false, http.StatusUnauthorized))
		return
	}
	if req.PhoneID == "" || req.Scope == "" || req.WrappedKey == "" || req.Nonce == "" {
		http.Error(w, "phone_id, scope, wrapped_key, nonce required", http.StatusBadRequest)
		return
	}
	if err := s.db.UpsertKeyPackage(req.PhoneID, req.DeviceID, req.Scope, req.SessionID, req.Epoch, req.WrappedKey, req.Nonce); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Fan-out key_package to phones subscribed to this device.
	msg := s.stampEnvelope(protocol.NewMessage(protocol.MsgKeyPackage, req.DeviceID))
	msg.Payload = map[string]any{
		"phone_id":    req.PhoneID,
		"scope":       req.Scope,
		"session_id":  req.SessionID,
		"epoch":       req.Epoch,
		"wrapped_key": req.WrappedKey,
		"nonce":       req.Nonce,
	}
	s.connMgr.BroadcastToPhones(req.DeviceID, msg)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleConsumePairCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
		return
	}

	var req struct {
		Code                string `json:"code"`
		Name                string `json:"name"` // optional phone display name when minting under admin
		ExpectedFingerprint string `json:"expected_fingerprint"`
		ExpectedDeviceID    string `json:"device_id"`
		PhoneEd25519Public  string `json:"phone_ed25519_public"`
		PhoneX25519Public   string `json:"phone_x25519_public"`
		PairTranscript      string `json:"pair_transcript"` // base64url optional audit
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
	if req.ExpectedDeviceID != "" && req.ExpectedDeviceID != deviceID {
		http.Error(w, "device fingerprint mismatch", http.StatusConflict)
		return
	}

	// Look up device name for the phone UI
	name := deviceID
	if d, err := s.db.GetDevice(deviceID); err == nil && d != nil {
		name = d.Name
	}
	keys, _ := s.db.GetDevicePublicKeys(deviceID)
	if req.ExpectedFingerprint != "" && keys != nil && keys.Fingerprint != "" &&
		req.ExpectedFingerprint != keys.Fingerprint {
		http.Error(w, "device fingerprint mismatch", http.StatusConflict)
		return
	}

	resp := map[string]any{
		"device_id": deviceID,
		"name":      name,
		"status":    "paired",
	}
	if keys != nil {
		resp["ed25519_public"] = keys.Ed25519Public
		resp["x25519_public"] = keys.X25519Public
		resp["identity_fingerprint"] = keys.Fingerprint
	}

	// Ensure we have a phone identity to grant.
	phoneID := ""
	phoneToken := ""
	if auth != nil && !auth.AdminBypass && auth.PhoneID != "" {
		phoneID = auth.PhoneID
	} else {
		// Admin secret / open-dev: mint a phone identity so grants can attach.
		pid, tok, err := s.db.CreatePhoneIdentity(req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		phoneID = pid
		phoneToken = tok
		resp["phone_id"] = phoneID
		resp["phone_token"] = phoneToken
	}

	if req.PhoneEd25519Public != "" || req.PhoneX25519Public != "" {
		_ = s.db.SetPhonePublicKeys(phoneID, req.PhoneEd25519Public, req.PhoneX25519Public)
	}

	if err := s.db.GrantPhoneDevice(phoneID, deviceID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp["phone_id"] = phoneID

	// Notify online daemon so it can wrap catalog keys immediately.
	notify := s.stampEnvelope(protocol.NewMessage(protocol.MsgPairReady, deviceID))
	notify.Payload = map[string]any{
		"phone_id":             phoneID,
		"phone_ed25519_public": req.PhoneEd25519Public,
		"phone_x25519_public":  req.PhoneX25519Public,
		"pair_code":            req.Code,
	}
	_ = s.connMgr.SendToDaemon(deviceID, notify)

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, resp)
}

func (s *Server) handlePhoneBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Bootstrap requires admin secret when configured; open in local-dev.
	if s.phoneSecret != "" {
		got := phoneSecretFromRequest(r)
		if !secureEqual(got, s.phoneSecret) {
			WriteAPIError(w, stableError(protocol.ServiceErrorPhoneCredentialInvalid, "phone credential invalid", false, http.StatusUnauthorized))
			return
		}
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = readJSON(r, &req)
	phoneID, token, err := s.db.CreatePhoneIdentity(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"phone_id": phoneID,
		"token":    token,
		"name":     req.Name,
	})
}

func (s *Server) handleListPhones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Admin-only listing.
	if s.phoneSecret != "" && !secureEqual(phoneSecretFromRequest(r), s.phoneSecret) {
		WriteAPIError(w, stableError(protocol.ServiceErrorPhoneCredentialInvalid, "phone credential invalid", false, http.StatusUnauthorized))
		return
	}
	phones, err := s.db.ListPhones()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		CreatedAt int64  `json:"created_at"`
		LastSeen  int64  `json:"last_seen"`
		Revoked   bool   `json:"revoked"`
	}
	out := make([]row, 0, len(phones))
	for _, p := range phones {
		out = append(out, row{
			ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastSeen: p.LastSeen, Revoked: p.RevokedAt > 0,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{"phones": out})
}

func (s *Server) handleRevokePhone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth, ok := s.requirePhoneAuthResult(w, r)
	if !ok {
		return
	}
	var req struct {
		PhoneID string `json:"phone_id"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.PhoneID) == "" {
		http.Error(w, "phone_id required", http.StatusBadRequest)
		return
	}
	// Non-admin phones may only revoke themselves.
	if auth != nil && !auth.AdminBypass && auth.PhoneID != req.PhoneID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.db.RevokePhone(req.PhoneID); err != nil {
		if errors.Is(err, db.ErrPhoneNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.connMgr.ClosePhonesByPrincipal(req.PhoneID)
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{"status": "revoked", "phone_id": req.PhoneID})
}

// requirePhoneAuth checks the phone shared secret or a phone token when configured.
// Accepts Authorization: Bearer <secret|token> or X-Neko-Secret: <secret|token>.
func (s *Server) requirePhoneAuth(w http.ResponseWriter, r *http.Request) bool {
	_, ok := s.requirePhoneAuthResult(w, r)
	return ok
}

// requirePhoneAuthResult authenticates the caller and returns phone auth context.
// When phoneSecret is empty (loopback dev), returns (nil, true) meaning open access.
func (s *Server) requirePhoneAuthResult(w http.ResponseWriter, r *http.Request) (*db.PhoneAuth, bool) {
	if s.phoneSecret == "" {
		return nil, true
	}
	cred := phoneSecretFromRequest(r)
	if cred == "" {
		WriteAPIError(w, stableError(protocol.ServiceErrorPhoneCredentialInvalid, "phone credential invalid", false, http.StatusUnauthorized))
		return nil, false
	}
	// Admin nest secret (legacy full access / bootstrap).
	if secureEqual(cred, s.phoneSecret) {
		return &db.PhoneAuth{AdminBypass: true, Name: "admin"}, true
	}
	// Independent phone token.
	auth, err := s.db.ValidatePhoneToken(cred)
	if err != nil {
		WriteAPIError(w, stableError(protocol.ServiceErrorPhoneCredentialInvalid, "phone credential invalid", false, http.StatusUnauthorized))
		return nil, false
	}
	return auth, true
}

// phoneMayAccessDevice enforces grants for non-admin phone tokens.
func (s *Server) phoneMayAccessDevice(auth *db.PhoneAuth, deviceID string) bool {
	if auth == nil || auth.AdminBypass {
		return true
	}
	return s.db.PhoneHasDeviceGrant(auth.PhoneID, deviceID)
}

func (s *Server) visibleDevices(auth *db.PhoneAuth) ([]*protocol.Device, error) {
	devices, err := s.db.ListDevices()
	if err != nil {
		return nil, err
	}
	if auth == nil || auth.AdminBypass {
		return devices, nil
	}
	allowed, err := s.db.ListPhoneDeviceIDs(auth.PhoneID)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		allow[id] = true
	}
	filtered := devices[:0]
	for _, d := range devices {
		if allow[d.ID] {
			filtered = append(filtered, d)
		}
	}
	return filtered, nil
}

func (s *Server) decorateDeviceOnlineStatus(devices []*protocol.Device) {
	onlineDevices := s.connMgr.GetOnlineDevices()
	onlineSet := make(map[string]bool, len(onlineDevices))
	for _, id := range onlineDevices {
		onlineSet[id] = true
	}
	for _, d := range devices {
		if d == nil {
			continue
		}
		if onlineSet[d.ID] {
			d.Status = "online"
			d.DaemonVersion = s.connMgr.GetDaemonVersion(d.ID)
		}
	}
}

func phoneSecretFromRequest(r *http.Request) string {
	if h := r.Header.Get("X-Neko-Secret"); h != "" {
		return h
	}
	if h := r.Header.Get("X-Neko-Phone-Token"); h != "" {
		return h
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if q := r.URL.Query().Get("secret"); q != "" {
		return q
	}
	if q := r.URL.Query().Get("phone_token"); q != "" {
		return q
	}
	return ""
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
