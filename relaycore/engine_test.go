package relaycore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/klarkxy/nekonest/relaycore/protocol"
	db "github.com/klarkxy/nekonest/relaycore/store"
)

type stubStore struct{ db.Store }

func (stubStore) UpdateDeviceLastSeen(string) error { return nil }

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeSync struct {
	device, phone string
	revoked       bool
	kind          CredentialKind
}

func (f *fakeSync) SyncDevice(id, _, _ string, credential Credential) error {
	f.device = id + ":" + credential.Value
	f.kind = credential.Kind
	return nil
}
func (f *fakeSync) RevokeDevice(id string) error { f.revoked = id != ""; return nil }
func (f *fakeSync) SyncPhone(id, _ string, credential Credential) error {
	f.phone = id + ":" + credential.Value
	f.kind = credential.Kind
	return nil
}
func (f *fakeSync) RevokePhone(id string) error { f.revoked = id != ""; return nil }

type fakeAttachmentStore struct {
	puts int
	last Attachment
	body []byte
}

func (f *fakeAttachmentStore) Put(_ context.Context, attachment Attachment, data []byte) error {
	f.puts++
	f.last = attachment
	f.body = append([]byte(nil), data...)
	return nil
}
func (f *fakeAttachmentStore) Get(_ context.Context, id string) (Attachment, io.ReadCloser, error) {
	if f.last.ID != id {
		return Attachment{}, nil, errors.New("missing")
	}
	return f.last, io.NopCloser(bytes.NewReader(f.body)), nil
}
func (*fakeAttachmentStore) Delete(context.Context, string) error { return nil }

type fakeURLBuilder struct{}

func (fakeURLBuilder) BuildAttachmentURL(id, key string) string {
	return "/api/attachments/" + id + "?route=opaque&k=" + key
}

type fakeAudit struct{ count int }

func (f *fakeAudit) Emit(context.Context, AuditEvent) { f.count++ }

type pushStore struct {
	stubStore
	subs    []*db.PushSubscription
	deleted int
}

func (s *pushStore) GetPushSubscriptions(string) ([]*db.PushSubscription, error) { return s.subs, nil }
func (s *pushStore) DeletePushSubscription(string) error                         { s.deleted++; return nil }

type fakePush struct{ sends int }

func (*fakePush) Enabled() bool                         { return true }
func (*fakePush) PublicKey() string                     { return "public" }
func (*fakePush) Validate(string, string, string) error { return nil }
func (p *fakePush) Send(context.Context, []db.PushSubscription, PushMessage, func(string)) bool {
	p.sends++
	return true
}

type rejectCredentialStore struct{ stubStore }

func (*rejectCredentialStore) ValidateDeviceToken(string, string) bool { return false }
func (*rejectCredentialStore) ValidatePhoneToken(string) (*db.PhoneAuth, error) {
	return nil, db.ErrPhoneTokenInvalid
}

func (*rejectCredentialStore) ListUncommittedAcceptedPrompts(string, int) ([]*db.PromptCommand, error) {
	return nil, nil
}

