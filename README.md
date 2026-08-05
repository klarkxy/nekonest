<div align="center">
  <img src="./pwa/public/brand/nekonest-duo.webp" width="360" alt="NekoNest">

  <h1>NekoNest · 猫娘窝</h1>

  <p><strong>Safely continue coding-agent threads on your Windows or Linux host — from your phone.</strong></p>
  <p>Self-hosted · Host outbound-only · Native session stores · Mobile PWA</p>

  <p>
    <a href="./README.zh-CN.md">简体中文</a> ·
    <a href="#quick-start">Quick start</a> ·
    <a href="#supported-agents">Agents</a> ·
    <a href="#documentation">Docs</a> ·
    <a href="#license">License</a>
  </p>
</div>

---

NekoNest is a self-hosted bridge: the VPS handles authentication, pairing, relay, and durability; a Windows/Linux daemon dials out to the VPS and discovers threads from each agent’s **native** local store; the phone PWA shows history, sends prompts and attachments, streams output, and fully controls Codex through its native app-server.

> [!IMPORTANT]
> NekoNest primarily **resumes** threads that already exist on the host. A phone may open an agent-scoped local draft; its first prompt may create a native thread only when that agent’s starter is installed, probed, and advertises `spawn=true`, and only in the daemon’s current union of native-discovered project directories. Arbitrary paths and generic `create_session` remain forbidden. A created thread is shown as owned only after both positive first-prompt acknowledgement and ownership in that agent's authoritative native store.

## How it works

```text
┌─────────────┐       HTTPS / WSS       ┌──────────────────┐
│  Phone PWA  │ ◄─────────────────────► │  VPS Server      │
│ Vue 3 + PWA │                         │  Go + SQLite     │
└─────────────┘                         └────────┬─────────┘
                                                 │ WSS
                                                 │ outbound from PC
                                        ┌────────▼─────────┐
                                        │ Host Daemon      │
                                        │ Windows / Linux  │
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
- **Codex full control** — native app-server send, approve/deny, steer, interrupt, and image/file attachments; honest `exec resume` fallback when unhealthy. All five agents may expose an agent-scoped native start only after their own starter probe succeeds.
- **Transport negotiation** — one fixed `open` or `sealed` mode per nest; v0.2 defaults to open while sealed remains an explicit v1 preview.
- **Mobile UX** — installable PWA, drafts, per-thread or whole-project phone-local archive, sanitized Markdown, reconnect outbox, optional Web Push.
- **Version diagnostics** — compare the loaded PWA with the live server at page level; each machine reports its own daemon release and update state on its device card.
- **Safe defaults** — admin bootstrap, revocable phone identities, daemon registration token, origin checks, attachment validation, size limits, controlled proxy trust.

## Supported agents

| Agent | Local session source | Control | Attachments |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | Compatibility resume via `claude --resume` | Authorize run temp dir; paths in prompt |
| Codex | `~/.codex/sessions` | **Full control** via `codex app-server`; `exec resume` fallback | Native images and files when app-server is healthy |
| Kilo | Kilo / OpenCode local DB | Compatibility resume via `kilo run --session` | Native `--file` |
| Kimi CLI | `.kimi-code` (legacy `.kimi`) | Compatibility resume via `kimi --session` | Paths in prompt; CLI file permissions apply |
| Grok Build | `~/.grok/sessions` | Compatibility resume via `grok --resume` | Paths in prompt; non-interactive safe mode |

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

export NEKONEST_ADMIN_SECRET='long-random-string'
export NEKONEST_BOOTSTRAP_TOKEN='another-long-random-string'
export NEKONEST_TRANSPORT_MODE='open'
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

Terminate public HTTPS/WSS at the reverse proxy to `127.0.0.1:8080`. Full systemd/Caddy/Nginx notes: [docs/deploy-vps.md](docs/deploy-vps.md).

### 2. Register and run the daemon (Windows/Linux)

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

Registration writes the host config and prints a 6-digit pair code. New code: `.\nekonest-daemon.exe -pair gen`. Autostart: [Windows](docs/deploy-windows.md) · [Linux](docs/deploy-linux.md).

### 3. Pair the phone

1. Open `https://nekonest.example.com` and enter `NEKONEST_ADMIN_SECRET` to bootstrap the phone identity.
2. **Pair computer** with the 6-digit code.
3. Confirm the device is online; open **directory → agent → thread**.
4. Send prompts and optional images / TXT / Markdown / PDF / JSON.

