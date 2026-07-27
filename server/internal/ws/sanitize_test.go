package ws

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nekonest/server/internal/protocol"
)

func TestSanitizeAttachmentURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"spaces", "   ", ""},
		{"relative ok", "/api/attachments/abc123", "/api/attachments/abc123"},
		{"with hex key", "/api/attachments/deadbeef?k=cafebabe", "/api/attachments/deadbeef?k=cafebabe"},
		{"absolute strips host", "https://evil.com/api/attachments/abc?k=aa", "/api/attachments/abc?k=aa"},
		{"http absolute", "http://nest.example/api/attachments/ff", "/api/attachments/ff"},
		{"scheme relative", "//evil.com/api/attachments/x", ""},
		{"nested path", "/api/attachments/a/b", ""},
		{"dotdot", "/api/attachments/..", ""},
		{"wrong path", "/api/other/x", ""},
		{"empty id", "/api/attachments/", ""},
		{"non hex key", "/api/attachments/id?k=not-hex!", ""},
		{"file scheme path only rejects non prefix after parse", "file:///api/attachments/x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeAttachmentURL(tt.in); got != tt.want {
				t.Fatalf("sanitizeAttachmentURL(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeClientMsgID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"ab", ""},
		{"local_1", "local_1"},
		{"user_abc-DEF_09", "user_abc-DEF_09"},
		{"msg_xyz", "msg_xyz"},
		{"other_1", ""},
		{"local_bad!", ""},
		{"local_" + string(make([]byte, 80)), ""}, // too long once prefixed... build carefully
	}
	for _, tt := range tests {
		if got := sanitizeClientMsgID(tt.in); got != tt.want {
			t.Errorf("sanitizeClientMsgID(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
	// length bounds
	ok := "local_" + "a"
	for len(ok) < 4 {
		ok += "a"
	}
	if sanitizeClientMsgID(ok) != ok {
		t.Fatalf("expected min length ok")
	}
	long := "local_" + string(make([]rune, 80))
	for i := range long[6:] {
		// fill with 'a'
		_ = i
	}
	b := make([]byte, 0, 100)
	b = append(b, "local_"...)
	for len(b) < 81 {
		b = append(b, 'a')
	}
	if sanitizeClientMsgID(string(b)) != "" {
		t.Fatalf("expected reject >80")
	}
	for len(b) > 80 {
		b = b[:80]
	}
	if sanitizeClientMsgID(string(b)) != string(b) {
		t.Fatalf("expected accept len=80")
	}
}

func TestNormalizeAttachments(t *testing.T) {
	if normalizeAttachments(nil) != nil {
		t.Fatal("nil")
	}
	if normalizeAttachments("x") != nil {
		t.Fatal("wrong type")
	}
	raw := []any{
		map[string]any{"url": "/api/attachments/a1", "name": "a.png", "mime": "image/png", "id": "a1", "size": float64(12)},
		map[string]any{"url": "https://evil.com/steal"},
		map[string]any{"url": "/api/attachments/a2"},
		"skip",
		map[string]any{"url": "/api/attachments/a3"},
		map[string]any{"url": "/api/attachments/a4"},
		map[string]any{"url": "/api/attachments/a5"},
		map[string]any{"url": "/api/attachments/a6"}, // over max 5
	}
	out := normalizeAttachments(raw)
	// max 5 items inspected; bad URLs dropped → 3 kept (a1,a2,a3); a4/a5 not reached after i>=5 break on 6th map...
	// Loop: 0 a1, 1 bad, 2 a2, 3 skip, 4 a3, 5 a4 break before process → 3 good
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	if out[0]["url"] != "/api/attachments/a1" || out[0]["name"] != "a.png" {
		t.Fatalf("first entry: %#v", out[0])
	}
	if out[0]["size"].(int64) != 12 {
		t.Fatalf("size type/value %#v", out[0]["size"])
	}
	// dedicated max-5: all valid
	many := make([]any, 0, 7)
	for i := 0; i < 7; i++ {
		many = append(many, map[string]any{"url": "/api/attachments/id" + string(rune('a'+i))})
	}
	if len(normalizeAttachments(many)) != 5 {
		t.Fatalf("max5 got %d", len(normalizeAttachments(many)))
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"../../etc/passwd", "passwd"},
		// filepath.Base first → only last segment
		{`a/b\c:d*e?.txt`, "c_d_e_.txt"},
		{"  hi  ", "hi"},
		{"", "."}, // filepath.Base("") == "."
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
	long := string(make([]byte, 200))
	for i := range long {
		long = long[:i] + "a" + long[i+1:]
	}
	// rebuild properly
	b := make([]byte, 200)
	for i := range b {
		b[i] = 'a'
	}
	got := sanitizeFilename(string(b))
	if len(got) != 120 {
		t.Fatalf("truncate len=%d", len(got))
	}
}

func TestExtFromMIME(t *testing.T) {
	if extFromMIME("image/JPEG") != ".jpg" {
		t.Fatal("jpeg")
	}
	if extFromMIME("unknown/x") != ".bin" {
		t.Fatal("default")
	}
	if extFromMIME("application/pdf") != ".pdf" {
		t.Fatal("pdf")
	}
}

func TestSubtleConstEq(t *testing.T) {
	if !subtleConstEq("abc", "abc") {
		t.Fatal("eq")
	}
	if subtleConstEq("abc", "abd") {
		t.Fatal("neq")
	}
	if subtleConstEq("ab", "abc") {
		t.Fatal("len")
	}
	if !subtleConstEq("", "") {
		t.Fatal("empty")
	}
}

func TestRandomHex(t *testing.T) {
	s, err := randomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 32 {
		t.Fatalf("len=%d", len(s))
	}
	s0, err := randomHex(0)
	if err != nil || s0 != "" {
		t.Fatalf("n=0: %q %v", s0, err)
	}
}

func TestIsAllowedOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://nekonest.test/ws/phone", nil)
	r.Host = "nekonest.test"
	t.Setenv("NEKONEST_ALLOWED_ORIGINS", "")
	if !isAllowedOrigin(r, "http://nekonest.test") {
		t.Fatal("empty should allow same origin")
	}
	if isAllowedOrigin(r, "https://any") {
		t.Fatal("empty must reject cross origin")
	}
	t.Setenv("NEKONEST_ALLOWED_ORIGINS", "*")
	if !isAllowedOrigin(r, "https://any") {
		t.Fatal("star")
	}
	t.Setenv("NEKONEST_ALLOWED_ORIGINS", "https://a.com, https://b.com")
	if !isAllowedOrigin(r, "https://b.com") {
		t.Fatal("match")
	}
	if isAllowedOrigin(r, "https://c.com") {
		t.Fatal("mismatch")
	}
}

func TestSameOriginOnlyTrustsForwardedProtoFromTrustedProxy(t *testing.T) {
	t.Setenv("NEKONEST_ALLOWED_ORIGINS", "")
	t.Setenv("NEKONEST_TRUST_PROXY", "1")
	t.Setenv("NEKONEST_TRUSTED_PROXY_CIDRS", "")
	r := httptest.NewRequest(http.MethodGet, "http://nekonest.test/ws/phone", nil)
	r.Host = "nekonest.test"
	r.Header.Set("X-Forwarded-Proto", "http, https")
	r.RemoteAddr = "203.0.113.8:443"
	if isAllowedOrigin(r, "https://nekonest.test") {
		t.Fatal("untrusted peer changed effective request scheme")
	}
	r.RemoteAddr = "127.0.0.1:443"
	if !isAllowedOrigin(r, "https://nekonest.test") {
		t.Fatal("loopback reverse proxy was not trusted")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(2, 50*time.Millisecond)
	if !rl.allow("a") || !rl.allow("a") {
		t.Fatal("under limit")
	}
	if rl.allow("a") {
		t.Fatal("at limit")
	}
	if !rl.allow("b") {
		t.Fatal("other key")
	}
	time.Sleep(60 * time.Millisecond)
	if !rl.allow("a") {
		t.Fatal("window reset")
	}

	bounded := newRateLimiter(1, time.Hour)
	bounded.maxKeys = 4
	for _, key := range []string{"a", "b", "c", "d"} {
		if !bounded.allow(key) {
			t.Fatalf("unexpected rejection for %s", key)
		}
	}
	if bounded.allow("e") {
		t.Fatal("new spoofed key should be rejected at capacity")
	}
	if len(bounded.requests) != 4 {
		t.Fatalf("map grew beyond cap: %d", len(bounded.requests))
	}
}

func TestHistoryLimit(t *testing.T) {
	tests := []struct {
		raw  any
		want int
	}{
		{nil, 50},
		{float64(0.5), 50},
		{float64(-1), 1},
		{float64(0), 1},
		{float64(12), 12},
		{float64(999999), 500},
		{"500", 50},
	}
	for _, test := range tests {
		payload := map[string]any{}
		if test.raw != nil {
			payload["limit"] = test.raw
		}
		if got := historyLimit(payload); got != test.want {
			t.Errorf("historyLimit(%v)=%d want %d", test.raw, got, test.want)
		}
	}
}

func TestTruncateHistory(t *testing.T) {
	rows := []*protocol.SessionMessage{
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
	}
	got, truncated := truncateHistory(rows, 2)
	if !truncated || len(got) != 2 || got[0].ID != "2" || got[1].ID != "3" {
		t.Fatalf("got=%#v truncated=%v", got, truncated)
	}
	got, truncated = truncateHistory(rows[:2], 2)
	if truncated || len(got) != 2 {
		t.Fatalf("exact limit got=%#v truncated=%v", got, truncated)
	}
}

func TestAllowDeviceSwitch(t *testing.T) {
	now := time.Now()
	var switches []time.Time
	for i := 0; i < 6; i++ {
		if !allowDeviceSwitch(&switches, now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("switch %d rejected", i)
		}
	}
	if allowDeviceSwitch(&switches, now.Add(10*time.Second)) {
		t.Fatal("seventh switch in one minute should be rejected")
	}
	if !allowDeviceSwitch(&switches, now.Add(2*time.Minute)) {
		t.Fatal("old switch entries were not expired")
	}
}

func TestNeedsApprovalPush(t *testing.T) {
	if needsApprovalPush(map[string]any{
		"message": map[string]any{"type": "tool_call"},
	}) {
		t.Fatal("ordinary tool_call must not trigger approval push")
	}
	for _, payload := range []map[string]any{
		{"pending_approval": map[string]any{"id": "a"}},
		{"status": "waiting_approval"},
		{"session": map[string]any{"pending_approval": map[string]any{"id": "a"}}},
		{"session": map[string]any{"status": "waiting_approval"}},
		{"message": map[string]any{"type": "permission_request"}},
		{"message": map[string]any{
			"type":     "tool_call",
			"metadata": map[string]any{"permission_request": true},
		}},
	} {
		if !needsApprovalPush(payload) {
			t.Fatalf("explicit approval not detected: %#v", payload)
		}
	}
}

func TestClientIPKeyNoSpoofWithoutTrust(t *testing.T) {
	t.Setenv("NEKONEST_TRUST_PROXY", "")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:12345"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIPKey(r); got != "203.0.113.9" {
		t.Fatalf("got %s", got)
	}
	t.Setenv("NEKONEST_TRUST_PROXY", "1")
	t.Setenv("NEKONEST_TRUSTED_PROXY_CIDRS", "")
	// Untrusted direct peers cannot opt themselves into proxy headers.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.10")
	if got := clientIPKey(r); got != "203.0.113.9" {
		t.Fatalf("untrusted proxy got %s", got)
	}
	r.RemoteAddr = "127.0.0.1:12345"
	if got := clientIPKey(r); got != "198.51.100.10" {
		t.Fatalf("trust right-most got %s", got)
	}
	r.RemoteAddr = "10.23.4.5:443"
	t.Setenv("NEKONEST_TRUSTED_PROXY_CIDRS", "10.23.0.0/16, 192.0.2.9")
	if got := clientIPKey(r); got != "198.51.100.10" {
		t.Fatalf("trusted CIDR got %s", got)
	}
	r.RemoteAddr = "10.24.4.5:443"
	if got := clientIPKey(r); got != "10.24.4.5" {
		t.Fatalf("outside trusted CIDR got %s", got)
	}
}

func TestPhoneSecretFromRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?secret=q", nil)
	r.Header.Set("X-Neko-Secret", "h")
	r.Header.Set("Authorization", "Bearer b")
	if phoneSecretFromRequest(r) != "h" {
		t.Fatal("header wins")
	}
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r2.Header.Set("Authorization", "Bearer tok")
	if phoneSecretFromRequest(r2) != "tok" {
		t.Fatal("bearer")
	}
	r3 := httptest.NewRequest(http.MethodGet, "/x?secret=qs", nil)
	if phoneSecretFromRequest(r3) != "qs" {
		t.Fatal("query")
	}
	r4 := httptest.NewRequest(http.MethodGet, "/x", nil)
	if phoneSecretFromRequest(r4) != "" {
		t.Fatal("empty")
	}
}
