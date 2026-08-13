package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nekonest/daemon/internal/buildinfo"
	"github.com/nekonest/daemon/internal/opslog"
	"github.com/nekonest/daemon/internal/wire"
)

const (
	maxReconnects      = 50
	maxReconnectDelay  = 60 * time.Second
	writeTimeout       = 10 * time.Second
	readCloseTimeout   = 3 * time.Second
	maxRemoteErrorBody = 64 << 10
)

var ErrEndpointPaused = errors.New("relay endpoint paused")

// Client manages the WebSocket connection to the NekoNest server.
type Client struct {
	serverURL        string
	deviceID         string
	token            string
	transportMode    string
	protocolVersion  string
	conn             *websocket.Conn
	mu               sync.Mutex
	connectMu        sync.Mutex
	dispatchMu       sync.RWMutex // linearizes callbacks against endpoint changes/Close
	onMessage        func([]byte)
	onConnect        func() // called after successful auth (initial + reconnect)
	connected        bool
	reconnects       int // generic network/transient failures only
	serviceRetries   int // allowed service provisioning retries; never capped
	retryableService bool
	retryAfter       time.Duration
	retryWait        func(context.Context, time.Duration) bool
	closed           bool
	endpointActive   bool
	endpointChanged  chan struct{}
	generation       uint64        // incremented whenever the desired endpoint changes
	closeCh          chan struct{} // closed when Close() is called
}

// NewClient creates a new connection client.
func NewClient(ctx context.Context, serverURL, deviceID, token string, modes ...string) *Client {
	transportMode := "open" // compatibility for callers compiled against the legacy constructor
	if len(modes) > 0 {
		transportMode = strings.TrimSpace(modes[0])
	}
	return &Client{
		serverURL:       serverURL,
		deviceID:        deviceID,
		token:           token,
		transportMode:   transportMode,
		protocolVersion: wire.CurrentProtocolVersion,
		endpointActive:  strings.TrimSpace(serverURL) != "",
		endpointChanged: make(chan struct{}),
		closeCh:         make(chan struct{}),
		retryWait: func(ctx context.Context, delay time.Duration) bool {
			select {
			case <-time.After(delay):
				return true
			case <-ctx.Done():
				return false
			}
		},
	}
}

