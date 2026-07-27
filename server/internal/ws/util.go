package ws

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

const maxJSONBody = 1 << 20 // 1 MiB

func writeJSON(w http.ResponseWriter, v any) {
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, maxJSONBody+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(body) > maxJSONBody {
		return fmt.Errorf("request body too large")
	}
	return json.Unmarshal(body, v)
}

// clientIPKey strips the ephemeral source port so rate limits apply per host.
// X-Forwarded-For is only honored when NEKONEST_TRUST_PROXY is set.
// When trusted, use the RIGHT-most hop (added by the immediate reverse proxy);
// left-most is client-spoofable.
func clientIPKey(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	if trustedProxyRequest(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			// Right-most non-empty entry — set/appended by trusted edge proxy.
			for i := len(parts) - 1; i >= 0; i-- {
				if h := strings.TrimSpace(parts[i]); h != "" {
					return h
				}
			}
		}
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
	}
	return addr
}

func trustProxy() bool {
	v := strings.TrimSpace(os.Getenv("NEKONEST_TRUST_PROXY"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// trustedProxyRequest requires both the explicit TRUST_PROXY opt-in and a
// trusted immediate peer. Loopback proxies are trusted by default; additional
// reverse-proxy networks must be listed in NEKONEST_TRUSTED_PROXY_CIDRS.
func trustedProxyRequest(r *http.Request) bool {
	if !trustProxy() || r == nil {
		return false
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	ip := net.ParseIP(strings.Trim(remote, "[]"))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, item := range strings.Split(os.Getenv("NEKONEST_TRUSTED_PROXY_CIDRS"), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if trustedIP := net.ParseIP(item); trustedIP != nil {
			if trustedIP.Equal(ip) {
				return true
			}
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
