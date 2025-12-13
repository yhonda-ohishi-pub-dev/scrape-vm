package p2p

import "encoding/json"

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	RequestID string          `json:"requestId,omitempty"`
}

// AuthPayload for auth message
type AuthPayload struct {
	APIKey string `json:"apiKey,omitempty"`
	Token  string `json:"token,omitempty"`
}

// AuthOKPayload response from successful auth
type AuthOKPayload struct {
	UserID string `json:"userId"`
	Type   string `json:"type"` // "browser" or "app"
}

// AuthErrorPayload response from failed auth
type AuthErrorPayload struct {
	Error string `json:"error"`
}

// AppRegisterPayload for app registration
type AppRegisterPayload struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// AppRegisteredPayload response from successful registration
type AppRegisteredPayload struct {
	AppID string `json:"appId"`
}

// AppStatusPayload for app status updates
type AppStatusPayload struct {
	AppID        string   `json:"appId"`
	Name         string   `json:"name,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Status       string   `json:"status"` // "online" or "offline"
}

// OfferPayload for WebRTC offer
type OfferPayload struct {
	SDP         string `json:"sdp"`
	TargetAppID string `json:"targetAppId,omitempty"` // Used when browser sends to app
}

// AnswerPayload for WebRTC answer
type AnswerPayload struct {
	SDP   string `json:"sdp"`
	AppID string `json:"appId,omitempty"` // Included when sent to browser
}

// ICEPayload for ICE candidate exchange
type ICEPayload struct {
	Candidate   json.RawMessage `json:"candidate"`
	TargetAppID string          `json:"targetAppId,omitempty"`
	AppID       string          `json:"appId,omitempty"`
}

// ErrorPayload for error messages
type ErrorPayload struct {
	Message string `json:"message"`
}

// AppsListPayload for apps list response
type AppsListPayload struct {
	Apps []AppInfo `json:"apps"`
}

// AppInfo represents app information
type AppInfo struct {
	AppID  string `json:"appId"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Message types
const (
	// Auth
	MsgTypeAuth      = "auth"
	MsgTypeAuthOK    = "auth_ok"
	MsgTypeAuthError = "auth_error"

	// App management
	MsgTypeAppRegister   = "app_register"
	MsgTypeAppRegistered = "app_registered"
	MsgTypeAppStatus     = "app_status"
	MsgTypeGetApps       = "get_apps"
	MsgTypeAppsList      = "apps_list"

	// WebRTC signaling
	MsgTypeOffer  = "offer"
	MsgTypeAnswer = "answer"
	MsgTypeICE    = "ice"

	// Error
	MsgTypeError = "error"
)
