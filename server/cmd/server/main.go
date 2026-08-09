package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nekonest/server/internal/buildinfo"
	"github.com/nekonest/server/internal/db"
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

	if *migrateV1 {
		if err := runMigrateV1(*dataDir, *backupDir); err != nil {
			log.Fatalf("migrate-v1 failed: %v", err)
		}
		return
	}

	// Ensure data directory exists
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("failed to create data directory: %v", err)
	}
	dbPath := filepath.Join(*dataDir, "nekonest.db")
	// Transport mode is immutable once persisted by the nest. The environment
	// may select only the first-run mode (or assert the already stored value).
	transportMode := strings.TrimSpace(os.Getenv("NEKONEST_TRANSPORT_MODE"))

	database, err := db.NewWithTransportMode(dbPath, transportMode)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Admin nest secret: prefer NEKONEST_ADMIN_SECRET; one-release alias PHONE_SECRET.
	phoneSecret := strings.TrimSpace(os.Getenv("NEKONEST_ADMIN_SECRET"))
	if phoneSecret == "" {
		phoneSecret = strings.TrimSpace(os.Getenv("NEKONEST_PHONE_SECRET"))
		if phoneSecret != "" {
			log.Printf("⚠️  NEKONEST_PHONE_SECRET is deprecated; use NEKONEST_ADMIN_SECRET")
		}
	}
	if phoneSecret == "" {
		log.Printf("⚠️  NEKONEST_ADMIN_SECRET not set — local-only development mode")
		if strings.TrimSpace(os.Getenv("NEKONEST_ALLOWED_ORIGINS")) == "" {
			_ = os.Setenv("NEKONEST_ALLOWED_ORIGINS", defaultLocalOrigins(*port))
		}
	} else {
		log.Printf("🔒 admin secret auth enabled (phone tokens via /api/phones/bootstrap)")
	}
	if strings.TrimSpace(os.Getenv("NEKONEST_BOOTSTRAP_TOKEN")) == "" {
		if phoneSecret != "" {
			log.Printf("⚠️  NEKONEST_BOOTSTRAP_TOKEN not set while ADMIN_SECRET is set — device registration is disabled")
		} else {
			log.Printf("⚠️  NEKONEST_BOOTSTRAP_TOKEN not set — /api/devices/register is open (dev only)")
		}
	} else {
		log.Printf("🔒 device registration bootstrap token enabled")
	}
	if v := strings.TrimSpace(os.Getenv("NEKONEST_TRUST_PROXY")); v == "1" || strings.EqualFold(v, "true") {
		log.Printf("🔒 TRUST_PROXY on — X-Forwarded-For used for rate limits (only behind a trusted reverse proxy)")
	}

	server := ws.NewWithSecret(database, phoneSecret)
	server.SetDataDir(*dataDir)
	persistedMode, err := database.TransportMode()
	if err != nil {
		log.Fatalf("failed to read persistent transport mode: %v", err)
	}
	if err := server.SetTransportMode(persistedMode); err != nil {
		log.Fatalf("invalid persistent transport mode: %v", err)
	}
	log.Printf("🔐 transport mode: %s", server.TransportMode())

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
	handler := ws.LoggingMiddleware(ws.CORSMiddleware(mux))

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
		log.Printf("🐱 NekoNest Server %s starting on %s", buildinfo.Version, addr)
		log.Printf("   Data: %s", dbPath)
		log.Printf("   PWA:  %s", *pwaDir)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[server] shutting down...")

	// Graceful shutdown with 10s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("[server] forced shutdown: %v", err)
	}

	log.Println("[server] goodbye 🐱")
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
