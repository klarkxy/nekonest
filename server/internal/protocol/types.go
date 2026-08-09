package protocol

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CurrentProtocolVersion is the server's advertised major.minor wire version.
const CurrentProtocolVersion = "1.1"

// CurrentProtocolMajor is the major component of CurrentProtocolVersion.
const CurrentProtocolMajor = 1

// MessageType defines the type of messages in the NekoNest protocol.
type MessageType string

const (
	MsgDeviceOnline        MessageType = "device_online"
	MsgDeviceOffline       MessageType = "device_offline"
	MsgDeviceList          MessageType = "device_list"
	MsgSessionList         MessageType = "session_list"
	MsgSessionUpdate       MessageType = "session_update"
	MsgSessionMessage      MessageType = "session_message"
	MsgSendPrompt          MessageType = "send_prompt"
	MsgPromptStatusQuery   MessageType = "prompt_status_query"
	MsgPromptNotSeen       MessageType = "prompt_not_seen"
	MsgPromptQueued        MessageType = "prompt_queued"
	MsgPromptAccepted      MessageType = "prompt_accepted"
	MsgPromptCommitted     MessageType = "prompt_committed"
	MsgPromptFailed        MessageType = "prompt_failed"
	MsgPromptSent          MessageType = "prompt_sent" // deprecated: clear outbox on prompt_committed
	MsgApprove             MessageType = "approve"
	MsgDeny                MessageType = "deny"
	MsgInterrupt           MessageType = "interrupt"
	MsgSteer               MessageType = "steer"
	MsgRespondUserInput    MessageType = "respond_user_input"
	MsgUserInputResult     MessageType = "user_input_result"
	MsgQueueUpdate         MessageType = "queue_update"
	MsgCancelPrompt        MessageType = "cancel_prompt"
	MsgPromptCancelled     MessageType = "prompt_cancelled"
	MsgResumePromptQueue   MessageType = "resume_prompt_queue"
	MsgStartThread         MessageType = "start_thread"
	MsgThreadStarting      MessageType = "thread_starting"
	MsgThreadOwned         MessageType = "thread_owned"
	MsgThreadFailed        MessageType = "thread_failed"
	MsgThreadIndeterminate MessageType = "thread_indeterminate"
	MsgHeartbeat           MessageType = "heartbeat"
	MsgError               MessageType = "error"
	MsgRegisterDevice      MessageType = "register_device"
	MsgAuthResponse        MessageType = "auth_response"
	MsgPairRequest         MessageType = "pair_request"
	MsgPairConfirm         MessageType = "pair_confirm"
	MsgPairReady           MessageType = "pair_ready"
	MsgPairFailed          MessageType = "pair_failed"
	MsgKeyPackage          MessageType = "key_package"
	MsgPhoneRevoked        MessageType = "phone_revoked"
	MsgAttentionEvent      MessageType = "attention_event"
	MsgSubscribe           MessageType = "subscribe"
	MsgSubscribeAck        MessageType = "subscribe_ack"
	MsgFetchHistory        MessageType = "fetch_history"
	MsgSessionHistory      MessageType = "session_history"
)

// Stable handshake / negotiation error codes (payload.error_code).
const (
	ErrCodeVersionMismatch       = "version_mismatch"
	ErrCodeTransportModeMismatch = "transport_mode_mismatch"
	ErrCodeInvalidEnvelope       = "invalid_envelope"
)

// TransportMode is the nest-wide wire mode.
type TransportMode string

const (
	TransportSealed TransportMode = "sealed"
	TransportOpen   TransportMode = "open"
)

// KeyScope identifies which key encrypts a sealed payload.
type KeyScope string

const (
	KeyScopeDeviceCatalog KeyScope = "device_catalog"
	KeyScopeSession       KeyScope = "session"
)

// ControlMode describes how the daemon drives an agent session.
type ControlMode string

const (
	ControlAppServer     ControlMode = "app_server"
	ControlExecResume    ControlMode = "exec_resume"
	ControlCompatibility ControlMode = "compatibility"
)

// AttachmentMode describes attachment support for a session.
type AttachmentMode string

const (
	AttachmentNativeImageAndFile AttachmentMode = "native_image_and_file"
	AttachmentNativeImage        AttachmentMode = "native_image"
	AttachmentPathBestEffort     AttachmentMode = "path_best_effort"
	AttachmentUnsupported        AttachmentMode = "unsupported"
)

