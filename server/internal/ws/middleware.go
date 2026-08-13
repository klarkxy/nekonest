package ws

import (
	"net/http"
	"strings"
	"time"

	"github.com/nekonest/server/internal/opslog"
)

// LoggingMiddleware is part of the standalone deployment shell. Relay data
// plane behavior lives in relaycore.Engine.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Preserve optional ResponseWriter interfaces required by WebSocket
		// upgrades. Health and static requests are intentionally not logged.
		if strings.HasPrefix(r.URL.Path, "/ws/") ||
			r.URL.Path == "/health" ||
			strings.HasPrefix(r.URL.Path, "/static/") ||
			strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		response := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(response, r)

		opslog.Info("server.http", "request_completed", "HTTP request completed",
			"method", r.Method,
			"route", requestRoute(r),
			"status", response.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// requestRoute returns an allowlisted route label instead of an arbitrary URL.
// This intentionally excludes query values and dynamic attachment identifiers.
func requestRoute(r *http.Request) string {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/attachments/"):
		return "/api/attachments/{id}"
	case strings.HasPrefix(path, "/static/"):
		return "/static/*"
	case strings.HasPrefix(path, "/assets/"):
		return "/assets/*"
	case knownOperatorRoute(path):
		return path
	case strings.HasPrefix(path, "/api/"):
		return "api_unmatched"
	case path == "/":
		return "/"
	default:
		return "spa_route"
	}
}

func knownOperatorRoute(path string) bool {
	switch path {
	case "/api/devices", "/api/devices/register", "/api/devices/sessions",
		"/api/phones/bootstrap", "/api/phones", "/api/phones/revoke",
		"/api/messages", "/api/attachments", "/api/push/subscribe",
		"/api/push/vapid-public-key", "/api/pair/generate", "/api/pair/consume",
		"/api/devices/keys", "/api/devices/grants", "/api/keys", "/api/keys/upload":
		return true
	default:
		return false
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
