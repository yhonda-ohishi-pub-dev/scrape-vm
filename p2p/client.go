package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// ClientEventHandler handles P2P client events
type ClientEventHandler interface {
	OnP2PConnected()
	OnP2PDisconnected()
	OnP2PMessage(data []byte)
	OnP2PError(err error)
}

// DataChannelReadyCallback is called when DataChannel is ready (for grpcweb transport setup)
type DataChannelReadyCallback func(dc *webrtc.DataChannel)

// Client integrates SignalingClient and PeerConnection for P2P communication
type Client struct {
	config            *ClientConfig
	signaling         *SignalingClient
	peer              *PeerConnection
	logger            *log.Logger
	handler           ClientEventHandler
	dcReadyCallback   DataChannelReadyCallback
	mu                sync.RWMutex
	connected         bool
	registered        bool
	ctx               context.Context
	cancel            context.CancelFunc
}

// ClientConfig holds configuration for P2P Client
type ClientConfig struct {
	SignalingURL          string   // WebSocket URL (e.g., wss://example.com/ws/app)
	APIKey                string   // API key for authentication
	AppName               string   // Application name
	Capabilities          []string // App capabilities
	ICEServers            []webrtc.ICEServer
	Logger                *log.Logger
	Handler               ClientEventHandler       // Optional event handler
	OnDataChannelReady    DataChannelReadyCallback // Called when DataChannel is ready
}

// NewClient creates a new P2P Client
func NewClient(config *ClientConfig) *Client {
	logger := config.Logger
	if logger == nil {
		logger = log.Default()
	}

	return &Client{
		config:          config,
		logger:          logger,
		handler:         config.Handler,
		dcReadyCallback: config.OnDataChannelReady,
	}
}

// Connect connects to signaling server and waits for WebRTC connection
func (c *Client) Connect(ctx context.Context) (err error) {
	// Recover from panic
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Connect: %v", r)
			c.logger.Printf("PANIC recovered: %v", r)
		}
	}()

	c.mu.Lock()
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	c.logger.Printf("Starting P2P client...")

	// Create signaling client with clientEventAdapter as the event handler
	c.signaling = NewSignalingClient(SignalingConfig{
		ServerURL:    c.config.SignalingURL,
		APIKey:       c.config.APIKey,
		AppName:      c.config.AppName,
		Capabilities: c.config.Capabilities,
		Handler:      &signalingEventAdapter{client: c},
	})

	// Connect to signaling server
	if err := c.signaling.Connect(c.ctx); err != nil {
		return fmt.Errorf("failed to connect to signaling server: %w", err)
	}

	c.logger.Printf("Connected to signaling server, waiting for browser connection...")
	return nil
}

// signalingEventAdapter adapts Client to EventHandler for SignalingClient
type signalingEventAdapter struct {
	client *Client
}

func (a *signalingEventAdapter) OnAuthenticated(payload AuthOKPayload) {
	a.client.logger.Printf("Authenticated: userId=%s, type=%s", payload.UserID, payload.Type)
}

func (a *signalingEventAdapter) OnAuthError(payload AuthErrorPayload) {
	a.client.logger.Printf("Auth error: %s", payload.Error)
	if a.client.handler != nil {
		a.client.handler.OnP2PError(fmt.Errorf("auth error: %s", payload.Error))
	}
}

func (a *signalingEventAdapter) OnAppRegistered(payload AppRegisteredPayload) {
	a.client.logger.Printf("App registered: appId=%s", payload.AppID)
	a.client.mu.Lock()
	a.client.registered = true
	a.client.mu.Unlock()

	// Create WebRTC peer connection after registration
	a.client.createPeerConnection()
}

func (a *signalingEventAdapter) OnOffer(sdp string, requestID string) {
	a.client.logger.Printf("Received offer from browser (requestID: %s)", requestID)

	a.client.mu.Lock()
	peer := a.client.peer
	a.client.mu.Unlock()

	// 既存の接続が閉じている場合は新しいPeerConnectionを作成
	if peer == nil || peer.ConnectionState() == webrtc.PeerConnectionStateClosed ||
		peer.ConnectionState() == webrtc.PeerConnectionStateFailed {
		a.client.logger.Printf("Creating new peer connection...")
		// 古い接続があればクリーンアップ
		if peer != nil {
			peer.Close()
		}
		a.client.createPeerConnection()
	}

	a.client.mu.RLock()
	peer = a.client.peer
	a.client.mu.RUnlock()

	if peer == nil {
		a.client.logger.Printf("Failed to create peer connection")
		return
	}

	if err := peer.HandleOffer(sdp, requestID); err != nil {
		a.client.logger.Printf("Failed to handle offer: %v", err)
		if a.client.handler != nil {
			a.client.handler.OnP2PError(fmt.Errorf("failed to handle offer: %w", err))
		}
	}
}

func (a *signalingEventAdapter) OnAnswer(sdp string, appID string) {
	// App doesn't receive answers (only browser does)
	a.client.logger.Printf("Received unexpected answer from appId=%s", appID)
}

