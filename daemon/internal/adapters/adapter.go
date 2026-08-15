package adapters

import (
	"errors"
	"time"

	"github.com/nekonest/daemon/internal/attach"
)

// ErrPromptBoundaryIndeterminate means a native prompt RPC may have crossed
// its write boundary. Callers must not retry through another path or delete
// resources that the native process may still be reading.
var ErrPromptBoundaryIndeterminate = errors.New("native prompt boundary indeterminate")

// AgentType identifies the type of coding agent.
type AgentType string

const (
	AgentClaudeCode AgentType = "claude_code"
	AgentCodex      AgentType = "codex"
	AgentKimiCLI    AgentType = "kimi_cli"
	AgentGrokBuild  AgentType = "grok_build"
)

// AgentStatus represents the current state of an agent session.
type AgentStatus string

const (
	StatusRunning         AgentStatus = "running"
	StatusIdle            AgentStatus = "idle"
	StatusWaitingUser     AgentStatus = "waiting_user"
	StatusWaitingApproval AgentStatus = "waiting_approval"
	StatusError           AgentStatus = "error"
)

// AttachmentMode is the honest attachment capability for a session/agent.
type AttachmentMode string

const (
	AttachNativeImageAndFile AttachmentMode = "native_image_and_file"
	AttachNativeImage        AttachmentMode = "native_image"
	AttachPathBestEffort     AttachmentMode = "path_best_effort"
	AttachUnsupported        AttachmentMode = "unsupported"
)

// ControlMode describes how the daemon drives the agent.
type ControlMode string

const (
	ControlAppServer     ControlMode = "app_server"
	ControlExecResume    ControlMode = "exec_resume"
	ControlCompatibility ControlMode = "compatibility"
)

// SessionCapabilities advertises phone-side controls (absent = unsupported).
type SessionCapabilities struct {
	ControlMode        ControlMode       `json:"control_mode"`
	Send               bool              `json:"send"`
	Approve            bool              `json:"approve"`
	Deny               bool              `json:"deny"`
	Interrupt          bool              `json:"interrupt"`
	Steer              bool              `json:"steer"`
	Queue              bool              `json:"queue"`
	Spawn              bool              `json:"spawn"`
	UserInput          bool              `json:"user_input"`
	AttachmentMode     AttachmentMode    `json:"attachment_mode"`
	ControlPath        string            `json:"control_path,omitempty"`
	ControlVersion     string            `json:"control_version,omitempty"`
	UnavailableReasons map[string]string `json:"unavailable_reasons,omitempty"`
}

// DefaultCapabilities returns honest v1 defaults for a wire agent type.
// Codex full-control (app-server) is advertised only when that path is live;
// until then Codex is exec_resume without approval/spawn/steer.
func DefaultCapabilities(agentType AgentType) *SessionCapabilities {
	switch agentType {
	case AgentCodex:
		// Full-control flags are raised at wire time when app-server is healthy.
		return &SessionCapabilities{
			ControlMode:    ControlExecResume,
			Send:           true,
			Interrupt:      true,
			ControlPath:    "exec_resume",
			AttachmentMode: AttachNativeImage,
		}
	case AgentClaudeCode:
		return &SessionCapabilities{
			ControlMode: ControlCompatibility,
			UnavailableReasons: map[string]string{
				"send": "runtime_not_probed", "interrupt": "runtime_not_probed",
				"approve": "claude_bridge_unavailable", "deny": "claude_bridge_unavailable",
				"user_input": "claude_bridge_unavailable", "steer": "unsupported_by_agent",
			},
			AttachmentMode: AttachPathBestEffort,
		}
	case AgentKimiCLI:
		return &SessionCapabilities{
			ControlMode: ControlCompatibility,
			UnavailableReasons: map[string]string{
				"send": "runtime_not_probed", "interrupt": "runtime_not_probed",
				"approve": "acp_permission_not_observed", "deny": "acp_permission_not_observed",
				"user_input": "acp_question_not_observed", "steer": "unsupported_by_agent",
			},
			// Existing-session exec resume receives controlled local paths in
			// the prompt. This is not native image/file transport.
			AttachmentMode: AttachPathBestEffort,
		}
	case AgentGrokBuild:
		return &SessionCapabilities{
			ControlMode: ControlCompatibility,
			UnavailableReasons: map[string]string{
				"send": "runtime_not_probed", "interrupt": "runtime_not_probed",
				"approve": "acp_permission_not_observed", "deny": "acp_permission_not_observed",
				"user_input": "acp_question_not_observed", "steer": "unsupported_by_agent",
			},
			AttachmentMode: AttachPathBestEffort,
		}
	default:
		return &SessionCapabilities{
			ControlMode:        ControlCompatibility,
			UnavailableReasons: map[string]string{"send": "agent_unavailable", "interrupt": "agent_unavailable"},
			AttachmentMode:     AttachUnsupported,
		}
	}
}

// SessionInfo describes a discovered agent session.
type SessionInfo struct {
	ID               string               `json:"id"`
	AgentType        AgentType            `json:"agent_type"`
	Status           AgentStatus          `json:"status"`
	Summary          string               `json:"summary,omitempty"`
	LastActivity     time.Time            `json:"last_activity"`
	SessionPath      string               `json:"-"`                     // local store path (jsonl/db), not sent
	ProjectDir       string               `json:"project_dir,omitempty"` // workspace / project folder on PC
	Capabilities     *SessionCapabilities `json:"capabilities,omitempty"`
	PendingApproval  *ApprovalInfo        `json:"pending_approval,omitempty"`
	PendingUserInput *UserInputInfo       `json:"pending_user_input,omitempty"`
	ActiveTurn       *ActiveTurnBinding   `json:"active_turn,omitempty"`
}

