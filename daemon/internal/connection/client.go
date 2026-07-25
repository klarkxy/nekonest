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
	onMessage  func([]byte)
	connected  bool
	reconnects int
	closed     bool
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
	c.onMessage = fn
}

// SetServerURL updates the server URL (for config hot-reload).
func (c *Client) SetServerURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serverURL = url
	// Close existing connection to trigger reconnect with new URL
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.connected = false
	}
}

// Connect establishes the WebSocket connection and authenticates.
func (c *Client) Connect() error {
	c.mu.Lock()
	serverURL := c.serverURL
	c.mu.Unlock()

	wsURL := serverURL + "/ws/daemon"
	log.Printf("[conn] connecting to %s", wsURL)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	// Send auth message
	authMsg := map[string]interface{}{
		"type":      "register_device",
		"device_id": c.deviceID,
		"timestamp": time.Now().Unix(),
		"payload": map[string]interface{}{
			"device_id": c.deviceID,
			"token":     c.token,
		},
	}

	if err := conn.WriteJSON(authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("auth write: %w", err)
	}

	// Check auth response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("auth read: %w", err)
	}

	// Clear read deadline after auth
	conn.SetReadDeadline(time.Time{})

	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		conn.Close()
		return fmt.Errorf("auth parse: %w", err)
	}
	if resp["type"] == "error" {
		conn.Close()
		return fmt.Errorf("auth failed: %v", resp["payload"])
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.reconnects = 0
	c.mu.Unlock()

	log.Printf("[conn] authenticated as %s", c.deviceID)
	return nil
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
			c.conn = nil
			c.connected = false
			c.mu.Unlock()
			conn.Close()

			if !c.reconnect(ctx) {
				return
			}
			continue
		}

		if c.onMessage != nil {
			c.onMessage(data)
		}
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
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