func (a *signalingEventAdapter) OnICE(candidate json.RawMessage) {
	if a.client.peer == nil {
		a.client.logger.Printf("Received ICE candidate but peer not initialized")
		return
	}

	if err := a.client.peer.AddICECandidate(candidate); err != nil {
		a.client.logger.Printf("Failed to add ICE candidate: %v", err)
	}
}

func (a *signalingEventAdapter) OnError(message string) {
	a.client.logger.Printf("Signaling error: %s", message)
	if a.client.handler != nil {
		a.client.handler.OnP2PError(fmt.Errorf("signaling error: %s", message))
	}
}

func (a *signalingEventAdapter) OnConnected() {
	a.client.logger.Printf("Signaling connected")
}

func (a *signalingEventAdapter) OnDisconnected() {
	a.client.logger.Printf("Signaling disconnected")
	a.client.mu.Lock()
	a.client.connected = false
	a.client.registered = false
	a.client.mu.Unlock()

	if a.client.handler != nil {
		a.client.handler.OnP2PDisconnected()
	}
}

// dataChannelEventAdapter adapts Client to DataChannelHandler
type dataChannelEventAdapter struct {
	client *Client
}

func (a *dataChannelEventAdapter) OnMessage(data []byte) {
	if a.client.handler != nil {
		a.client.handler.OnP2PMessage(data)
	}
}

func (a *dataChannelEventAdapter) OnOpen() {
	a.client.logger.Printf("P2P connection established!")
	a.client.mu.Lock()
	a.client.connected = true
	a.client.mu.Unlock()

	// Call DataChannelReady callback for grpcweb transport setup
	if a.client.dcReadyCallback != nil {
		a.client.mu.RLock()
		peer := a.client.peer
		a.client.mu.RUnlock()
		if peer != nil {
			if dc := peer.DataChannel(); dc != nil {
				a.client.dcReadyCallback(dc)
			}
		}
	}

	if a.client.handler != nil {
		a.client.handler.OnP2PConnected()
	}
}

func (a *dataChannelEventAdapter) OnClose() {
	a.client.logger.Printf("Data channel closed")
	a.client.mu.Lock()
	a.client.connected = false
	a.client.mu.Unlock()

	if a.client.handler != nil {
		a.client.handler.OnP2PDisconnected()
	}
}

func (c *Client) createPeerConnection() {
	iceServers := c.config.ICEServers
	if len(iceServers) == 0 {
		iceServers = []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}
	}

	peer, err := NewPeerConnection(PeerConfig{
		ICEServers:      iceServers,
		SignalingClient: c.signaling,
		Handler:         &dataChannelEventAdapter{client: c},
	})
	if err != nil {
		c.logger.Printf("Failed to create peer connection: %v", err)
		if c.handler != nil {
			c.handler.OnP2PError(fmt.Errorf("failed to create peer connection: %w", err))
		}
		return
	}

	c.mu.Lock()
	c.peer = peer
	c.mu.Unlock()
}

// SendMessage sends data through WebRTC data channel
func (c *Client) SendMessage(data []byte) error {
	c.mu.RLock()
	peer := c.peer
	c.mu.RUnlock()

	if peer == nil {
		return fmt.Errorf("peer connection not initialized")
	}
	return peer.Send(data)
}

// SendText sends text through WebRTC data channel
func (c *Client) SendText(text string) error {
	c.mu.RLock()
	peer := c.peer
	c.mu.RUnlock()

	if peer == nil {
		return fmt.Errorf("peer connection not initialized")
	}
	return peer.SendText(text)
}

// SendJSON sends JSON data through WebRTC data channel
func (c *Client) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return c.SendMessage(data)
}

// IsConnected returns whether P2P connection is established
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// WaitForConnection waits for P2P connection with timeout
func (c *Client) WaitForConnection(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		if c.IsConnected() {
			return nil
		}
		select {
		case <-ticker.C:
		case <-c.ctx.Done():
			return fmt.Errorf("context cancelled")
		}
	}

	return fmt.Errorf("connection timeout after %v", timeout)
}

// Close closes P2P connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	var errs []error

	if c.peer != nil {
		if err := c.peer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("peer close: %w", err))
		}
		c.peer = nil
	}

	if c.signaling != nil {
		if err := c.signaling.Close(); err != nil {
			errs = append(errs, fmt.Errorf("signaling close: %w", err))
		}
		c.signaling = nil
	}

	c.connected = false
	c.registered = false

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetAppID returns the registered app ID from signaling server
func (c *Client) GetAppID() string {
	if c.signaling == nil {
		return ""
	}
	return c.signaling.GetAppID()
}

// GetConnectionState returns WebRTC connection state
func (c *Client) GetConnectionState() string {
	c.mu.RLock()
	peer := c.peer
	c.mu.RUnlock()

	if peer == nil {
		return "not initialized"
	}
	return peer.ConnectionState().String()
}
