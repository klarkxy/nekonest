package attach

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWsToHTTP(t *testing.T) {
	tests := []struct{ in, want string }{
		{"wss://h.example/ws", "https://h.example"},
		{"ws://h/", "http://h"},
		{"https://h", "https://h"},
		{"  wss://x/ws/  ", "https://x"},
	}
	for _, tt := range tests {
		if got := wsToHTTP(tt.in); got != tt.want {
			t.Errorf("wsToHTTP(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestHostOf(t *testing.T) {
	h, err := hostOf("https://Nest.Example:443")
	if err != nil || h != "nest.example:443" {
		t.Fatalf("%q %v", h, err)
	}
	if _, err := hostOf("https://"); err == nil {
		t.Fatal("missing host")
	}
}

func TestValidateAttachmentURL(t *testing.T) {
	ok := "https://nest.local/api/attachments/abc?k=1"
	if err := validateAttachmentURL(ok, "nest.local"); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"file:///api/attachments/x",
		"https://evil/api/attachments/x",
		"https://nest.local/api/other/x",
		"https://nest.local/api/attachments/",
		"https://nest.local/api/attachments/a/b",
		"https://nest.local/api/attachments/..",
	}
	for _, c := range cases {
		if err := validateAttachmentURL(c, "nest.local"); err == nil {
			t.Errorf("expected reject %s", c)
		}
	}
}

func TestAbsolutize(t *testing.T) {
	base := "https://nest.local"
	if absolutize(base, "https://x/y") != "https://x/y" {
		t.Fatal("abs")
	}
	if absolutize(base, "/api/attachments/a") != "https://nest.local/api/attachments/a" {
		t.Fatal("root rel")
	}
	if absolutize(base, "rel/path") != "" {
		t.Fatal("non-root")
	}
	if absolutize(base, "") != "" {
		t.Fatal("empty")
	}
}

func TestSanitizeAndExt(t *testing.T) {
	if sanitize("a b/c!") != "a-b-c-" {
		t.Fatalf("%q", sanitize("a b/c!"))
	}
	long := strings.Repeat("x", 40)
	if len(sanitize(long)) != 24 {
		t.Fatal("truncate")
	}
	if extFromMIME("image/png") != ".png" {
		t.Fatal("png")
	}
	if extFromMIME("application/json") != "" {
		t.Fatal("daemon unknown empty")
	}
}

func TestMaterialize(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/api/attachments/f1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("hello-file"))
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	dir, paths, suffix, err := Materialize("ws://"+host, "sess-1", []Ref{
		{URL: "/api/attachments/f1", Name: "note.txt", MIME: "text/plain"},
		{URL: "https://evil.example/api/attachments/x", Name: "bad"},
		{URL: "/api/attachments/f1", Name: "note.txt", MIME: "text/plain"}, // collision
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if len(paths) != 2 {
		t.Fatalf("paths=%v hits=%d", paths, hits)
	}
	if !strings.Contains(suffix, "rejected") {
		t.Fatalf("suffix missing reject: %s", suffix)
	}
	b, err := os.ReadFile(paths[0])
	if err != nil || string(b) != "hello-file" {
		t.Fatalf("content %q %v", b, err)
	}
	// name collision -> 2_note.txt style
	base0 := filepath.Base(paths[0])
	base1 := filepath.Base(paths[1])
	if base0 == base1 {
		t.Fatalf("collision names %s %s", base0, base1)
	}
}

func TestMaterializeEmpty(t *testing.T) {
	dir, paths, suf, err := Materialize("wss://x", "s", nil)
	if err != nil || dir != "" || paths != nil || suf != "" {
		t.Fatalf("%v %q %v %q", err, dir, paths, suf)
	}
}

func TestRedirectOffHostRejected(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pwned"))
	}))
	defer evil.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/api/attachments/x", http.StatusFound)
	}))
	defer good.Close()

	host := strings.TrimPrefix(good.URL, "http://")
	client := newSafeHTTPClient(host)
	err := downloadTo(client, good.URL+"/api/attachments/x", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected redirect reject")
	}
	_ = fmt.Sprintf("%v", err)
}
