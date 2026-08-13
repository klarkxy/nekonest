package relaycore

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

func normalizeAllowedOrigins(configured []string) map[string]struct{} {
	if len(configured) == 0 {
		configured = strings.Split(os.Getenv("NEKONEST_ALLOWED_ORIGINS"), ",")
	}
	out := make(map[string]struct{})
	for _, raw := range configured {
		raw = strings.TrimSpace(raw)
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || raw == "*" {
			continue
		}
		out[u.Scheme+"://"+u.Host] = struct{}{}
	}
	return out
}

func (s *Engine) originAllowed(r *http.Request, origin string) bool {
	if strings.TrimSpace(origin) == "" {
		return true
	}
	if isSameOrigin(r, origin) {
		return true
	}
	_, ok := s.allowedOrigins[strings.TrimRight(origin, "/")]
	return ok
}

func (s *Engine) websocketUpgrader() websocket.Upgrader {
	u := upgrader
	u.CheckOrigin = func(r *http.Request) bool { return s.originAllowed(r, r.Header.Get("Origin")) }
	return u
}

// CORSMiddleware is the package-level helper for callers that have not
// constructed an Engine. It uses the same exact-origin policy as Engine.
func CORSMiddleware(next http.Handler) http.Handler {
	return (&Engine{allowedOrigins: normalizeAllowedOrigins(nil)}).CORSMiddleware(next)
}

// CORSMiddleware permits a cross-origin PWA's bearer and opaque route headers only
// for configured exact origins. No wildcard or credentialed-cookie mode is
// emitted; same-origin self-host requests are unchanged.
func (s *Engine) CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !s.originAllowed(r, origin) {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Neko-Bootstrap, X-Neko-Phone-Token, X-Neko-Route-Handle, X-Neko-Secret")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
