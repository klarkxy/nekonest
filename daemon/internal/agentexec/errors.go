package agentexec

import "errors"

// ErrSessionBusy means a prompt was rejected before a new agent process was
// started because the prior run still owns the session. Callers may safely
// offer an explicit retry after the current run finishes.
var ErrSessionBusy = errors.New("agent session is already running")