// SetTransportMode sets the persistent nest mode before connecting. Runtime
// config reloads must not call this: changing modes requires re-pairing and a
// fresh daemon process.
func (c *Client) SetTransportMode(mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != "open" && mode != "sealed" {
		return fmt.Errorf("invalid transport_mode %q", mode)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connected && c.transportMode != mode {
		return fmt.Errorf("transport_mode is immutable while connected")
	}
	c.transportMode = mode
	return nil
}

// OnMessage sets the callback for incoming messages.
func (c *Client) OnMessage(fn func([]byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onMessage = fn
}

// OnConnect sets the callback after successful authentication (including reconnect).
func (c *Client) OnConnect(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnect = fn
}

// SetServerURL updates the server URL (for config hot-reload).
func (c *Client) SetServerURL(url string) {
	c.SetServerURLAndPublish(url, nil)
}

// SetServerURLAndPublish updates the desired endpoint and publishes related
// runtime state at the same linearization point as message/connect callbacks.
// The publish callback must not call back into Client methods that acquire
// dispatchMu.
func (c *Client) SetServerURLAndPublish(url string, publish func()) {
	url = strings.TrimSpace(url)
	if url == "" {
		c.PauseAndPublish(publish)
		return
	}
	c.dispatchMu.Lock()
	var oldConn *websocket.Conn
	defer func() {
		c.dispatchMu.Unlock()
		if oldConn != nil {
			_ = oldConn.Close()
		}
	}()

	c.mu.Lock()
	if !c.closed && (c.serverURL != url || !c.endpointActive) {
		c.serverURL = url
		c.endpointActive = true
		c.generation++
		c.reconnects = 0
		c.signalEndpointChangeLocked()
		// Detach before close so an old read error cannot clear a connection that
		// has already authenticated against the new endpoint.
		oldConn = c.conn
		c.conn = nil
		c.connected = false
		c.protocolVersion = wire.CurrentProtocolVersion
	}
	c.mu.Unlock()

	if publish != nil {
		publish()
	}
}

// PauseAndPublish disables dialing without destroying the client. Resume by
// calling SetServerURLAndPublish with a configured service endpoint.
func (c *Client) PauseAndPublish(publish func()) {
	c.dispatchMu.Lock()
	var oldConn *websocket.Conn
	defer func() {
		c.dispatchMu.Unlock()
		if oldConn != nil {
			_ = oldConn.Close()
		}
	}()

	c.mu.Lock()
	if !c.closed && c.endpointActive {
		c.endpointActive = false
		c.generation++
		c.reconnects = 0
		c.signalEndpointChangeLocked()
		oldConn = c.conn
		c.conn = nil
		c.connected = false
		c.protocolVersion = wire.CurrentProtocolVersion
	}
	c.mu.Unlock()
	if publish != nil {
		publish()
	}
}

func (c *Client) signalEndpointChangeLocked() {
	close(c.endpointChanged)
	c.endpointChanged = make(chan struct{})
}

// Connect establishes the WebSocket connection and authenticates.
func (c *Client) Connect() error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return fmt.Errorf("client closed")
		}
		serverURL := c.serverURL
		deviceID := c.deviceID
		token := c.token
		transportMode := c.transportMode
		generation := c.generation
		endpointActive := c.endpointActive
		c.mu.Unlock()
		if !endpointActive {
			return ErrEndpointPaused
		}

		wsURL := serverURL + "/ws/daemon"
		opslog.Info("daemon.connection", "connect_attempt", "connecting to server", "device_id", deviceID, "generation", generation)

		conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			if remote := remoteErrorFromHandshake(response); remote != nil {
				if !c.isCurrentGeneration(generation) {
					continue
				}
				return remote
			}
			if !c.isCurrentGeneration(generation) {
				continue
			}
			return fmt.Errorf("dial: %w", err)
		}

		if transportMode != "open" && transportMode != "sealed" {
			_ = conn.Close()
			return fmt.Errorf("invalid configured transport_mode %q", transportMode)
		}
		authMsg := map[string]interface{}{
			"protocol_version": wire.CurrentProtocolVersion,
			"transport_mode":   transportMode,
			"type":             "register_device",
			"device_id":        deviceID,
			"timestamp":        time.Now().Unix(),
			"payload": map[string]interface{}{
				"device_id":      deviceID,
				"token":          token,
				"os":             runtime.GOOS,
				"daemon_version": buildinfo.Version,
			},
		}

		if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			_ = conn.Close()
			return fmt.Errorf("auth write deadline: %w", err)
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			_ = conn.Close()
			if !c.isCurrentGeneration(generation) {
				continue
			}
			return fmt.Errorf("auth write: %w", err)
		}

		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			_ = conn.Close()
			return fmt.Errorf("auth deadline: %w", err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			_ = conn.Close()
			if !c.isCurrentGeneration(generation) {
				continue
			}
			return fmt.Errorf("auth read: %w", err)
		}
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			_ = conn.Close()
			return fmt.Errorf("clear auth deadline: %w", err)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(data, &resp); err != nil {
			_ = conn.Close()
			return fmt.Errorf("auth parse: %w", err)
		}
		if resp["type"] == "error" {
			_ = conn.Close()
			if !c.isCurrentGeneration(generation) {
				continue
			}
			if payload, ok := resp["payload"]; ok {
				if encoded, marshalErr := json.Marshal(payload); marshalErr == nil {
					if remote, parsed := DecodeRemoteError(encoded, 0); parsed {
						return remote
					}
				}
			}
			return &RemoteError{Code: "unknown_remote_error", Message: fmt.Sprintf("authentication failed: %v", resp["payload"])}
		}
		if resp["type"] != "auth_response" {
			_ = conn.Close()
			return fmt.Errorf("auth failed: expected auth_response, got %v", resp["type"])
		}
		serverMode, _ := resp["transport_mode"].(string)
		if serverMode == "" {
			if payload, ok := resp["payload"].(map[string]interface{}); ok {
				serverMode, _ = payload["transport_mode"].(string)
			}
		}
		if serverMode != transportMode {
			_ = conn.Close()
			return fmt.Errorf("auth failed: transport_mode mismatch: configured %s, server %q", transportMode, serverMode)
		}
		negotiatedVersion := ""
		if payload, ok := resp["payload"].(map[string]interface{}); ok {
			negotiatedVersion, _ = payload["protocol_version"].(string)
			serverVersion, _ := payload["server_version"].(string)
			if serverVersion != "" {
				opslog.Info("daemon.connection", "version_negotiated", "component versions negotiated", "device_id", deviceID, "generation", generation, "daemon_version", buildinfo.Version, "server_version", serverVersion, "update_required", serverVersion != buildinfo.Version)
			}
		}
		if negotiatedVersion == "" {
			negotiatedVersion, _ = resp["protocol_version"].(string)
		}
		if negotiatedVersion == "" {
			// Backward support for pre-negotiation test/development servers. A real
			// v1.1 server sends the negotiated version in auth_response payload.
			negotiatedVersion = wire.CurrentProtocolVersion
		}
		if err := wire.ValidateNegotiatedVersion(negotiatedVersion); err != nil {
			_ = conn.Close()
			return fmt.Errorf("auth failed: %w", err)
		}

		c.dispatchMu.Lock()
		c.mu.Lock()
		if c.closed || !c.endpointActive || c.generation != generation || c.serverURL != serverURL {
			closed := c.closed
			c.mu.Unlock()
			c.dispatchMu.Unlock()
			_ = conn.Close()
			if closed {
				return fmt.Errorf("client closed")
			}
			// Endpoint changed while Dial/auth was in flight. Discard this
			// authenticated-but-stale socket and immediately try the new URL.
			continue
		}
		oldConn := c.conn
		c.conn = conn
		c.connected = true
		c.protocolVersion = negotiatedVersion
		c.reconnects = 0
		c.serviceRetries = 0
		c.retryableService = false
		c.retryAfter = 0
		onConnect := c.onConnect
		c.mu.Unlock()
		c.dispatchMu.Unlock()
		if oldConn != nil && oldConn != conn {
			_ = oldConn.Close()
		}

		opslog.Info("daemon.connection", "authenticated", "server authentication completed", "device_id", deviceID, "generation", generation)
		if onConnect != nil {
			// The callback is generation-bound. If an endpoint switch wins
			// before this goroutine starts, it is discarded; if the callback
			// starts first, SetServerURL waits for it to finish.
			go c.dispatchConnect(conn, onConnect)
		}
		return nil
	}
}

