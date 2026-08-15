package agentexec

import "errors"

// ErrSessionBusy means a prompt was rejected before a new agent process was
// started because the prior run still owns the session. Callers may safely
// offer an explicit retry after the current run finishes.
var ErrSessionBusy = errors.New("agent session is already running")

// ErrPromptNotStarted reports a definitive failure before turn/start. Callers
// may safely mark the prompt failed or blocked without treating native
// acceptance as indeterminate.
var ErrPromptNotStarted = errors.New("agent prompt was not started")
