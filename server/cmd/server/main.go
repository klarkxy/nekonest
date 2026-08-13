package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nekonest/server/internal/buildinfo"
	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/opslog"
	"github.com/nekonest/server/internal/ws"
)

func main() {
	port := flag.String("port", "8080", "server port")
	dataDir := flag.String("data", "./data", "data directory for SQLite")
	pwaDir := flag.String("pwa", "./pwa-dist", "PWA static files directory")
	migrateV1 := flag.Bool("migrate-v1", false, "offline destructive v0.1→v1 content migration (requires -backup)")
	backupDir := flag.String("backup", "", "backup directory for -migrate-v1")
	showVersion := flag.Bool("version", false, "print the NekoNest server version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}
	setPrivateUmask()
	if _, err := opslog.Configure(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	opslog.RedirectStandard("server.legacy")

	if err := preparePrivateDirectory(*dataDir); err != nil {
		opslog.Error("server.main", "data_directory_prepare_failed", "failed to prepare private data directory", err)
		os.Exit(1)
	}

	if *migrateV1 {
		if err := runMigrateV1(*dataDir, *backupDir); err != nil {
			opslog.Error("server.main", "migration_failed", "v1 migration failed", err)
			os.Exit(1)
		}
		return
	}

	dbPath := filepath.Join(*dataDir, "nekonest.db")
	// Transport mode is immutable once persisted by the nest. The environment
	// may select only the first-run mode (or assert the already stored value).
	transportMode := strings.TrimSpace(os.Getenv("NEKONEST_TRANSPORT_MODE"))

	database, err := db.NewWithTransportMode(dbPath, transportMode)
	if err != nil {
		opslog.Error("server.main", "database_open_failed", "failed to open database", err)
		os.Exit(1)
	}
	defer database.Close()
	// Admin nest secret may be provided inline or through a private file.
	phoneSecret, deprecatedPhoneSecret, secretErr := loadAdminSecret()
	if secretErr != nil {
		opslog.Error("server.main", "admin_secret_load_failed", "failed to load admin authentication secret", secretErr)
		os.Exit(1)
	}
	if deprecatedPhoneSecret {
		opslog.Warn("server.main", "deprecated_phone_secret", "deprecated phone secret environment variable is in use")
	}
	if phoneSecret == "" {
		opslog.Warn("server.main", "local_only_mode", "admin authentication is not configured")
		if strings.TrimSpace(os.Getenv("NEKONEST_ALLOWED_ORIGINS")) == "" {
			_ = os.Setenv("NEKONEST_ALLOWED_ORIGINS", defaultLocalOrigins(*port))
		}
	} else {
		opslog.Info("server.main", "phone_auth_enabled", "phone authentication enabled")
	}
	if strings.TrimSpace(os.Getenv("NEKONEST_BOOTSTRAP_TOKEN")) == "" {
		if phoneSecret != "" {
			opslog.Warn("server.main", "registration_disabled", "device registration bootstrap is not configured")
		} else {
			opslog.Warn("server.main", "registration_open", "device registration is open in local development")
		}
	} else {
		opslog.Info("server.main", "registration_token_enabled", "device registration bootstrap enabled")
	}
	if v := strings.TrimSpace(os.Getenv("NEKONEST_TRUST_PROXY")); v == "1" || strings.EqualFold(v, "true") {
		opslog.Info("server.main", "trusted_proxy_enabled", "trusted proxy rate-limit mode enabled")
	}

	server := ws.NewCore(database, phoneSecret)
	server.SetDataDir(*dataDir)
	persistedMode, err := database.TransportMode()
	if err != nil {
		opslog.Error("server.main", "transport_mode_read_failed", "failed to read persistent transport mode", err)
		os.Exit(1)
	}
	if err := server.SetTransportMode(persistedMode); err != nil {
		opslog.Error("server.main", "transport_mode_invalid", "invalid persistent transport mode", err)
		os.Exit(1)
	}
	opslog.Info("server.main", "transport_mode_loaded", "persistent transport mode loaded", "transport_mode", server.TransportMode())

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	// Serve PWA static files (built Vue app)
	if _, err := os.Stat(*pwaDir); err == nil {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(*pwaDir))))
		// SPA fallback: serve index.html for any non-API, non-WS route
		mux.HandleFunc("/", spaHandler(*pwaDir))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<!DOCTYPE html><html><body><h1>🐱 NekoNest Server</h1><p>PWA not built yet. Run <code>cd pwa && pnpm build</code> first.</p></body></html>`))
				return
			}
			http.NotFound(w, r)
		})
	}

	// Apply middleware
	handler := ws.LoggingMiddleware(server.CORSMiddleware(mux))

	addr := listenAddress(*port, phoneSecret) // empty admin secret → loopback only

	// Create HTTP server with graceful shutdown support
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
		// WebSocket long-poll: do not set WriteTimeout (kills idle WS frames).
		// ReadHeaderTimeout bounds slowloris on headers; REST bodies are also
		// capped via MaxBytesReader/LimitReader in handlers (1 MiB).
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second, // REST slow-body cap; WS hijacks before body read
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// Start server in goroutine
	go func() {
		opslog.Info("server.main", "starting", "server starting", "version", buildinfo.Version, "bind_scope", map[bool]string{true: "loopback", false: "public"}[strings.HasPrefix(addr, "127.0.0.1:")])
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			opslog.Error("server.main", "listen_failed", "server stopped unexpectedly", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	opslog.Info("server.main", "shutdown_started", "server shutdown started")

	// Graceful shutdown with 10s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		opslog.Error("server.main", "shutdown_forced", "graceful shutdown timed out", err)
	}

	opslog.Info("server.main", "shutdown_complete", "server shutdown complete")
}

// listenAddress prevents an accidentally unauthenticated process from being
// exposed to the LAN. Public binds are enabled only when phone auth exists.
func listenAddress(port, phoneSecret string) string {
	if strings.TrimSpace(phoneSecret) == "" {
		return "127.0.0.1:" + port
	}
	return ":" + port
}

func defaultLocalOrigins(port string) string {
	return strings.Join([]string{
		"http://127.0.0.1:" + port,
		"http://localhost:" + port,
		"http://[::1]:" + port,
	}, ",")
}

// spaHandler returns an HTTP handler that serves the SPA's index.html
// for any route that doesn't match an API or static file.
func spaHandler(distDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Don't intercept API or WebSocket routes
		if len(r.URL.Path) > 4 {
			prefix := r.URL.Path[:4]
			if prefix == "/api" || prefix == "/ws/" {
				http.NotFound(w, r)
				return
			}
		}

		// Try to serve the file from dist
		path := filepath.Join(distDir, r.URL.Path)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.FileServer(http.Dir(distDir)).ServeHTTP(w, r)
			return
		}

		// Fall back to index.html (SPA routing)
		indexPath := filepath.Join(distDir, "index.html")
		if data, err := os.ReadFile(indexPath); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
		} else {
			http.NotFound(w, r)
		}
	}
}
