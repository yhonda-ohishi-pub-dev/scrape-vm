package p2p

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
)

// DataChannelHandler handles data channel events
type DataChannelHandler interface {
	OnMessage(data []byte)
	OnOpen()
	OnClose()
}

// PeerConnection wraps pion/webrtc peer connection
type PeerConnection struct {
	pc              *webrtc.PeerConnection
	dataChannel     *webrtc.DataChannel
	signalingClient *SignalingClient
	handler         DataChannelHandler
	mu              sync.RWMutex
	pendingICE      []webrtc.ICECandidateInit
	requestID       string
}

// PeerConfig configuration for peer connection
type PeerConfig struct {
	ICEServers      []webrtc.ICEServer
	SignalingClient *SignalingClient
	Handler         DataChannelHandler
}

// NewPeerConnection creates a new WebRTC peer connection
func NewPeerConnection(config PeerConfig) (*PeerConnection, error) {
	// Default STUN servers if not provided
	iceServers := config.ICEServers
	if len(iceServers) == 0 {
		iceServers = []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}
	}

	rtcConfig := webrtc.Configuration{
		ICEServers: iceServers,
	}

	pc, err := webrtc.NewPeerConnection(rtcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	peer := &PeerConnection{
		pc:              pc,
		signalingClient: config.SignalingClient,
		handler:         config.Handler,
		pendingICE:      make([]webrtc.ICECandidateInit, 0),
	}

	// Handle ICE candidates
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}

		candidateJSON, err := json.Marshal(candidate.ToJSON())
		if err != nil {
			return
		}

		if peer.signalingClient != nil {
			peer.signalingClient.SendICE(candidateJSON)
		}
	})

	// Handle connection state changes
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			// Connection established
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			if peer.handler != nil {
				peer.handler.OnClose()
			}
		}
	})

	// Handle incoming data channels (for browser-initiated connections)
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		peer.setupDataChannel(dc)
	})

	return peer, nil
}

// HandleOffer processes an incoming SDP offer and returns an answer
func (p *PeerConnection) HandleOffer(sdp string, requestID string) error {
	p.mu.Lock()
	p.requestID = requestID
	p.mu.Unlock()

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}

	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	// Process pending ICE candidates
	p.mu.Lock()
	for _, candidate := range p.pendingICE {
		p.pc.AddICECandidate(candidate)
	}
	p.pendingICE = nil
	p.mu.Unlock()

	// Create answer
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("failed to create answer: %w", err)
	}

	if err := p.pc.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("failed to set local description: %w", err)
	}

	// Send answer via signaling
	if p.signalingClient != nil {
		if err := p.signalingClient.SendAnswer(answer.SDP, requestID); err != nil {
			return fmt.Errorf("failed to send answer: %w", err)
		}
	}

	return nil
}

// AddICECandidate adds an ICE candidate
func (p *PeerConnection) AddICECandidate(candidateJSON json.RawMessage) error {
	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
		return fmt.Errorf("failed to unmarshal candidate: %w", err)
	}

	// If remote description not set yet, queue the candidate
	if p.pc.RemoteDescription() == nil {
		p.mu.Lock()
		p.pendingICE = append(p.pendingICE, candidate)
		p.mu.Unlock()
		return nil
	}

	return p.pc.AddICECandidate(candidate)
}

// Send sends data through the data channel
func (p *PeerConnection) Send(data []byte) error {
	p.mu.RLock()
	dc := p.dataChannel
	p.mu.RUnlock()

	if dc == nil {
		return fmt.Errorf("data channel not available")
	}

	return dc.Send(data)
}

// SendText sends text data through the data channel
func (p *PeerConnection) SendText(text string) error {
	p.mu.RLock()
	dc := p.dataChannel
	p.mu.RUnlock()

	if dc == nil {
		return fmt.Errorf("data channel not available")
	}

	return dc.SendText(text)
}

// Close closes the peer connection
func (p *PeerConnection) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.dataChannel != nil {
		p.dataChannel.Close()
		p.dataChannel = nil
	}

	if p.pc != nil {
		return p.pc.Close()
	}

	return nil
}

func (p *PeerConnection) setupDataChannel(dc *webrtc.DataChannel) {
	p.mu.Lock()
	p.dataChannel = dc
	p.mu.Unlock()

	dc.OnOpen(func() {
		if p.handler != nil {
			p.handler.OnOpen()
		}
	})

	dc.OnClose(func() {
		if p.handler != nil {
			p.handler.OnClose()
		}
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if p.handler != nil {
			p.handler.OnMessage(msg.Data)
		}
	})
}

// ConnectionState returns the current connection state
func (p *PeerConnection) ConnectionState() webrtc.PeerConnectionState {
	if p.pc == nil {
		return webrtc.PeerConnectionStateClosed
	}
	return p.pc.ConnectionState()
}

// DataChannel returns the underlying WebRTC data channel
// Returns nil if the data channel hasn't been established yet
func (p *PeerConnection) DataChannel() *webrtc.DataChannel {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dataChannel
}

// IsConnected returns whether the data channel is open and ready
func (p *PeerConnection) IsConnected() bool {
	p.mu.RLock()
	dc := p.dataChannel
	p.mu.RUnlock()

	if dc == nil {
		return false
	}
	return dc.ReadyState() == webrtc.DataChannelStateOpen
}
