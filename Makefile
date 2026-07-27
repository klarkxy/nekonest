.PHONY: all server daemon pwa clean dev-server dev-daemon dev-pwa test test-server test-daemon test-pwa

# Default target
all: server daemon pwa

# Build Go server
server:
	cd server && go build -o ../bin/nekonest-server ./cmd/server

# Build Go daemon (Windows)
daemon:
	cd daemon && GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/nekonest-daemon.exe ./cmd/daemon

# Build daemon for current platform (dev)
daemon-local:
	cd daemon && go build -o ../bin/nekonest-daemon ./cmd/daemon

# Build PWA
pwa:
	cd pwa && pnpm install && pnpm build:only

# Build PWA and copy to server's pwa-dist
deploy: pwa
	rm -rf server/pwa-dist
	cp -r pwa/dist server/pwa-dist

# Development
dev-server:
	cd server && go run ./cmd/server

dev-daemon:
	cd daemon && go run ./cmd/daemon

dev-pwa:
	cd pwa && pnpm dev

# Unit tests
test: test-server test-daemon test-pwa

test-server:
	cd server && go test ./...

test-daemon:
	cd daemon && go test ./...

test-pwa:
	cd pwa && pnpm install && pnpm test

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf pwa/dist/
	rm -rf server/pwa-dist/

# Generate protocol types (placeholder)
proto:
	@echo "Protocol types are manually maintained in:"
	@echo "  server/internal/protocol/types.go"
	@echo "  pwa/src/types/protocol.ts"
	@echo "  protocol/protocol.json"
	@echo "Canonical app code lives only under server/, daemon/, pwa/."

# Production-oriented env reminders (not secrets — just names)
env-hints:
	@echo "NEKONEST_PHONE_SECRET     - protect phone REST/WS"
	@echo "NEKONEST_BOOTSTRAP_TOKEN  - protect /api/devices/register (required on public VPS)"
	@echo "NEKONEST_ALLOWED_ORIGINS  - CORS allowlist (comma-separated)"
	@echo "NEKONEST_VAPID_*          - optional Web Push (PUBLIC_KEY, PRIVATE_KEY, SUBJECT)"
