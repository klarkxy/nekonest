package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://a.com/", "wss://a.com"},
		{"http://a.com", "ws://a.com"},
		{"wss://a.com/", "wss://a.com"},
		{"ws://a.com", "ws://a.com"},
		{"localhost:8080", "ws://localhost:8080"},
		{"  https://x  ", "wss://x"},
	}
	for _, tt := range tests {
		if got := NormalizeServerURL(tt.in); got != tt.want {
			t.Errorf("NormalizeServerURL(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestHTTPBaseURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"wss://a.com/", "https://a.com"},
		{"ws://a.com", "http://a.com"},
		{"https://a.com", "https://a.com"},
		{"host:1", "http://host:1"},
	}
	for _, tt := range tests {
		if got := HTTPBaseURL(tt.in); got != tt.want {
			t.Errorf("HTTPBaseURL(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadFrom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := map[string]string{
		"server_url": "https://nest.example/",
		"device_id":  "d1",
		"token":      "t",
		"work_dir":   "C:\\w",
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "wss://nest.example" {
		t.Fatalf("normalized %q", cfg.ServerURL)
	}
	if cfg.DeviceID != "d1" || cfg.Token != "t" {
		t.Fatalf("%#v", cfg)
	}
	if _, err := LoadFrom(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing")
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("bad json")
	}
}

func TestWatcherLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	_ = os.WriteFile(path, []byte(`{"server_url":"ws://h","device_id":"x","token":"y"}`), 0o600)
	w := NewWatcher(path)
	var called int
	w.OnChange(func(c *Config) {
		called++
		if c.DeviceID != "x" {
			t.Errorf("device %s", c.DeviceID)
		}
	})
	if err := w.Load(); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("callbacks %d", called)
	}
	if w.Current().Token != "y" {
		t.Fatal("current")
	}
}
