package adapters

import "time"

// AgentType identifies the type of coding agent.
type AgentType string

const (
	AgentClaudeCode AgentType = "claude_code"
	AgentCodex      AgentType = "codex"
	AgentKilo       AgentType = "kilo"
	AgentKimiCLI    AgentType = "kimi_cli"
	AgentGrokBuild  AgentType = "grok_build"
)

// AgentStatus represents the current state of an agent session.
type AgentStatus string

const (
	StatusRunning         AgentStatus = "running"
	StatusIdle            AgentStatus = "idle"
	StatusWaitingApproval AgentStatus = "waiting_approval"
)

// SessionInfo describes a discovered agent session.
type SessionInfo struct {
	ID              string        `json:"id"`
	AgentType       AgentType     `json:"agent_type"`
	Status          AgentStatus   `json:"status"`
	Summary         string        `json:"summary,omitempty"`
	LastActivity    time.Time     `json:"last_activity"`
	SessionPath     string        `json:"-"`                     // local store path (jsonl/db), not sent
	ProjectDir      string        `json:"project_dir,omitempty"` // workspace / project folder on PC
	PendingApproval *ApprovalInfo `json:"pending_approval,omitempty"`
}

// ApprovalInfo describes a pending tool-call approval.
type ApprovalInfo struct {
	ID          string `json:"id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
}

// HistoryMessage is a chat turn imported from the agent-native store.
type HistoryMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"` // user | assistant
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

	// SendPrompt sends a new prompt to an existing session.
	SendPrompt(sessionID string, prompt string) error

	// Approve approves a pending tool call.
	Approve(sessionID string, approvalID string) error

	// Deny denies a pending tool call.
	Deny(sessionID string, approvalID string) error

	// Interrupt interrupts a running session (like Ctrl+C).
	Interrupt(sessionID string) error

	// FetchHistory returns the last N user/assistant turns from the agent store.
	// limit is capped by each adapter (typically 50).
	FetchHistory(sessionID string, limit int) ([]*HistoryMessage, error)
}

// ClosableAdapter extends Adapter with resource cleanup.
type ClosableAdapter interface {
	Adapter
	Close() error
}