// SealedPayload is the ciphertext envelope for sealed transport.
// Crypto material is not validated here; phase-1 only defines the wire shape.
type SealedPayload struct {
	Alg         string   `json:"alg"`
	Version     int      `json:"version,omitempty"`
	KeyScope    KeyScope `json:"key_scope"`
	Epoch       uint64   `json:"epoch"`
	SenderID    string   `json:"sender_id"`
	RecipientID string   `json:"recipient_id"`
	Sequence    uint64   `json:"sequence"`
	Nonce       string   `json:"nonce"`
	Ciphertext  string   `json:"ciphertext"`
}

// NekoMessage is the JSON envelope for all communication.
type NekoMessage struct {
	ProtocolVersion string         `json:"protocol_version,omitempty"`
	TransportMode   TransportMode  `json:"transport_mode,omitempty"`
	Type            MessageType    `json:"type"`
	DeviceID        string         `json:"device_id"`
	SessionID       string         `json:"session_id,omitempty"`
	ClientMsgID     string         `json:"client_msg_id,omitempty"`
	Outcome         string         `json:"outcome,omitempty"`
	RetryAllowed    *bool          `json:"retry_allowed,omitempty"`
	Timestamp       int64          `json:"timestamp"`
	Payload         map[string]any `json:"payload,omitempty"`
	SealedPayload   *SealedPayload `json:"sealed_payload,omitempty"`
}

// Device represents a registered host device.
type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OS            string `json:"os"`
	Status        string `json:"status"` // "online" | "offline"
	LastSeen      int64  `json:"last_seen"`
	ActiveAgents  int    `json:"active_agents"`
	DaemonVersion string `json:"daemon_version,omitempty"`
	Token         string `json:"-"` // device auth token, not sent to clients
}

// AgentStatus represents the status of an AI coding agent.
type AgentStatus string

