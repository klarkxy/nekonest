package protocol

import "time"

// MessageType defines the type of messages in the NekoNest protocol.
type MessageType string

const (
	MsgDeviceOnline      MessageType = "device_online"
	MsgDeviceOffline     MessageType = "device_offline"
	MsgDeviceList        MessageType = "device_list"
	MsgSessionList       MessageType = "session_list"
	MsgSessionUpdate     MessageType = "session_update"
	MsgSessionMessage    MessageType = "session_message" // new: real-time output streaming
	MsgSendPrompt        MessageType = "send_prompt"
	MsgPromptStatusQuery MessageType = "prompt_status_query"
	MsgPromptNotSeen     MessageType = "prompt_not_seen"
	MsgPromptAccepted    MessageType = "prompt_accepted"
	MsgPromptCommitted   MessageType = "prompt_committed"
	MsgPromptFailed      MessageType = "prompt_failed"
	MsgPromptSent        MessageType = "prompt_sent"
	MsgApprove           MessageType = "approve"
	MsgDeny              MessageType = "deny"
	MsgInterrupt         MessageType = "interrupt"
	MsgHeartbeat         MessageType = "heartbeat"
	MsgError             MessageType = "error"
	MsgRegisterDevice    MessageType = "register_device"
	MsgAuthResponse      MessageType = "auth_response"
	MsgPairRequest       MessageType = "pair_request"
	MsgPairConfirm       MessageType = "pair_confirm"
	MsgSubscribe         MessageType = "subscribe" // phone subscribes to a device
	MsgSubscribeAck      MessageType = "subscribe_ack"
	MsgFetchHistory      MessageType = "fetch_history"   // phone asks daemon for PC-side transcript
	MsgSessionHistory    MessageType = "session_history" // daemon returns imported transcript
)

// NekoMessage is the JSON envelope for all communication.
type NekoMessage struct {
	Type      MessageType    `json:"type"`
	DeviceID  string         `json:"device_id"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// Device represents a registered Windows device.
type Device struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OS           string `json:"os"`
	Status       string `json:"status"` // "online" | "offline"
	LastSeen     int64  `json:"last_seen"`
	ActiveAgents int    `json:"active_agents"`
	Token        string `json:"-"` // device auth token, not sent to clients
}

// AgentStatus represents the status of an AI coding agent.
type AgentStatus string

const (
	AgentRunning         AgentStatus = "running"
	AgentIdle            AgentStatus = "idle"
	AgentWaitingApproval AgentStatus = "waiting_approval"
)

// AgentType represents the type of AI agent.
type AgentType string

const (
	AgentClaudeCode AgentType = "claude_code"
	AgentCodex      AgentType = "codex"
	AgentKilo       AgentType = "kilo"
	AgentKimiCLI    AgentType = "kimi_cli"
	AgentGrokBuild  AgentType = "grok_build"
)

// AgentSession represents an active agent session on a device.
type AgentSession struct {
	ID              string           `json:"id"`
	DeviceID        string           `json:"device_id"`
	AgentType       AgentType        `json:"agent_type"`
	Status          AgentStatus      `json:"status"`
	Summary         string           `json:"summary"`
	LastActivity    int64            `json:"last_activity"`
	ProjectDir      string           `json:"project_dir,omitempty"` // full path on PC
	Project         string           `json:"project,omitempty"`     // short folder name
	PendingApproval *PendingApproval `json:"pending_approval,omitempty"`
}

// PendingApproval represents a tool call awaiting user approval.
type PendingApproval struct {
	ID          string         `json:"id"`
	ToolName    string         `json:"tool_name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// SessionMessage represents a message in an agent conversation.
type SessionMessage struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"` // "assistant" | "user" | "tool" | "system"
	Content   string         `json:"content"`
	Type      string         `json:"type,omitempty"` // "thinking" | "text" | "tool_call" | "tool_result" | "error"
	Timestamp int64          `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// PairCode represents a temporary pairing code for device binding.
type PairCode struct {
	Code      string    `json:"code"`
	DeviceID  string    `json:"device_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"-"`
}

// NewMessage creates a new NekoMessage with the current timestamp.
func NewMessage(msgType MessageType, deviceID string) *NekoMessage {
	return &NekoMessage{
		Type:      msgType,
		DeviceID:  deviceID,
		Timestamp: time.Now().Unix(),
	}
}

// NewMessageWithSession creates a session-scoped message.
func NewMessageWithSession(msgType MessageType, deviceID, sessionID string) *NekoMessage {
	return &NekoMessage{
		Type:      msgType,
		DeviceID:  deviceID,
		SessionID: sessionID,
		Timestamp: time.Now().Unix(),
	}
}
