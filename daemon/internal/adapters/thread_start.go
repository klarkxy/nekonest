package adapters

import (
	"context"

	"github.com/nekonest/daemon/internal/attach"
)

// ThreadStartRequest describes an agent-scoped native thread creation.
// CWD must already belong to the daemon's discovered project set.
type ThreadStartRequest struct {
	ProjectDir string
	Prompt     string
	// Attachments are materialized by the daemon before native thread/start.
	// Starters which do not advertise attachment support must leave them unused.
	Attachments []attach.LocalFile
	// OnComplete releases attachment files after the initial native turn ends.
	// It is nil when no files were materialized.
	OnComplete func()
}

// ThreadStartResult reports observations at the CLI boundary. None of these
// fields alone prove ownership: the coordinator must positively confirm the
// returned SessionID against the adapter's authoritative native store before
// emitting thread_owned.
type ThreadStartResult struct {
	SessionID      string
	Created        bool
	PromptAccepted bool
	// PromptResult resolves only after the native controller has positively
	// completed or rejected the initial prompt. Nil means PromptAccepted is the
	// final synchronous observation.
	PromptResult <-chan error
}

// ThreadStartCapability is published at device scope so the phone can offer
// agents that do not yet have a session in a discovered project.
type ThreadStartCapability struct {
	Available      bool           `json:"available"`
	Reason         string         `json:"reason,omitempty"`
	ControlPath    string         `json:"control_path,omitempty"`
	ControlVersion string         `json:"control_version,omitempty"`
	AttachmentMode AttachmentMode `json:"attachment_mode"`
}

// NativeThreadStarter is an optional adapter capability. A returned SessionID
// is always supplied by the native CLI/store; callers must still positively
// establish adapter ownership before exposing it to the phone.
type NativeThreadStarter interface {
	ProbeThreadStart(context.Context) ThreadStartCapability
	StartNativeThread(context.Context, ThreadStartRequest) (ThreadStartResult, error)
}