func remoteErrorFromHandshake(response *http.Response) *RemoteError {
	if response == nil || response.Body == nil {
		return nil
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxRemoteErrorBody+1))
	if err != nil || len(data) > maxRemoteErrorBody {
		return nil
	}
	remote, ok := DecodeRemoteError(data, response.StatusCode)
	if !ok {
		return nil
	}
	return remote
}

// ConnectUntilConnected makes initial provisioning resilient: a credential
// accepted by registration may keep retrying the same stable endpoint while
// the service provisions its tenant. Terminal and unknown errors stop.
func (c *Client) ConnectUntilConnected(ctx context.Context) error {
	for {
		err := c.Connect()
		if err == nil {
			return nil
		}
		if !c.recordConnectionFailure(err) {
			return err
		}
		if !c.waitForReconnect(ctx) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
}

func (c *Client) isCurrentGeneration(generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.endpointActive && c.generation == generation
}

func (c *Client) dispatchConnect(conn *websocket.Conn, onConnect func()) {
	c.dispatchMu.RLock()
	defer c.dispatchMu.RUnlock()
	c.mu.Lock()
	current := !c.closed && c.conn == conn
	c.mu.Unlock()
	if current {
		onConnect()
	}
}

// Send sends a message to the server.
func (c *Client) Send(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.endpointActive {
		return ErrEndpointPaused
	}
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.conn.WriteJSON(msg)
}

// ProtocolVersion returns the current connection generation's negotiated
// major.minor. Before authentication it returns the highest locally supported
// version used by the register_device frame.
func (c *Client) ProtocolVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.protocolVersion == "" {
		return wire.CurrentProtocolVersion
	}
	return c.protocolVersion
}

