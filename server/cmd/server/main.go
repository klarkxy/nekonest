package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/ws"
)

func main() {
	port := flag.String("port", "8080", "server port")
	dataDir := flag.String("data", "./data", "data directory for SQLite")
	pwaDir := flag.String("pwa", "./pwa-dist", "PWA static files directory")
	flag.Parse()

	// Ensure data directory exists
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("failed to create data directory: %v", err)
	}
	dbPath := filepath.Join(*dataDir, "nekonest.db")

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	phoneSecret := os.Getenv("NEKONEST_PHONE_SECRET")
	if phoneSecret == "" {
		log.Printf("⚠️  NEKONEST_PHONE_SECRET not set — phone API/WS are open (dev only)")
	} else {
		log.Printf("🔒 phone secret auth enabled")
	}

	server := ws.NewWithSecret(database, phoneSecret)

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

	addr := ":" + *port

	// Create HTTP server with graceful shutdown support
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("🐱 NekoNest Server starting on %s", addr)
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


