package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxReconnects     = 50
	maxReconnectDelay = 60 * time.Second
	writeTimeout      = 10 * time.Second
	readCloseTimeout  = 3 * time.Second
)

// Client manages the WebSocket connection to the NekoNest server.
type Client struct {
	serverURL  string
	deviceID   string
	token      string
	conn       *websocket.Conn
	mu         sync.Mutex
	connectMu  sync.Mutex
	dispatchMu sync.RWMutex // linearizes callbacks against endpoint changes/Close
	onMessage  func([]byte)
	onConnect  func() // called after successful auth (initial + reconnect)
	connected  bool
	reconnects int
	closed     bool
	generation uint64        // incremented whenever the desired endpoint changes
	closeCh    chan struct{} // closed when Close() is called
}

// NewClient creates a new connection client.
func NewClient(ctx context.Context, serverURL, deviceID, token string) *Client {
	return &Client{
		serverURL: serverURL,
		deviceID:  deviceID,
		token:     token,
		closeCh:   make(chan struct{}),
	}
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
	c.dispatchMu.Lock()
	var oldConn *websocket.Conn
	defer func() {
		c.dispatchMu.Unlock()
		if oldConn != nil {
			_ = oldConn.Close()
		}
	}()

	c.mu.Lock()
	if !c.closed && c.serverURL != url {
		c.serverURL = url
		c.generation++
		c.reconnects = 0
		// Detach before close so an old read error cannot clear a connection that
		// has already authenticated against the new endpoint.
		oldConn = c.conn
		c.conn = nil
		c.connected = false
	}
	c.mu.Unlock()

	if publish != nil {
		publish()
	}
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
		generation := c.generation
		c.mu.Unlock()

		wsURL := serverURL + "/ws/daemon"
		log.Printf("[conn] connecting to %s", wsURL)

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			if !c.isCurrentGeneration(generation) {
				continue
			}
			return fmt.Errorf("dial: %w", err)
		}

		authMsg := map[string]interface{}{
			"type":      "register_device",
			"device_id": deviceID,
			"timestamp": time.Now().Unix(),
			"payload": map[string]interface{}{
				"device_id": deviceID,
				"token":     token,
			},
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
			return fmt.Errorf("auth failed: %v", resp["payload"])
		}

		c.dispatchMu.Lock()
		c.mu.Lock()
		if c.closed || c.generation != generation || c.serverURL != serverURL {
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
		c.reconnects = 0
		onConnect := c.onConnect
		c.mu.Unlock()
		c.dispatchMu.Unlock()
		if oldConn != nil && oldConn != conn {
			_ = oldConn.Close()
		}

		log.Printf("[conn] authenticated as %s", deviceID)
		if onConnect != nil {
			// The callback is generation-bound. If an endpoint switch wins
			// before this goroutine starts, it is discarded; if the callback
			// starts first, SetServerURL waits for it to finish.
			go c.dispatchConnect(conn, onConnect)
		}
		return nil
	}
}

func (c *Client) isCurrentGeneration(generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.generation == generation
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

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.conn.WriteJSON(msg)
}

// StartReadLoop starts the read loop with auto-reconnect, respects context cancellation.
func (c *Client) StartReadLoop(ctx context.Context) {
	for {
		// Check if we should stop
		select {
		case <-ctx.Done():
			log.Printf("[conn] read loop stopped (context done)")
			return
		case <-c.closeCh:
			log.Printf("[conn] read loop stopped (closed)")
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
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

			log.Printf("[conn] read error: %v", err)
			c.mu.Lock()
			wasCurrent := c.conn == conn
			if wasCurrent {
				c.conn = nil
				c.connected = false
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
			msg := map[string]interface{}{
				"type":      "heartbeat",
				"device_id": c.deviceID,
				"timestamp": time.Now().Unix(),
			}
			if err := c.Send(msg); err != nil {
				log.Printf("[conn] heartbeat error: %v", err)
			}
		case <-ctx.Done():
			log.Printf("[conn] heartbeat stopped")
			return
		case <-c.closeCh:
			return
		}
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
	close(c.closeCh)

	if c.conn != nil {
		// Send a close message to the server
		c.conn.SetWriteDeadline(time.Now().Add(readCloseTimeout))
		err := c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"),
		)
		if err != nil {
			log.Printf("[conn] close message error: %v", err)
		}
		c.conn.Close()
		c.conn = nil
		c.connected = false
	}

	log.Printf("[conn] connection closed")
}

// reconnect attempts to reconnect with exponential backoff.
// Returns false if context was cancelled.
func (c *Client) reconnect(ctx context.Context) bool {
	c.mu.Lock()
	c.reconnects++
	attempt := c.reconnects
	c.mu.Unlock()

	if attempt > maxReconnects {
		log.Printf("[conn] max reconnection attempts (%d) reached, giving up", maxReconnects)
		return false
	}

	delay := time.Duration(attempt) * 5 * time.Second
	if delay > maxReconnectDelay {
		delay = maxReconnectDelay
	}

	log.Printf("[conn] reconnecting in %v (attempt %d/%d)", delay, attempt, maxReconnects)

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return false
	case <-c.closeCh:
		return false
	}

	if err := c.Connect(); err != nil {
		log.Printf("[conn] reconnect failed: %v", err)
		return true // keep trying
	}
	return true
}