func TestDeviceAlreadyConnectedErrorIsRetryable(t *testing.T) {
	engine, err := NewEngine(Config{Store: acceptingStore{}, DuplicateDaemon: RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewServer(http.HandlerFunc(engine.HandleDaemonWS))
	defer first.Close()
	conn1, _, err := websocket.DefaultDialer.Dial("ws"+first.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Close()
	auth := protocol.NewMessage(protocol.MsgRegisterDevice, "d")
	auth.TransportMode = protocol.TransportSealed
	auth.Payload = map[string]any{"device_id": "d", "token": "credential"}
	if err := conn1.WriteJSON(auth); err != nil {
		t.Fatal(err)
	}
	var accepted protocol.NekoMessage
	if err := conn1.ReadJSON(&accepted); err != nil || accepted.Type != protocol.MsgAuthResponse {
		t.Fatalf("first auth=%#v err=%v", accepted, err)
	}

	conn2, _, err := websocket.DefaultDialer.Dial("ws"+first.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	if err := conn2.WriteJSON(auth); err != nil {
		t.Fatal(err)
	}
	var rejected protocol.NekoMessage
	if err := conn2.ReadJSON(&rejected); err != nil {
		t.Fatal(err)
	}
	if rejected.Type != protocol.MsgError || rejected.Payload["error_code"] != string(protocol.ServiceErrorDeviceAlreadyConnected) {
		t.Fatalf("second frame=%#v", rejected)
	}
	retryable, _ := rejected.Payload["retryable"].(bool)
	if !retryable {
		t.Fatalf("device_already_connected must be retryable: %#v", rejected.Payload)
	}
}

type acceptingStore struct{ stubStore }

func (acceptingStore) ValidateDeviceToken(string, string) bool { return true }
func (acceptingStore) ListUncommittedAcceptedPrompts(string, int) ([]*db.PromptCommand, error) {
	return nil, nil
}

func TestDuplicateDaemonPolicyRejectsSecondLiveIdentity(t *testing.T) {
	manager := NewConnectionManagerWithPolicy(stubStore{}, RejectNew)
	first, err := manager.TryAddDaemonVersioned("device-a", nil, "1")
	if err != nil || first == nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := manager.TryAddDaemonVersioned("device-a", nil, "2"); !errors.Is(err, ErrDeviceAlreadyConnected) {
		t.Fatalf("second add error=%v", err)
	}
}

func TestDuplicateDaemonPolicyReplaceExistingPreservesStandalone(t *testing.T) {
	manager := NewConnectionManagerWithPolicy(stubStore{}, ReplaceExisting)
	first, err := manager.TryAddDaemonVersioned("device-a", nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.TryAddDaemonVersioned("device-a", nil, "2")
	if err != nil || second == nil || manager.IsLiveDaemon(first) || !manager.IsLiveDaemon(second) {
		t.Fatalf("replacement first=%v second=%v err=%v", manager.IsLiveDaemon(first), manager.IsLiveDaemon(second), err)
	}
}

func TestCORSRequiresExactConfiguredOriginAndPermitsRouteHeaders(t *testing.T) {
	engine, err := NewEngine(Config{Store: stubStore{}, AllowedOrigins: []string{"https://phone.example"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := engine.CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	allowed := httptest.NewRequest(http.MethodOptions, "https://relay.example/api/messages", nil)
	allowed.Header.Set("Origin", "https://phone.example")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, allowed)
	if res.Code != http.StatusNoContent || res.Header().Get("Access-Control-Allow-Origin") != "https://phone.example" {
		t.Fatalf("allowed response=%d headers=%v", res.Code, res.Header())
	}
	if got := res.Header().Get("Access-Control-Allow-Headers"); got == "" || !containsHeader(got, "X-Neko-Route-Handle") || !containsHeader(got, "X-Neko-Phone-Token") || !containsHeader(got, "X-Neko-Secret") {
		t.Fatalf("allow headers=%q", got)
	}

	denied := httptest.NewRequest(http.MethodOptions, "https://relay.example/api/messages", nil)
	denied.Header.Set("Origin", "https://phone.example.evil")
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, denied)
	if bad.Code != http.StatusForbidden || bad.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("denied response=%d headers=%v", bad.Code, bad.Header())
	}
}

func TestSameOriginOnlyTrustsForwardedProtoFromTrustedProxy(t *testing.T) {
	engine, err := NewEngine(Config{Store: stubStore{}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://nest.example/api/devices", nil)
	req.Host = "nest.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	if engine.originAllowed(req, "https://nest.example") {
		t.Fatal("untrusted X-Forwarded-Proto must not satisfy same-origin")
	}
	t.Setenv("NEKONEST_TRUST_PROXY", "1")
	req.RemoteAddr = "127.0.0.1:12345"
	if !engine.originAllowed(req, "https://nest.example") {
		t.Fatal("trusted loopback proxy should honor X-Forwarded-Proto")
	}
}

func TestInjectedPortsAndTrustedPrincipalSynchronization(t *testing.T) {
	syncer := &fakeSync{}
	clock := fakeClock{now: time.Unix(123, 0)}
	attachments := &fakeAttachmentStore{}
	engine, err := NewEngine(Config{Store: stubStore{}, Ports: Ports{PrincipalSynchronizer: syncer, Clock: clock, AttachmentStore: attachments}})
	if err != nil {
		t.Fatal(err)
	}
	if engine.clock.Now().Unix() != 123 || engine.attachments != attachments {
		t.Fatal("injected ports were not retained")
	}
	digest := strings.Repeat("a", 64)
	if err := engine.SyncApprovedDevice(ApprovedDevice{ID: "d", Credential: Credential{Kind: CredentialSHA256, Value: digest}}); err != nil || syncer.device != "d:"+digest || syncer.kind != CredentialSHA256 {
		t.Fatalf("sync device: %v %#v", err, syncer)
	}
	if err := engine.SyncApprovedPhone(ApprovedPhone{ID: "p", Credential: Credential{Kind: CredentialRaw, Value: "phone-secret"}}); err != nil || syncer.phone != "p:phone-secret" {
		t.Fatalf("sync phone: %v %#v", err, syncer)
	}
	if err := engine.RevokeApprovedDevice("d"); err != nil || !syncer.revoked {
		t.Fatalf("revoke: %v %#v", err, syncer)
	}
}

func TestInjectedAttachmentStorePerformsNoLocalWritesAndDecoratesURL(t *testing.T) {
	attachments := &fakeAttachmentStore{}
	dataDir := filepath.Join(t.TempDir(), "must-not-exist")
	engine, err := NewEngine(Config{Store: stubStore{}, DataDir: dataDir, Ports: Ports{AttachmentStore: attachments, AttachmentURLBuilder: fakeURLBuilder{}}})
	if err != nil {
		t.Fatal(err)
	}
	engine.phoneSecret = ""
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", "note.txt")
	_, _ = part.Write([]byte("hello"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res := httptest.NewRecorder()
	engine.handleAttachmentUpload(res, req)
	if res.Code != http.StatusOK || attachments.puts != 1 || !strings.Contains(res.Body.String(), "route=opaque") {
		t.Fatalf("code=%d puts=%d body=%s", res.Code, attachments.puts, res.Body.String())
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("injected attachment path wrote local storage: %v", err)
	}
}

type phoneTokenStore struct {
	stubStore
	auth *db.PhoneAuth
}

func (s phoneTokenStore) ValidatePhoneToken(token string) (*db.PhoneAuth, error) {
	if token == "phone-token" && s.auth != nil {
		return s.auth, nil
	}
	return nil, db.ErrPhoneTokenInvalid
}

func (phoneTokenStore) PhoneHasDeviceGrant(phoneID, deviceID string) bool {
	return phoneID == "phone-1" && deviceID == "dev1"
}

func TestInjectedAttachmentStoreAcceptsGrantedPhoneToken(t *testing.T) {
	attachments := &fakeAttachmentStore{}
	engine, err := NewEngine(Config{
		Store:       phoneTokenStore{auth: &db.PhoneAuth{PhoneID: "phone-1"}},
		PhoneSecret: "admin",
		Ports:       Ports{AttachmentStore: attachments},
	})
	if err != nil {
		t.Fatal(err)
	}
	attachments.last = Attachment{ID: "att-1", Key: "capability-key", MIME: "text/plain", DeviceID: "dev1"}
	attachments.body = []byte("opaque-body")
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/att-1", nil)
	req.Header.Set("X-Neko-Phone-Token", "phone-token")
	res := httptest.NewRecorder()
	engine.handleAttachmentGet(res, req, "att-1")
	if res.Code != http.StatusOK || res.Body.String() != "opaque-body" {
		t.Fatalf("code=%d body=%s", res.Code, res.Body.String())
	}
}

func TestAuditPortReceivesSecurityEvent(t *testing.T) {
	audit := &fakeAudit{}
	engine, err := NewEngine(Config{Store: stubStore{}, Ports: Ports{AuditSink: audit}})
	if err != nil {
		t.Fatal(err)
	}
	engine.auditEvent("relay.auth", "rejected", "credential rejected", nil)
	if audit.count != 1 {
		t.Fatalf("audit count=%d", audit.count)
	}
}

func TestInjectedPushSinkIsTheOnlyDeliveryPath(t *testing.T) {
	store := &pushStore{subs: []*db.PushSubscription{{Endpoint: "https://push.example", P256DH: "key", Auth: "auth"}}}
	push := &fakePush{}
	engine, err := NewEngine(Config{Store: store, Ports: Ports{PushSink: push}})
	if err != nil {
		t.Fatal(err)
	}
	engine.sendPushNotification("d", "s", "title", "body")
	if push.sends != 1 {
		t.Fatalf("push sends=%d", push.sends)
	}
}

func TestDigestPrincipalRegistryHashesPresentedCredentialsAndRevokes(t *testing.T) {
	registry := NewDigestPrincipalRegistry()
	if err := registry.SyncDevice("d", "host", "linux", SHA256Credential("device-secret")); err != nil {
		t.Fatal(err)
	}
	if !registry.ValidateDeviceToken("d", "device-secret") || registry.ValidateDeviceToken("d", "wrong") {
		t.Fatal("device credential digest comparison failed")
	}
	if err := registry.SyncPhone("p", "phone", Credential{Kind: CredentialRaw, Value: "phone-secret"}); err != nil {
		t.Fatal(err)
	}
	if auth, err := registry.ValidatePhoneToken("phone-secret"); err != nil || auth.PhoneID != "p" {
		t.Fatalf("phone auth=%#v err=%v", auth, err)
	}
	_ = registry.RevokeDevice("d")
	_ = registry.RevokePhone("p")
	if registry.ValidateDeviceToken("d", "device-secret") {
		t.Fatal("revoked device credential remains valid")
	}
	if _, err := registry.ValidatePhoneToken("phone-secret"); !errors.Is(err, db.ErrPhoneTokenInvalid) {
		t.Fatalf("revoked phone err=%v", err)
	}
}

func TestStableErrorsCoverHTTPAndFirstWebSocketFrames(t *testing.T) {
	t.Run("registration disabled", func(t *testing.T) {
		t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
		engine, err := NewEngine(Config{Store: &rejectCredentialStore{}, PhoneSecret: "admin"})
		if err != nil {
			t.Fatal(err)
		}
		res := httptest.NewRecorder()
		engine.handleRegisterDevice(res, httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{}`)))
		assertHTTPServiceError(t, res, http.StatusServiceUnavailable, protocol.ServiceErrorRegistrationDisabled)
	})

	t.Run("registration credential", func(t *testing.T) {
		t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "expected")
		engine, err := NewEngine(Config{Store: &rejectCredentialStore{}, PhoneSecret: "admin"})
		if err != nil {
			t.Fatal(err)
		}
		res := httptest.NewRecorder()
		engine.handleRegisterDevice(res, httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{}`)))
		assertHTTPServiceError(t, res, http.StatusUnauthorized, protocol.ServiceErrorDeviceCredentialInvalid)
	})

	t.Run("daemon credential", func(t *testing.T) {
		engine, err := NewEngine(Config{Store: &rejectCredentialStore{}})
		if err != nil {
			t.Fatal(err)
		}
		frame := firstDaemonFrame(t, engine, protocol.CurrentProtocolVersion, "wrong")
		assertWSServiceError(t, frame, protocol.ServiceErrorDeviceCredentialInvalid)
	})

	t.Run("phone credential", func(t *testing.T) {
		engine, err := NewEngine(Config{Store: &rejectCredentialStore{}, PhoneSecret: "admin"})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(engine.HandlePhoneWS))
		defer server.Close()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		msg := protocol.NewMessage(protocol.MsgSubscribe, "d")
		msg.TransportMode = protocol.TransportSealed
		msg.Payload = map[string]any{"device_id": "d", "phone_token": "wrong"}
		if err := conn.WriteJSON(msg); err != nil {
			t.Fatal(err)
		}
		var frame protocol.NekoMessage
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		assertWSServiceError(t, frame, protocol.ServiceErrorPhoneCredentialInvalid)
	})

	t.Run("protocol upgrade", func(t *testing.T) {
		engine, err := NewEngine(Config{Store: &rejectCredentialStore{}})
		if err != nil {
			t.Fatal(err)
		}
		frame := firstDaemonFrame(t, engine, "2.0", "wrong")
		assertWSServiceError(t, frame, protocol.ServiceErrorProtocolUpgradeRequired)
	})
}

func TestTrustedDaemonIngressConsumesFirstFrameExactlyOnce(t *testing.T) {
	authSeen := make(chan struct{}, 1)
	store := &authStore{stubStore: stubStore{}, onAuth: func() { authSeen <- struct{}{} }}
	engine, err := NewEngine(Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		var first protocol.NekoMessage
		if err := conn.ReadJSON(&first); err != nil {
			return
		}
		engine.ServeDaemonConn(conn, &first)
	}))
	defer server.Close()
	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	first := protocol.NewMessage(protocol.MsgRegisterDevice, "d")
	first.TransportMode = protocol.TransportSealed
	first.Payload = map[string]any{"device_id": "d", "token": "credential"}
	if err := conn.WriteJSON(first); err != nil {
		t.Fatal(err)
	}
	var auth protocol.NekoMessage
	if err := conn.ReadJSON(&auth); err != nil || auth.Type != protocol.MsgAuthResponse {
		t.Fatalf("auth=%#v err=%v", auth, err)
	}
	if err := conn.WriteJSON(protocol.NewMessage(protocol.MsgHeartbeat, "d")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-authSeen:
	case <-time.After(time.Second):
		t.Fatal("auth not invoked")
	}
	select {
	case <-authSeen:
		t.Fatal("first frame authenticated twice")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPhoneRevocationClosesOnlyTargetPrincipal(t *testing.T) {
	syncer := &fakeSync{}
	engine, err := NewEngine(Config{Store: stubStore{}, Ports: Ports{PrincipalSynchronizer: syncer}})
	if err != nil {
		t.Fatal(err)
	}
	_, target, closeTarget := websocketPair(t)
	defer closeTarget()
	_, other, closeOther := websocketPair(t)
	defer closeOther()
	_, admin, closeAdmin := websocketPair(t)
	defer closeAdmin()
	engine.connMgr.AddPhoneAuthenticated("d", target, "phone-target", false)
	engine.connMgr.AddPhoneAuthenticated("d", other, "phone-other", false)
	engine.connMgr.AddPhoneAuthenticated("d", admin, "", true)

	if err := engine.RevokeApprovedPhone("phone-target"); err != nil {
		t.Fatal(err)
	}
	engine.connMgr.mu.RLock()
	_, targetPresent := engine.connMgr.phonePrincipals[target]
	otherPrincipal, otherPresent := engine.connMgr.phonePrincipals[other]
	adminPrincipal, adminPresent := engine.connMgr.phonePrincipals[admin]
	engine.connMgr.mu.RUnlock()
	if targetPresent || !otherPresent || otherPrincipal.PhoneID != "phone-other" || !adminPresent || !adminPrincipal.AdminBypass {
		t.Fatalf("target=%v other=%#v/%v admin=%#v/%v", targetPresent, otherPrincipal, otherPresent, adminPrincipal, adminPresent)
	}
}

func TestDeviceRevocationLeavesPhoneAuthorizationConnectionUntouched(t *testing.T) {
	syncer := &fakeSync{}
	engine, err := NewEngine(Config{Store: stubStore{}, Ports: Ports{PrincipalSynchronizer: syncer}})
	if err != nil {
		t.Fatal(err)
	}
	_, phone, closePair := websocketPair(t)
	defer closePair()
	engine.connMgr.AddPhoneAuthenticated("device-target", phone, "phone", false)
	if err := engine.RevokeApprovedDevice("device-target"); err != nil {
		t.Fatal(err)
	}
	engine.connMgr.mu.RLock()
	_, present := engine.connMgr.phonePrincipals[phone]
	engine.connMgr.mu.RUnlock()
	if !present {
		t.Fatal("device revocation unexpectedly revoked an independent phone principal")
	}
}

type authStore struct {
	stubStore
	onAuth func()
}

func (s *authStore) ValidateDeviceToken(_, token string) bool {
	s.onAuth()
	return token == "credential"
}

func firstDaemonFrame(t *testing.T, engine *Engine, version, token string) protocol.NekoMessage {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(engine.HandleDaemonWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	msg := protocol.NewMessage(protocol.MsgRegisterDevice, "d")
	msg.ProtocolVersion = version
	msg.TransportMode = protocol.TransportSealed
	msg.Payload = map[string]any{"device_id": "d", "token": token}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
	var frame protocol.NekoMessage
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	return frame
}

func assertHTTPServiceError(t *testing.T, res *httptest.ResponseRecorder, status int, code protocol.ServiceErrorCode) {
	t.Helper()
	if res.Code != status || !strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status=%d content-type=%q body=%s", res.Code, res.Header().Get("Content-Type"), res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	assertServiceErrorPayload(t, payload, code)
}

func assertWSServiceError(t *testing.T, frame protocol.NekoMessage, code protocol.ServiceErrorCode) {
	t.Helper()
	if frame.Type != protocol.MsgError {
		t.Fatalf("frame type=%q payload=%#v", frame.Type, frame.Payload)
	}
	assertServiceErrorPayload(t, frame.Payload, code)
}

func assertServiceErrorPayload(t *testing.T, payload map[string]any, code protocol.ServiceErrorCode) {
	t.Helper()
	if payload["error_code"] != string(code) || payload["message"] == "" {
		t.Fatalf("service error payload=%#v", payload)
	}
	if retryable, ok := payload["retryable"].(bool); !ok || retryable {
		t.Fatalf("retryable must be explicit false: %#v", payload)
	}
}

func websocketPair(t *testing.T) (client, serverConn *websocket.Conn, closePair func()) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-release
	}))
	client, _, err := websocket.DefaultDialer.Dial("ws"+testServer.URL[len("http"):], nil)
	if err != nil {
		close(release)
		testServer.Close()
		t.Fatal(err)
	}
	serverConn = <-accepted
	return client, serverConn, func() {
		_ = client.Close()
		_ = serverConn.Close()
		close(release)
		testServer.Close()
	}
}
func (*authStore) ListUncommittedAcceptedPrompts(string, int) ([]*db.PromptCommand, error) {
	return nil, nil
}

func containsHeader(list, want string) bool {
	for _, part := range []string{"Authorization", "Content-Type", "X-Neko-Bootstrap", "X-Neko-Phone-Token", "X-Neko-Route-Handle", "X-Neko-Secret"} {
		if part == want && stringContainsToken(list, part) {
			return true
		}
	}
	return false
}

func stringContainsToken(list, token string) bool {
	for start := 0; start < len(list); {
		end := start
		for end < len(list) && list[end] != ',' {
			end++
		}
		part := list[start:end]
		for len(part) > 0 && part[0] == ' ' {
			part = part[1:]
		}
		for len(part) > 0 && part[len(part)-1] == ' ' {
			part = part[:len(part)-1]
		}
		if part == token {
			return true
		}
		start = end + 1
	}
	return false
}

var _ = websocket.CloseNormalClosure