Acceptance checklist: [docs/e2e-smoke.md](docs/e2e-smoke.md).

## Configuration (summary)

| Variable | Role |
|---|---|
| `NEKONEST_ADMIN_SECRET` | Admin bootstrap and phone-token minting (**required** on public VPS) |
| `NEKONEST_PHONE_SECRET` | Deprecated compatibility alias for the admin secret |
| `NEKONEST_BOOTSTRAP_TOKEN` | Daemon register gate (**required** on public VPS; ≠ phone secret) |
| `NEKONEST_TRANSPORT_MODE` | Fixed nest mode; v0.2 defaults to `open`, sealed is opt-in preview |
| `NEKONEST_ALLOWED_ORIGINS` | Browser origin allowlist |
| `NEKONEST_TRUST_PROXY` | `1` only behind a proxy that **overwrites** XFF |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | Trusted proxy CIDRs when proxy is not loopback |
| `NEKONEST_VAPID_*` | Optional Web Push |
| `NEKONEST_SERVER` | Daemon register: VPS URL |

> [!WARNING]
> Without an admin secret the server binds **loopback only** for local development. Do not expose unauthenticated mode publicly.

Full flags, `config.json` fields, routes, and limits: [docs/configuration.md](docs/configuration.md). Trust model: [docs/security.md](docs/security.md).

In v0.2 the operational default is `open`, so the VPS can relay and persist application plaintext; treat the host and `data/` as sensitive. Sealed E2E is an explicit preview mode and becomes the new-nest default only at the v1 acceptance cutover.

## Documentation

| Doc | Purpose |
|---|---|
| [docs/README.md](docs/README.md) | Full doc index (EN + ZH links) |
| [docs/deploy-vps.md](docs/deploy-vps.md) | Server, systemd, reverse proxy |
| [docs/deploy-windows.md](docs/deploy-windows.md) | Daemon register, run, autostart |
| [docs/deploy-linux.md](docs/deploy-linux.md) | Linux daemon and systemd user service |
| [docs/configuration.md](docs/configuration.md) | Env, flags, limits |
| [docs/security.md](docs/security.md) | Secrets, trust, hardening |
| [docs/architecture.md](docs/architecture.md) | Data flow and delivery |
| [docs/protocol.md](docs/protocol.md) | Wire types and REST/WS |
| [docs/development.md](docs/development.md) | Local dev and tests |
| [docs/troubleshooting.md](docs/troubleshooting.md) | Common failures |
| [docs/e2e-smoke.md](docs/e2e-smoke.md) | Deploy acceptance |
| [docs/release.md](docs/release.md) | Maintainer release cut |
| [docs/v1-product.md](docs/v1-product.md) | Frozen v1.0.0 target contract |
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
├── daemon/     # Windows/Linux: discover, history, journal, agent processes
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
# Optional Windows/Chromium screenshot regression
pnpm test:visual
```

Local run sketch:

```text
server:  go run ./cmd/server -port 8080 -pwa ../pwa/dist
daemon:  go run ./cmd/daemon
pwa:     pnpm dev
```

Details: [docs/development.md](docs/development.md).

## Current boundaries (v0.2)

These are stable product limits, not a todo list:

- Phone primarily resumes native threads. Any supported agent may expose agent-scoped `start_thread` only after its native starter is installed/probed; the phone keeps a local draft until its first prompt creates the native thread in the daemon's current union of discovered project directories.
- Codex is the only full-control agent (send, approve/deny, interrupt, steer, and full native attachments); the other four remain compatibility-resume adapters even when they advertise native thread start.
- v0.2 defaults every peer to `open` transport. Sealed transport is opt-in preview; one nest has one fixed mode and never downgrades automatically.
- Kimi CLI and Grok Build receive attachment **paths** in the prompt; reads depend on CLI permissions.
- Web Push needs VAPID; without it, no real push is sent.
- Daemon supports **Windows and Linux**; macOS remains later.
- In open mode the VPS stores application plaintext; treat it as sensitive.

## License

**Star And Thank Author License (SATA) 2.0**.

- Legal text: English [LICENSE](LICENSE)
- Convenience translation: [LICENSE_zh](LICENSE_zh) (not independently binding)

Please star this repository and thank the author before use, distribution, or modification.
