package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EventHandler handles signaling events
type EventHandler interface {
	OnAuthenticated(payload AuthOKPayload)
	OnAuthError(payload AuthErrorPayload)
	OnAppRegistered(payload AppRegisteredPayload)
	OnOffer(sdp string, requestID string)
	OnAnswer(sdp string, appID string)
	OnICE(candidate json.RawMessage)
	OnError(message string)
	OnConnected()
	OnDisconnected()
}

// SignalingConfig configuration for SignalingClient
type SignalingConfig struct {
	ServerURL    string        // WebSocket URL (e.g., wss://example.com/ws/app)
	APIKey       string        // API key for authentication
	AppName      string        // Application name
	Capabilities []string      // App capabilities (e.g., ["print", "scrape"])
	Handler      EventHandler  // Event handler
	PingInterval time.Duration // Ping interval (default: 30s)
}

// SignalingClient manages WebSocket connection to signaling server
type SignalingClient struct {
	config          SignalingConfig
	conn            *websocket.Conn
	mu              sync.RWMutex
	isConnected     bool
	isAuthenticated bool
	appID           string
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}
}

// NewSignalingClient creates a new SignalingClient
func NewSignalingClient(config SignalingConfig) *SignalingClient {
	if config.PingInterval == 0 {
		config.PingInterval = 30 * time.Second
	}
	return &SignalingClient{
		config: config,
		done:   make(chan struct{}),
	}
}

// Connect establishes WebSocket connection and authenticates
func (c *SignalingClient) Connect(ctx context.Context) (err error) {
	log.Printf("[DEBUG-SIG] Connect() ENTRY")

	// Recover from panic
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[DEBUG-SIG] PANIC recovered: %v", r)
			err = fmt.Errorf("panic in SignalingClient.Connect: %v", r)
		}
	}()

	log.Printf("[DEBUG-SIG] SignalingClient.Connect() started, about to lock mutex")

	c.mu.Lock()
	log.Printf("[DEBUG-SIG] Mutex locked, checking isConnected=%v", c.isConnected)
	if c.isConnected {
		c.mu.Unlock()
		log.Printf("[DEBUG-SIG] Already connected, returning")
		return nil
	}

	log.Printf("[DEBUG-SIG] Creating context with cancel")
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()
	log.Printf("[DEBUG-SIG] Mutex unlocked")

	log.Printf("[DEBUG-SIG] Context created, parsing URL...")

	// Build URL with API key
	u, err := url.Parse(c.config.ServerURL)
	if err != nil {
		log.Printf("[DEBUG-SIG] URL parse error: %v", err)
		return fmt.Errorf("invalid server URL: %w", err)
	}
	q := u.Query()
	q.Set("apiKey", c.config.APIKey)
	u.RawQuery = q.Encode()

	log.Printf("[DEBUG-SIG] Attempting WebSocket dial to: %s", c.config.ServerURL)

	// Connect WebSocket
	conn, resp, err := websocket.DefaultDialer.DialContext(c.ctx, u.String(), nil)
	if err != nil {
		if resp != nil {
			log.Printf("[DEBUG-SIG] WebSocket dial failed: %v, HTTP status: %d", err, resp.StatusCode)
		} else {
			log.Printf("[DEBUG-SIG] WebSocket dial failed: %v (no HTTP response)", err)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	log.Printf("[DEBUG-SIG] WebSocket connected successfully")

	c.mu.Lock()
	c.conn = conn
	c.isConnected = true
	c.mu.Unlock()

	if c.config.Handler != nil {
		c.config.Handler.OnConnected()
	}

	// Start message handler
	go c.readPump()
	go c.pingPump()

	// Send auth message
	if err := c.sendAuth(); err != nil {
		c.Close()
		return fmt.Errorf("auth failed: %w", err)
	}

	return nil
}

// Close disconnects from the server
func (c *SignalingClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected {
		return nil
	}

	c.isConnected = false
	c.isAuthenticated = false

	if c.cancel != nil {
		c.cancel()
	}

	if c.conn != nil {
		// Send close message
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		err := c.conn.Close()
		c.conn = nil
		return err
	}

	return nil
}

// IsConnected returns connection status
func (c *SignalingClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected && c.isAuthenticated
}

// GetAppID returns the registered app ID
func (c *SignalingClient) GetAppID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.appID
}

