package relaycore

import "time"

// UnauthenticatedReadLimit is the first-frame size cap before daemon/phone
// authentication. Exported so standalone integration tests can share the value.
const UnauthenticatedReadLimit = unauthenticatedReadLimit

// OverrideWSRateLimiter replaces the process-wide WebSocket rate limiter.
// Tests must call the returned restore function.
func OverrideWSRateLimiter(limit int, window time.Duration) (restore func()) {
	previous := wsRateLimiter
	wsRateLimiter = newRateLimiter(limit, window)
	return func() { wsRateLimiter = previous }
}
