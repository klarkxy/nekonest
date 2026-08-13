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

func TestSaveToAtomicallyReplacesStableEndpointConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"server_url":"wss://old.example"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		ServerURL:     "wss://connect.example.cn",
		DeviceID:      "host_abc",
		Token:         "token",
		TransportMode: TransportSealed,
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ServerURL != cfg.ServerURL || loaded.DeviceID != cfg.DeviceID || loaded.Token != cfg.Token {
		t.Fatalf("saved config mismatch: %#v", loaded)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".nekonest-config-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary config leaked: %v, %v", matches, err)
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
