package config

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config holds the daemon configuration.
type Config struct {
	ServerURL string `json:"server_url"` // e.g. "wss://nekonest.example.com" (ws/wss)
	DeviceID  string `json:"device_id"`
	Token     string `json:"token"`
	WorkDir   string `json:"work_dir"` // base directory for agent sessions
}

// NormalizeServerURL converts http(s) base URLs to ws(s) for WebSocket dialing.
// Strips trailing slashes.
func NormalizeServerURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	case strings.HasPrefix(u, "wss://") || strings.HasPrefix(u, "ws://"):
		return u
	default:
		// bare host:port → assume ws for local, caller should prefer full URL
		return "ws://" + u
	}
}

// HTTPBaseURL converts ws(s) URL to http(s) for REST calls.
func HTTPBaseURL(wsURL string) string {
	u := strings.TrimSpace(wsURL)
	u = strings.TrimRight(u, "/")
	switch {
	case strings.HasPrefix(u, "wss://"):
		return "https://" + strings.TrimPrefix(u, "wss://")
	case strings.HasPrefix(u, "ws://"):
		return "http://" + strings.TrimPrefix(u, "ws://")
	case strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://"):
		return u
	default:
		return "http://" + u
	}
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekonest", "config.json")
}

// Load reads config from the default path.
func Load() (*Config, error) {
	return LoadFrom(DefaultConfigPath())
}

// LoadFrom reads config from a specific path.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.ServerURL != "" {
		cfg.ServerURL = NormalizeServerURL(cfg.ServerURL)
	}
	return &cfg, nil
}

// Save writes config to the default path.
func (c *Config) Save() error {
	path := DefaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// WatchForChanges monitors the default config path.
func WatchForChanges(ctx context.Context, onChange func(*Config)) {
	WatchPath(ctx, DefaultConfigPath(), onChange)
}

// WatchPath monitors a specific config file for changes (polling).
func WatchPath(ctx context.Context, configPath string, onChange func(*Config)) {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	var lastModTime time.Time
	var lastSize int64

	if info, err := os.Stat(configPath); err == nil {
		lastModTime = info.ModTime()
		lastSize = info.Size()
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[config] watcher stopped")
			return
		case <-ticker.C:
			info, err := os.Stat(configPath)
			if err != nil {
				continue
			}
			if info.ModTime().Equal(lastModTime) && info.Size() == lastSize {
				continue
			}
			lastModTime = info.ModTime()
			lastSize = info.Size()
			time.Sleep(200 * time.Millisecond)

			newCfg, err := LoadFrom(configPath)
			if err != nil {
				log.Printf("[config] reload error: %v", err)
				continue
			}
			log.Printf("[config] config file changed, reloading %s", configPath)
			onChange(newCfg)
		}
	}
}

// Watcher provides a more structured way to watch config changes.
type Watcher struct {
	mu       sync.RWMutex
	config   *Config
	path     string
	onChange []func(*Config)
}

// NewWatcher creates a config watcher.
func NewWatcher(path string) *Watcher {
	return &Watcher{
		path: path,
	}
}

// Current returns the current config.
func (w *Watcher) Current() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// OnChange registers a callback for config changes.
func (w *Watcher) OnChange(fn func(*Config)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onChange = append(w.onChange, fn)
}

// Load loads or reloads the config.
func (w *Watcher) Load() error {
	cfg, err := LoadFrom(w.path)
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.config = cfg
	callbacks := make([]func(*Config), len(w.onChange))
	copy(callbacks, w.onChange)
	w.mu.Unlock()

	for _, fn := range callbacks {
		fn(cfg)
	}
	return nil
}