const (
	AgentIdle            AgentStatus = "idle"
	AgentRunning         AgentStatus = "running"
	AgentWaitingUser     AgentStatus = "waiting_user"
	AgentWaitingApproval AgentStatus = "waiting_approval"
	AgentError           AgentStatus = "error"
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

// AgentStartCapability is a device-scoped native thread-creation capability.
// A missing entry, an unknown agent, or Available=false must be treated as
// unavailable by consumers.
type AgentStartCapability struct {
	AgentType      AgentType `json:"agent_type"`
	Available      bool      `json:"available"`
	Spawn          bool      `json:"spawn"`
	Reason         string    `json:"reason,omitempty"`
	ControlPath    string    `json:"control_path,omitempty"`
	ControlVersion string    `json:"control_version,omitempty"`
}

// SessionListPayload is the optional structured payload for session_list.
// A missing StartCapabilities catalog means phone-side creation is unsupported.
type SessionListPayload struct {
	Sessions          []AgentSession         `json:"sessions,omitempty"`
	StartCapabilities []AgentStartCapability `json:"start_capabilities,omitempty"`
}

// SessionCapabilities advertises per-session control surface.
// Absent / zero values mean unsupported (false).
type SessionCapabilities struct {
	ControlMode    ControlMode    `json:"control_mode,omitempty"`
	Approve        bool           `json:"approve,omitempty"`
	Deny           bool           `json:"deny,omitempty"`
	Interrupt      bool           `json:"interrupt,omitempty"`
	Steer          bool           `json:"steer,omitempty"`
	Queue          bool           `json:"queue,omitempty"`
	Spawn          bool           `json:"spawn,omitempty"`
	AttachmentMode AttachmentMode `json:"attachment_mode,omitempty"`
}

// Normalize applies defaults for absent capability fields.
func (c *SessionCapabilities) Normalize() {
	if c == nil {
		return
	}
	if c.AttachmentMode == "" {
		c.AttachmentMode = AttachmentUnsupported
	}
	if c.ControlMode == "" {
		c.ControlMode = ControlCompatibility
	}
}

// CapabilityBool returns false when caps is nil (absent = unsupported).
func CapabilityBool(caps *SessionCapabilities, get func(*SessionCapabilities) bool) bool {
	if caps == nil {
		return false
	}
	return get(caps)
}

// AgentSession represents an active agent session on a device.
type AgentSession struct {
	ID               string               `json:"id"`
	DeviceID         string               `json:"device_id"`
	AgentType        AgentType            `json:"agent_type"`
	Status           AgentStatus          `json:"status"`
	Summary          string               `json:"summary"`
	LastActivity     int64                `json:"last_activity"`
	ProjectDir       string               `json:"project_dir,omitempty"`
	Project          string               `json:"project,omitempty"`
	Capabilities     *SessionCapabilities `json:"capabilities,omitempty"`
	PendingApproval  *PendingApproval     `json:"pending_approval,omitempty"`
	PendingUserInput *PendingUserInput    `json:"pending_user_input,omitempty"`
}

// PendingApproval represents a tool call awaiting user approval.
type PendingApproval struct {
	ID          string         `json:"id"`
	ToolName    string         `json:"tool_name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type UserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type UserInputQuestion struct {
	ID       string            `json:"id"`
	Header   string            `json:"header"`
	Question string            `json:"question"`
	Options  []UserInputOption `json:"options,omitempty"`
	IsOther  bool              `json:"is_other,omitempty"`
	IsSecret bool              `json:"is_secret,omitempty"`
}

// PendingUserInput is independent from approval. ExpiresAt is Unix milliseconds.
type PendingUserInput struct {
	RequestID        string              `json:"request_id"`
	ItemID           string              `json:"item_id"`
	Questions        []UserInputQuestion `json:"questions"`
	AutoResolutionMS *uint64             `json:"auto_resolution_ms,omitempty"`
	ExpiresAt        int64               `json:"expires_at,omitempty"`
}

// AttachmentRef identifies a phone-uploaded attachment for a first prompt.
type AttachmentRef struct {
	ID   string `json:"id,omitempty"`
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
	MIME string `json:"mime,omitempty"`
	Size int64  `json:"size,omitempty"`
	Key  string `json:"key,omitempty"`
}

// StartThreadPayload creates a native agent thread from a phone-local draft.
// Prompt and ProjectDir are canonical; CWD and InitialPrompt are legacy
// aliases accepted by receivers during the minor-version transition.
type StartThreadPayload struct {
	AgentType     AgentType       `json:"agent_type"`
	OperationID   string          `json:"operation_id"`
	ProjectDir    string          `json:"project_dir"`
	CWD           string          `json:"cwd,omitempty"`
	Prompt        string          `json:"prompt"`
	InitialPrompt string          `json:"initial_prompt,omitempty"`
	Attachments   []AttachmentRef `json:"attachments,omitempty"`
}

// ThreadStartState is the fail-closed native thread-start lifecycle.
type ThreadStartState string

const (
	ThreadStartStarting      ThreadStartState = "thread_starting"
	ThreadStartOwned         ThreadStartState = "thread_owned"
	ThreadStartFailed        ThreadStartState = "thread_failed"
	ThreadStartIndeterminate ThreadStartState = "thread_indeterminate"
)

// ThreadStartResult is the structured payload for thread_* lifecycle messages.
// ThreadOwned is only emitted after positive native-store ownership.
type ThreadStartResult struct {
	AgentType      AgentType        `json:"agent_type"`
	OperationID    string           `json:"operation_id"`
	State          ThreadStartState `json:"state"`
	Session        *AgentSession    `json:"session,omitempty"`
	SessionID      string           `json:"session_id,omitempty"`
	ThreadID       string           `json:"thread_id,omitempty"`
	PromptAccepted bool             `json:"prompt_accepted,omitempty"`
	Reason         string           `json:"reason,omitempty"`
	Error          string           `json:"error,omitempty"`
	Message        string           `json:"message,omitempty"`
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

// VersionParts holds parsed major.minor components.
type VersionParts struct {
	Major int
	Minor int
}

// ParseProtocolVersion parses a "major.minor" string.
func ParseProtocolVersion(v string) (VersionParts, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return VersionParts{}, fmt.Errorf("protocol_version required")
	}
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return VersionParts{}, fmt.Errorf("protocol_version must be major.minor")
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return VersionParts{}, fmt.Errorf("invalid protocol major")
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return VersionParts{}, fmt.Errorf("invalid protocol minor")
	}
	// Reject padded forms like "01.0".
	if parts[0] != strconv.Itoa(major) || parts[1] != strconv.Itoa(minor) {
		return VersionParts{}, fmt.Errorf("invalid protocol_version format")
	}
	return VersionParts{Major: major, Minor: minor}, nil
}

// FormatProtocolVersion formats major.minor.
func FormatProtocolVersion(major, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
}

// ParseTransportMode validates sealed|open.
func ParseTransportMode(raw string) (TransportMode, error) {
	switch TransportMode(strings.TrimSpace(raw)) {
	case TransportSealed:
		return TransportSealed, nil
	case TransportOpen:
		return TransportOpen, nil
	case "":
		return "", fmt.Errorf("transport_mode required")
	default:
		return "", fmt.Errorf("invalid transport_mode")
	}
}

// NegotiateProtocolVersion accepts matching major and returns the negotiated
// minor (min of client and server). Unknown optional fields remain ignored by
// callers for lower minors.
func NegotiateProtocolVersion(clientVersion string, serverMajor, serverMinor int) (negotiated string, err error) {
	client, err := ParseProtocolVersion(clientVersion)
	if err != nil {
		return "", err
	}
	if client.Major != serverMajor {
		return "", fmt.Errorf("%s", ErrCodeVersionMismatch)
	}
	minor := client.Minor
	if serverMinor < minor {
		minor = serverMinor
	}
	return FormatProtocolVersion(serverMajor, minor), nil
}

// ValidateEnvelopeForm rejects mixed open/sealed payload forms.
// Empty body (neither payload nor sealed_payload) is allowed for control frames.
func ValidateEnvelopeForm(msg *NekoMessage) error {
	if msg == nil {
		return fmt.Errorf("message required")
	}
	hasPayload := msg.Payload != nil
	hasSealed := msg.SealedPayload != nil
	if hasPayload && hasSealed {
		return fmt.Errorf("%s: payload and sealed_payload are mutually exclusive", ErrCodeInvalidEnvelope)
	}
	return nil
}

// MessageBodyPolicy describes whether a post-handshake frame carries
// application data or only relay-visible routing/authentication metadata.
// The list is deliberately exhaustive: newly added message types fail closed
// until their confidentiality boundary is chosen explicitly.
type MessageBodyPolicy uint8

const (
	MessageBodyUnknown MessageBodyPolicy = iota
	MessageBodyRouting
	MessageBodyApplication
)

// BodyPolicy returns the v1 confidentiality policy for a wire message type.
func BodyPolicy(msgType MessageType) MessageBodyPolicy {
	switch msgType {
	case MsgDeviceOnline, MsgDeviceOffline, MsgDeviceList,
		MsgPromptStatusQuery, MsgPromptNotSeen, MsgPromptCommitted,
		MsgHeartbeat, MsgError, MsgRegisterDevice, MsgAuthResponse,
		MsgPairRequest, MsgPairConfirm, MsgPairReady, MsgPairFailed,
		MsgKeyPackage, MsgPhoneRevoked, MsgAttentionEvent,
		MsgSubscribe, MsgSubscribeAck:
		return MessageBodyRouting
	case MsgSessionList, MsgSessionUpdate, MsgSessionMessage,
		MsgSendPrompt, MsgPromptQueued, MsgPromptAccepted, MsgPromptFailed, MsgPromptSent,
		MsgApprove, MsgDeny, MsgInterrupt, MsgSteer,
		MsgRespondUserInput, MsgUserInputResult,
		MsgQueueUpdate, MsgCancelPrompt, MsgPromptCancelled, MsgResumePromptQueue,
		MsgStartThread, MsgThreadStarting, MsgThreadOwned, MsgThreadFailed, MsgThreadIndeterminate,
		MsgFetchHistory, MsgSessionHistory:
		return MessageBodyApplication
	default:
		return MessageBodyUnknown
	}
}

// ValidateFrameForTransport enforces the negotiated nest mode on every
// post-handshake frame. In sealed mode, application messages must be opaque to
// the relay. Routing frames may remain plaintext, but their handlers must
// continue to validate the small metadata fields they consume.
func ValidateFrameForTransport(msg *NekoMessage, mode TransportMode) error {
	if err := ValidateEnvelopeForm(msg); err != nil {
		return err
	}
	if msg == nil {
		return fmt.Errorf("%s: message required", ErrCodeInvalidEnvelope)
	}
	if msg.TransportMode != mode {
		return fmt.Errorf("%s: frame transport_mode %q does not match %q", ErrCodeTransportModeMismatch, msg.TransportMode, mode)
	}
	version, err := ParseProtocolVersion(msg.ProtocolVersion)
	if err != nil || version.Major != CurrentProtocolMajor {
		return fmt.Errorf("%s: invalid frame protocol_version", ErrCodeVersionMismatch)
	}
	policy := BodyPolicy(msg.Type)
	if policy == MessageBodyUnknown {
		return fmt.Errorf("%s: unknown message type %q", ErrCodeInvalidEnvelope, msg.Type)
	}
	switch mode {
	case TransportSealed:
		if msg.Type == MsgThreadIndeterminate && msg.ClientMsgID != "" && msg.Payload == nil && msg.SealedPayload == nil {
			return nil
		}
		if policy == MessageBodyApplication && msg.SealedPayload == nil {
			return fmt.Errorf("%s: sealed application frame %q requires sealed_payload", ErrCodeInvalidEnvelope, msg.Type)
		}
	case TransportOpen:
		if msg.SealedPayload != nil {
			return fmt.Errorf("%s: open frame %q cannot contain sealed_payload", ErrCodeInvalidEnvelope, msg.Type)
		}
	default:
		return fmt.Errorf("%s: invalid negotiated transport mode", ErrCodeTransportModeMismatch)
	}
	return nil
}

// NewMessage creates a new NekoMessage with the current timestamp.
func NewMessage(msgType MessageType, deviceID string) *NekoMessage {
	return &NekoMessage{
		ProtocolVersion: CurrentProtocolVersion,
		Type:            msgType,
		DeviceID:        deviceID,
		Timestamp:       time.Now().Unix(),
	}
}

// NewMessageWithSession creates a session-scoped message.
func NewMessageWithSession(msgType MessageType, deviceID, sessionID string) *NekoMessage {
	return &NekoMessage{
		ProtocolVersion: CurrentProtocolVersion,
		Type:            msgType,
		DeviceID:        deviceID,
		SessionID:       sessionID,
		Timestamp:       time.Now().Unix(),
	}
}

// WithTransport stamps the nest transport mode on the message.
func (m *NekoMessage) WithTransport(mode TransportMode) *NekoMessage {
	if m != nil {
		m.TransportMode = mode
	}
	return m
}

// HandshakeResult is the outcome of a first-frame version/mode negotiation.
type HandshakeResult struct {
	NegotiatedVersion string
	TransportMode     TransportMode
	ErrorCode         string
	Message           string
}

// NegotiateHandshake validates client protocol_version and transport_mode against
// the nest configuration. serverMinor is the server's supported minor for the
// current major (currently 1 for protocol 1.1).
func NegotiateHandshake(clientVersion string, clientMode string, nestMode TransportMode, serverMinor int) HandshakeResult {
	if nestMode == "" {
		nestMode = TransportSealed
	}
	mode, err := ParseTransportMode(clientMode)
	if err != nil {
		return HandshakeResult{
			ErrorCode: ErrCodeTransportModeMismatch,
			Message:   "transport_mode required (sealed|open)",
		}
	}
	if mode != nestMode {
		return HandshakeResult{
			ErrorCode: ErrCodeTransportModeMismatch,
			Message:   fmt.Sprintf("transport_mode mismatch: nest is %s", nestMode),
		}
	}
	neg, err := NegotiateProtocolVersion(clientVersion, CurrentProtocolMajor, serverMinor)
	if err != nil {
		return HandshakeResult{
			ErrorCode: ErrCodeVersionMismatch,
			Message:   fmt.Sprintf("protocol_version mismatch: server supports %s.x", FormatProtocolVersion(CurrentProtocolMajor, 0)),
		}
	}
	return HandshakeResult{
		NegotiatedVersion: neg,
		TransportMode:     mode,
	}
}

// HandshakeErrorPayload builds a stable error payload for version/mode failures.
func HandshakeErrorPayload(result HandshakeResult) map[string]any {
	return map[string]any{
		"error_code":       result.ErrorCode,
		"message":          result.Message,
		"protocol_version": CurrentProtocolVersion,
		"transport_mode":   string(TransportSealed), // overwritten by caller when known
	}
}
