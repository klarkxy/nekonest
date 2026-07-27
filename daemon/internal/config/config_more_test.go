package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndDefaultPath(t *testing.T) {
	// DefaultConfigPath should be under home
	p := DefaultConfigPath()
	if p == "" || filepath.Base(p) != "config.json" {
		t.Fatalf("%q", p)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := []byte(`{"server_url":"http://h","device_id":"d","token":"t"}`)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(cfgPath)
	if err != nil || loaded.ServerURL != "ws://h" {
		t.Fatalf("%#v %v", loaded, err)
	}
}

func TestHTTPBaseURLRoundTrip(t *testing.T) {
	ws := NormalizeServerURL("https://nest.example/path/")
	if ws != "wss://nest.example/path" {
		t.Fatalf("%s", ws)
	}
	if HTTPBaseURL(ws) != "https://nest.example/path" {
		t.Fatalf("%s", HTTPBaseURL(ws))
	}
}