// ActiveTurnBinding identifies the exact native turn that may be controlled.
// Generation and ClientMsgID are daemon-owned and remain stable for the turn;
// NativeRequestID is populated when the native controller returns one.
type ActiveTurnBinding struct {
	Generation      uint64 `json:"generation"`
	ClientMsgID     string `json:"client_msg_id"`
	NativeRequestID string `json:"native_request_id,omitempty"`
}

// ApprovalInfo describes a pending tool-call approval.
type ApprovalInfo struct {
	ID          string `json:"id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
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

type UserInputInfo struct {
	RequestID        string              `json:"request_id"`
	ItemID           string              `json:"item_id"`
	Questions        []UserInputQuestion `json:"questions"`
	AutoResolutionMS *uint64             `json:"auto_resolution_ms,omitempty"`
	ExpiresAt        int64               `json:"expires_at,omitempty"`
}

// ControlEvent is a positive app-server status signal for immediate wire updates.
type ControlEvent struct {
	SessionID        string
	AgentType        AgentType
	Status           AgentStatus
	Capabilities     *SessionCapabilities
	PendingApproval  *ApprovalInfo
	PendingUserInput *UserInputInfo
	Class            string
	EventID          string
	Generation       uint64
	ClientMsgID      string
	NativeRequestID  string
	Lifecycle        TurnLifecycle
	ActiveTurn       *ActiveTurnBinding
	ClearActiveTurn  bool
}

type TurnLifecycle string

const (
	TurnAccepted        TurnLifecycle = "accepted"
	TurnTerminalSuccess TurnLifecycle = "terminal_success"
	TurnTerminalFailure TurnLifecycle = "terminal_failure"
	TurnInterrupted     TurnLifecycle = "interrupted"
	TurnIndeterminate   TurnLifecycle = "indeterminate"
)

// HistoryMessage is a chat turn imported from the agent-native store.
type HistoryMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"` // user | assistant | system
	Content   string `json:"content"`
	Type      string `json:"type,omitempty"`
	Timestamp int64  `json:"timestamp"` // unix seconds
}

// OutputEvent is a normalized streaming event emitted by an adapter.
// SessionID always uses the public session id returned by Discover.
type OutputEvent struct {
	SessionID string
	AgentType AgentType
	Type      string
	Content   string
	MessageID string
}

// OutputSink receives normalized streaming events from every registered adapter.
type OutputSink func(OutputEvent)

// PromptRequest carries a user turn and any daemon-materialized local files.
// OnComplete releases those temporary files after the resumed CLI process exits.
type PromptRequest struct {
	Prompt            string
	Attachments       []attach.LocalFile
	CollaborationMode string
	OnComplete        func()
	Generation        uint64
	ClientMsgID       string
	OnLifecycle       func(TurnLifecycle, string)
	OnNativeBound     func(string)
	DeferAcceptance   bool
}

type PromptAcceptanceAcker interface {
	AcknowledgePrompt(sessionID string, generation uint64)
	AbandonPrompt(sessionID string, generation uint64)
}

// ControlSinkAdapter publishes generation-bound native control events.
type ControlSinkAdapter interface{ SetControlSink(func(ControlEvent)) }

// UserInputResponder is implemented only by controllers with a positive
// structured user-input request signal.
type UserInputResponder interface {
	RespondUserInput(requestID string, answers map[string][]string) (string, error)
}

// SteerAdapter is intentionally implemented only by Codex full control.
type SteerAdapter interface {
	Steer(sessionID, text string) error
}

// OutputAdapter is implemented by adapters that can stream resumed-agent output.
type OutputAdapter interface {
	SetOutputSink(OutputSink)
}

// Adapter defines the interface for discovering and controlling coding agents.
type Adapter interface {
	// Name returns the agent type identifier.
	Name() string

	// IsAvailable checks if the agent CLI is installed and accessible.
	IsAvailable() bool

	// Discover finds all active sessions for this agent type.
	Discover() ([]*SessionInfo, error)

	// OwnsSession performs a positive existence check against this adapter's
	// authoritative local store. Routing must not infer ownership from a
	// successful history read because an empty history can be valid.
	OwnsSession(sessionID string) bool

	// Watch monitors a specific session for state changes.
	// Returns a channel that emits updated SessionInfo on changes.
	Watch(sessionID string) (<-chan *SessionInfo, error)

	// SendPrompt sends a new prompt to an existing session. Implementations
	// must invoke request.OnComplete after a successfully started run exits.
	SendPrompt(sessionID string, request PromptRequest) error

	// Approve approves a pending tool call.
	Approve(sessionID string, approvalID string) error

	// Deny denies a pending tool call.
	Deny(sessionID string, approvalID string) error

	// Interrupt interrupts a running session (like Ctrl+C).
	Interrupt(sessionID string) error

	// FetchHistory returns the last N visible turns from the agent store.
	// Adapters may include system diagnostics that are durable native history.
	// limit is capped by each adapter (typically 50).
	FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error)
}

// ClosableAdapter extends Adapter with resource cleanup.
type ClosableAdapter interface {
	Adapter
	Close() error
}
