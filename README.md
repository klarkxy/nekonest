<div align="center">
  <img src="./pwa/public/brand/nekonest-duo.webp" width="360" alt="NekoNest">

  <h1>NekoNest · 猫娘窝</h1>

  <p><strong>Safely continue coding-agent threads that already live on your home Windows PC — from your phone.</strong></p>
  <p>Self-hosted · PC outbound-only · Native session stores · Mobile PWA</p>

  <p>
    <a href="./README.zh-CN.md">简体中文</a> ·
    <a href="#quick-start">Quick start</a> ·
    <a href="#supported-agents">Agents</a> ·
    <a href="#documentation">Docs</a> ·
    <a href="#license">License</a>
  </p>
</div>

---

NekoNest is a self-hosted bridge: the VPS handles authentication, pairing, relay, and durability; a Windows daemon dials out to the VPS and discovers threads from each agent’s **native** local store; the phone PWA shows history, sends prompts and attachments, and streams output.

> [!IMPORTANT]
> NekoNest only **resumes** threads that already exist on the PC. It does not create remote sessions from the phone. Each agent’s native store remains the authority for discovery and transcript history.

## How it works

```text
┌─────────────┐       HTTPS / WSS       ┌──────────────────┐
│  Phone PWA  │ ◄─────────────────────► │  VPS Server      │
│ Vue 3 + PWA │                         │  Go + SQLite     │
└─────────────┘                         └────────┬─────────┘
                                                 │ WSS
                                                 │ outbound from PC
                                        ┌────────▼─────────┐
                                        │ Windows Daemon   │
                                        │ discover/history │
                                        │ journal / exec   │
                                        └────────┬─────────┘
                                                 │ local store + CLI
                    ┌────────────┬───────────┬───┴──────┬────────────┐
                    │Claude Code │   Codex   │  Kilo    │ Kimi CLI   │ Grok Build
                    └────────────┴───────────┴──────────┴────────────┴───────────
```

The home PC needs neither a public IP nor inbound ports. The daemon opens an outbound WebSocket to the VPS; the phone only talks to the HTTPS/WSS nest.

## Features

- **Native thread discovery** — browse `directory → agent → thread`; orphans land in **未分类** (Uncategorized).
- **Reliable resume** — independent accepted / committed / failed delivery states; transport success is not agent acceptance.
- **History + streaming** — merge native history, server durability, and live output with stable message ids; CLI stderr stays diagnostic.
- **Attachments** — phone upload → daemon per-run temp dir → agent-specific wiring (max 5 files, 4 MB each).
- **Mobile UX** — installable PWA, drafts, sanitized Markdown, reconnect outbox, optional Web Push.
- **Safe defaults** — phone secret, bootstrap token, origin checks, attachment validation, size limits, controlled proxy trust.

## Supported agents

| Agent | Local session source | Resume entry | Attachments |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | `claude --resume` | Authorize run temp dir; paths in prompt |
| Codex | `~/.codex/sessions` | `codex exec resume` | Native image args; other files via restricted dir + paths |
| Kilo | Kilo / OpenCode local DB | `kilo run --session` | Native `--file` |
| Kimi CLI | `.kimi-code` (legacy `.kimi`) | `kimi --session` | Paths in prompt; CLI file permissions apply |
| Grok Build | `~/.grok/sessions` | `grok --resume` | Paths in prompt; non-interactive safe mode |

A missing CLI or empty store for one agent does not disable the others.

Wire ids: `claude_code`, `codex`, `kilo`, `kimi_cli`, `grok_build`.

## Quick start

### 1. Build and run the server (VPS)

Needs Go 1.22+, Node.js, pnpm. Public deploy also needs a TLS domain and Caddy/Nginx.

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest

cd pwa
pnpm install --frozen-lockfile
pnpm build

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server

export NEKONEST_PHONE_SECRET='long-random-string'
export NEKONEST_BOOTSTRAP_TOKEN='another-long-random-string'
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

Terminate public HTTPS/WSS at the reverse proxy to `127.0.0.1:8080`. Full systemd/Caddy/Nginx notes: [docs/deploy-vps.md](docs/deploy-vps.md).

### 2. Register and run the daemon (Windows)

Install and use at least one supported agent CLI so native threads exist.

```powershell
git clone https://github.com/klarkxy/nekonest.git
Set-Location nekonest\daemon

$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o nekonest-daemon.exe ./cmd/daemon

$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "same-bootstrap-token-as-vps"
.\nekonest-daemon.exe -register -name "Study PC"
.\nekonest-daemon.exe
```

Registration writes `%USERPROFILE%\.nekonest\config.json` and prints a 6-digit pair code. New code: `.\nekonest-daemon.exe -pair gen`. Autostart: [docs/deploy-windows.md](docs/deploy-windows.md).

### 3. Pair the phone

