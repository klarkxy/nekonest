package adapters

import "time"

// AgentType identifies the type of coding agent.
type AgentType string

const (
	AgentClaudeCode AgentType = "claude_code"
	AgentCodex      AgentType = "codex"
	AgentKilo       AgentType = "kilo"
)

// AgentStatus represents the current state of an agent session.
type AgentStatus string

const (
	StatusRunning        AgentStatus = "running"
	StatusIdle           AgentStatus = "idle"
	StatusWaitingApproval AgentStatus = "waiting_approval"
)

// SessionInfo describes a discovered agent session.
type SessionInfo struct {
	ID              string      `json:"id"`
	AgentType       AgentType   `json:"agent_type"`
	Status          AgentStatus `json:"status"`
	Summary         string      `json:"summary,omitempty"`
	LastActivity    time.Time   `json:"last_activity"`
	SessionPath     string      `json:"-"` // local filesystem path, not sent to server
	PendingApproval *ApprovalInfo `json:"pending_approval,omitempty"`
}

// ApprovalInfo describes a pending tool-call approval.
type ApprovalInfo struct {
	ID          string `json:"id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
}

// Adapter defines the interface for discovering and controlling coding agents.
type Adapter interface {
	// Name returns the agent type identifier.
	Name() string

	// IsAvailable checks if the agent CLI is installed and accessible.
	IsAvailable() bool

	// Discover finds all active sessions for this agent type.
	Discover() ([]*SessionInfo, error)

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
}

// ClosableAdapter extends Adapter with resource cleanup.
type ClosableAdapter interface {
	Adapter
	Close() error
}