// StartReadLoop starts the read loop with auto-reconnect, respects context cancellation.
func (c *Client) StartReadLoop(ctx context.Context) {
	for {
		// Check if we should stop
		select {
		case <-ctx.Done():
			opslog.Info("daemon.connection", "read_loop_stopped", "connection read loop stopped", "reason", "context_done")
			return
		case <-c.closeCh:
			opslog.Info("daemon.connection", "read_loop_stopped", "connection read loop stopped", "reason", "closed")
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		deviceID := c.deviceID
		generation := c.generation
		c.mu.Unlock()

		if conn == nil {
			if !c.reconnect(ctx) {
				return // context cancelled during reconnect
			}
			continue
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-ctx.Done():
				return
			case <-c.closeCh:
				return
			default:
			}

			opslog.Error("daemon.connection", "read_failed", "connection read failed", err, "device_id", deviceID, "generation", generation)
			c.mu.Lock()
			wasCurrent := c.conn == conn
			if wasCurrent {
				c.conn = nil
				c.connected = false
				c.protocolVersion = wire.CurrentProtocolVersion
			}
			c.mu.Unlock()
			_ = conn.Close()

			if wasCurrent && !c.reconnect(ctx) {
				return
			}
			continue
		}

		c.dispatchMessage(conn, data)
	}
}

func (c *Client) dispatchMessage(conn *websocket.Conn, data []byte) {
	c.dispatchMu.RLock()
	defer c.dispatchMu.RUnlock()

	c.mu.Lock()
	// SetServerURL detaches the old socket before closing it. The read callback
	// runs under dispatchMu so the check and invocation are one linearizable
	// operation: an endpoint switch either happens before both or after both.
	if c.closed || c.conn != conn {
		c.mu.Unlock()
		return
	}
	onMessage := c.onMessage
	c.mu.Unlock()
	if onMessage != nil {
		onMessage(data)
	}
}

// StartHeartbeat sends periodic heartbeats until context is cancelled.
func (c *Client) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			msg := c.heartbeatMessage(time.Now())
			c.mu.Lock()
			deviceID := c.deviceID
			generation := c.generation
			c.mu.Unlock()
			if err := c.Send(msg); err != nil {
				if errors.Is(err, ErrEndpointPaused) {
					continue
				}
				opslog.Error("daemon.connection", "heartbeat_failed", "heartbeat send failed", err, "device_id", deviceID, "generation", generation)
			}
		case <-ctx.Done():
			opslog.Info("daemon.connection", "heartbeat_stopped", "heartbeat stopped")
			return
		case <-c.closeCh:
			return
		}
	}
}

func (c *Client) heartbeatMessage(now time.Time) map[string]interface{} {
	c.mu.Lock()
	deviceID := c.deviceID
	transportMode := c.transportMode
	protocolVersion := c.protocolVersion
	if protocolVersion == "" {
		protocolVersion = wire.CurrentProtocolVersion
	}
	c.mu.Unlock()
	return map[string]interface{}{
		"protocol_version": protocolVersion,
		"transport_mode":   transportMode,
		"type":             "heartbeat",
		"device_id":        deviceID,
		"timestamp":        now.Unix(),
	}
}