// SendAnswer sends WebRTC answer SDP
func (c *SignalingClient) SendAnswer(sdp string, requestID string) error {
	payload := AnswerPayload{SDP: sdp}
	return c.sendMessage(MsgTypeAnswer, payload, requestID)
}

// SendICE sends ICE candidate
func (c *SignalingClient) SendICE(candidate json.RawMessage) error {
	payload := ICEPayload{Candidate: candidate}
	return c.sendMessage(MsgTypeICE, payload, "")
}

func (c *SignalingClient) sendAuth() error {
	payload := AuthPayload{APIKey: c.config.APIKey}
	return c.sendMessage(MsgTypeAuth, payload, "")
}

// RegisterApp registers the app with name and capabilities
func (c *SignalingClient) RegisterApp() error {
	payload := AppRegisterPayload{
		Name:         c.config.AppName,
		Capabilities: c.config.Capabilities,
	}
	return c.sendMessage(MsgTypeAppRegister, payload, "")
}

func (c *SignalingClient) sendMessage(msgType string, payload interface{}, requestID string) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload failed: %w", err)
	}

	msg := WSMessage{
		Type:      msgType,
		Payload:   payloadJSON,
		RequestID: requestID,
	}

	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("connection closed")
	}
	return c.conn.WriteMessage(websocket.TextMessage, msgJSON)
}

func (c *SignalingClient) readPump() {
	log.Printf("[DEBUG-SIG] readPump() started")
	defer func() {
		log.Printf("[DEBUG-SIG] readPump() exiting, calling OnDisconnected")
		c.mu.Lock()
		c.isConnected = false
		c.isAuthenticated = false
		c.mu.Unlock()
		if c.config.Handler != nil {
			c.config.Handler.OnDisconnected()
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("[DEBUG-SIG] readPump(): context done, returning")
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			log.Printf("[DEBUG-SIG] readPump(): conn is nil, returning")
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[DEBUG-SIG] readPump(): ReadMessage error: %v", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				if c.config.Handler != nil {
					c.config.Handler.OnError(fmt.Sprintf("websocket error: %v", err))
				}
			}
			return
		}

		log.Printf("[DEBUG-SIG] readPump(): received message (%d bytes)", len(message))
		c.handleMessage(message)
	}
}

func (c *SignalingClient) pingPump() {
	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()
			if conn == nil {
				return
			}

			c.mu.Lock()
			if c.conn != nil {
				c.conn.WriteMessage(websocket.PingMessage, nil)
			}
			c.mu.Unlock()
		}
	}
}

func (c *SignalingClient) handleMessage(data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		if c.config.Handler != nil {
			c.config.Handler.OnError(fmt.Sprintf("invalid message format: %v", err))
		}
		return
	}

	switch msg.Type {
	case MsgTypeAuthOK:
		var payload AuthOKPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			c.mu.Lock()
			c.isAuthenticated = true
			c.mu.Unlock()
			if c.config.Handler != nil {
				c.config.Handler.OnAuthenticated(payload)
			}
			// Auto-register app after auth
			c.RegisterApp()
		}

	case MsgTypeAuthError:
		var payload AuthErrorPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if c.config.Handler != nil {
				c.config.Handler.OnAuthError(payload)
			}
		}

	case MsgTypeAppRegistered:
		var payload AppRegisteredPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			c.mu.Lock()
			c.appID = payload.AppID
			c.mu.Unlock()
			if c.config.Handler != nil {
				c.config.Handler.OnAppRegistered(payload)
			}
		}

	case MsgTypeOffer:
		var payload OfferPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if c.config.Handler != nil {
				c.config.Handler.OnOffer(payload.SDP, msg.RequestID)
			}
		}

	case MsgTypeAnswer:
		var payload AnswerPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if c.config.Handler != nil {
				c.config.Handler.OnAnswer(payload.SDP, payload.AppID)
			}
		}

	case MsgTypeICE:
		var payload ICEPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if c.config.Handler != nil {
				c.config.Handler.OnICE(payload.Candidate)
			}
		}

	case MsgTypeError:
		var payload ErrorPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if c.config.Handler != nil {
				c.config.Handler.OnError(payload.Message)
			}
		}
	}
}