1. Open `https://nekonest.example.com` and enter `NEKONEST_PHONE_SECRET`.
2. **Pair computer** with the 6-digit code.
3. Confirm the device is online; open **directory → agent → thread**.
4. Send prompts and optional images / TXT / Markdown / PDF / JSON.

Acceptance checklist: [docs/e2e-smoke.md](docs/e2e-smoke.md).

## Configuration (summary)

| Variable | Role |
|---|---|
| `NEKONEST_PHONE_SECRET` | Phone REST/WS auth (**required** on public VPS) |
| `NEKONEST_BOOTSTRAP_TOKEN` | Daemon register gate (**required** on public VPS; ≠ phone secret) |
| `NEKONEST_ALLOWED_ORIGINS` | Browser origin allowlist |
| `NEKONEST_TRUST_PROXY` | `1` only behind a proxy that **overwrites** XFF |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | Trusted proxy CIDRs when proxy is not loopback |
| `NEKONEST_VAPID_*` | Optional Web Push |
| `NEKONEST_SERVER` | Daemon register: VPS URL |

> [!WARNING]
> Without `NEKONEST_PHONE_SECRET` the server binds **loopback only** for local development. Do not expose unauthenticated mode publicly.

Full flags, `config.json` fields, routes, and limits: [docs/configuration.md](docs/configuration.md). Trust model: [docs/security.md](docs/security.md).

The VPS relays and persists devices, messages, and attachments. **There is no end-to-end encryption.** Treat the host and `data/` as sensitive.

## Documentation

| Doc | Purpose |
|---|---|
| [docs/README.md](docs/README.md) | Full doc index (EN + ZH links) |
| [docs/deploy-vps.md](docs/deploy-vps.md) | Server, systemd, reverse proxy |
| [docs/deploy-windows.md](docs/deploy-windows.md) | Daemon register, run, autostart |
| [docs/configuration.md](docs/configuration.md) | Env, flags, limits |
| [docs/security.md](docs/security.md) | Secrets, trust, hardening |
| [docs/architecture.md](docs/architecture.md) | Data flow and delivery |
| [docs/protocol.md](docs/protocol.md) | Wire types and REST/WS |
| [docs/development.md](docs/development.md) | Local dev and tests |
| [docs/troubleshooting.md](docs/troubleshooting.md) | Common failures |
| [docs/e2e-smoke.md](docs/e2e-smoke.md) | Deploy acceptance |
| [docs/release.md](docs/release.md) | Maintainer release cut |
| [docs/brand-art.md](docs/brand-art.md) | Brand asset rebuild |
| [CHANGELOG.md](CHANGELOG.md) | User-visible history |
| [docs/archive/](docs/archive/) | Frozen construction history (**not** the live contract) |

简体中文: [README.zh-CN.md](README.zh-CN.md) and `docs/*.zh-CN.md`.

Contributors and coding agents: start with [AGENTS.md](AGENTS.md).

## Repository layout

```text
nekonest/
├── protocol/   # language-neutral JSON schema
├── server/     # VPS: auth, pair, relay, SQLite, attachments, push
├── daemon/     # Windows: discover, history, journal, agent processes
├── pwa/        # Vue 3 + TypeScript + Pinia mobile client
├── docs/       # operator + contributor docs (EN + .zh-CN)
├── CHANGELOG.md
├── LICENSE / LICENSE_zh
└── tools/      # reproducible brand asset build
```

Two Go modules (`server/`, `daemon/`) and one pnpm app (`pwa/`); no root Go module. Protocol types are manual—see [docs/protocol.md](docs/protocol.md).

## Development and verification

```powershell
# Server
Set-Location server
go test -count=1 ./...
go vet ./...

# Daemon
Set-Location ..\daemon
go test -count=1 ./...
go vet ./...

# PWA
Set-Location ..\pwa
pnpm install --frozen-lockfile
pnpm test
pnpm type-check
pnpm build
```

Local run sketch:

```text
server:  go run ./cmd/server -port 8080 -pwa ../pwa/dist
daemon:  go run ./cmd/daemon
pwa:     pnpm dev
```

Details: [docs/development.md](docs/development.md).

## Current boundaries (v0.1)

These are stable product limits, not a todo list:

- Phone does not create threads; create them on the PC first.
- Tool approval depends on each agent’s non-interactive CLI; blocked work may need the PC.
- Kimi CLI and Grok Build receive attachment **paths** in the prompt; reads depend on CLI permissions.
- Web Push needs VAPID; without it, no real push is sent.
- Daemon targets **Windows**.
- VPS stores metadata, messages, and attachments; **no E2E encryption**.

## License

**Star And Thank Author License (SATA) 2.0**.

- Legal text: English [LICENSE](LICENSE)
- Convenience translation: [LICENSE_zh](LICENSE_zh) (not independently binding)

Please star this repository and thank the author before use, distribution, or modification.