// Close gracefully closes the WebSocket connection.
func (c *Client) Close() {
	c.dispatchMu.Lock()
	defer c.dispatchMu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	c.generation++
	c.protocolVersion = wire.CurrentProtocolVersion
	close(c.closeCh)

	if c.conn != nil {
		// Send a close message to the server
		c.conn.SetWriteDeadline(time.Now().Add(readCloseTimeout))
		err := c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"),
		)
		if err != nil {
			opslog.Error("daemon.connection", "close_message_failed", "close message failed", err, "device_id", c.deviceID, "generation", c.generation)
		}
		c.conn.Close()
		c.conn = nil
		c.connected = false
	}

	opslog.Info("daemon.connection", "closed", "connection closed", "device_id", c.deviceID, "generation", c.generation)
}

// reconnect attempts to reconnect with exponential backoff.
// Returns false if context was cancelled.
func (c *Client) reconnect(ctx context.Context) bool {
	if !c.waitForReconnect(ctx) {
		return false
	}
	if err := c.Connect(); err != nil {
		return c.recordConnectionFailure(err)
	}
	return true
}

func (c *Client) recordConnectionFailure(err error) bool {
	if errors.Is(err, ErrEndpointPaused) {
		return true
	}
	c.mu.Lock()
	deviceID := c.deviceID
	generation := c.generation
	c.mu.Unlock()
	if remote := (*RemoteError)(nil); AsRemoteError(err, &remote) {
		if !remote.Retryable() {
			opslog.Error("daemon.connection", "reconnect_refused", "service rejected reconnect without a retryable error code", err, "device_id", deviceID, "generation", generation, "error_code", remote.Code)
			return false
		}
		c.mu.Lock()
		c.retryableService = true
		c.retryAfter = remote.RetryAfter
		c.mu.Unlock()
		opslog.Info("daemon.connection", "reconnect_service_retry", "service requested stable-endpoint retry", "device_id", deviceID, "generation", generation, "error_code", remote.Code)
		return true
	}
	c.mu.Lock()
	c.retryableService = false
	c.retryAfter = 0
	c.mu.Unlock()
	opslog.Error("daemon.connection", "reconnect_failed", "reconnect attempt failed", err, "device_id", deviceID, "generation", generation)
	return true
}

func (c *Client) waitForReconnect(ctx context.Context) bool {
	if !c.waitUntilEndpointActive(ctx) {
		return false
	}
	c.mu.Lock()
	serviceRetry := c.retryableService
	var attempt int
	if serviceRetry {
		c.serviceRetries++
		attempt = c.serviceRetries
	} else {
		c.reconnects++
		attempt = c.reconnects
	}
	retryAfter := c.retryAfter
	c.retryAfter = 0
	deviceID := c.deviceID
	generation := c.generation
	c.mu.Unlock()

	if !serviceRetry && attempt > maxReconnects {
		opslog.Error("daemon.connection", "reconnect_exhausted", "maximum generic reconnection attempts reached", nil, "device_id", deviceID, "generation", generation, "count", maxReconnects)
		return false
	}
	delay := time.Duration(attempt) * 5 * time.Second
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > maxReconnectDelay {
		delay = maxReconnectDelay
	}
	class := "network"
	if serviceRetry {
		class = "service"
	}
	opslog.Info("daemon.connection", "reconnect_scheduled", "reconnect scheduled", "device_id", deviceID, "generation", generation, "attempt", attempt, "class", class, "delay_ms", delay.Milliseconds())
	if c.retryWait != nil && !c.retryWait(ctx, delay) {
		return false
	}
	select {
	case <-c.closeCh:
		return false
	default:
		return true
	}
}

func (c *Client) waitUntilEndpointActive(ctx context.Context) bool {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return false
		}
		if c.endpointActive {
			c.mu.Unlock()
			return true
		}
		changed := c.endpointChanged
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return false
		case <-c.closeCh:
			return false
		case <-changed:
		}
	}
}
