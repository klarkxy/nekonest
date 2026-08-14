<div align="center">
  <img src="./pwa/public/brand/nekonest-duo.webp" width="360" alt="NekoNest">

  <h1>NekoNest · 猫娘乐园</h1>

  <p><strong>Safely continue coding-agent threads on your Windows or Linux host — from your phone.</strong></p>
  <p>Self-hosted · Host outbound-only · Native session stores · Mobile PWA</p>

  <p>
    <a href="./README.zh-CN.md">简体中文</a> ·
    <a href="#quick-start">Quick start</a> ·
    <a href="#supported-agents">Agents</a> ·
    <a href="#documentation">Docs</a> ·
    <a href="#license">License</a>
  </p>
  <p>
    <a href="https://github.com/klarkxy/nekonest/actions/workflows/ci.yml"><img src="https://github.com/klarkxy/nekonest/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
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
                    ┌────────────┬───────────┬───┴────────┬────────────┐
                    │Claude Code │   Codex   │ Kimi CLI   │ Grok Build │
                    └────────────┴───────────┴────────────┴────────────┘
```

The home PC needs neither a public IP nor inbound ports. The daemon opens an outbound WebSocket to the VPS; the phone only talks to the HTTPS/WSS nest.

## Features

- **Recent native thread discovery** — browse the last 7 days of activity as `directory → agent → thread`; actionable running/waiting threads stay visible, orphans land in **未分类** (Uncategorized), and hidden old threads remain untouched in their native stores.
- **Reliable resume** — independent accepted / committed / failed delivery states; transport success is not agent acceptance.
- **History + streaming** — merge native history, server durability, and live output with stable message ids; CLI stderr stays diagnostic.
- **Attachments** — phone upload → daemon per-run temp dir → agent-specific wiring (max 5 files, 4 MB each).
- **Capability-gated agent control** — Codex remains the only full-control agent. Every reliable installed send path may use NekoNest's durable FIFO (not an agent-native queue); approval, user input, start, interrupt, and attachments remain independently probed and default closed.
- **Persistent transport mode** — one immutable `open` or `sealed` mode per nest. New databases default to sealed; legacy databases without mode metadata are persisted as open; mismatches fail closed.
- **Phone-side downgrade guard** — the PWA pins mode per origin; a sealed origin cannot silently become open, and first use of an intentional open relay requires explicit confirmation.
- **Fresh phone catalog** — the PWA first renders the cached catalog, then asks an online daemon to rescan its native stores. Only agents with threads render as groups; enabled missing agents remain available from the project's **New** menu.
- **Mobile UX** — installable PWA, drafts, per-thread or whole-project phone-local archive, sanitized Markdown, reconnect outbox, optional Web Push.
- **Version diagnostics** — compare the loaded PWA with the live server at page level; each machine reports its own daemon release and update state on its device card.
- **Safe defaults** — admin bootstrap, revocable phone identities, daemon registration token, origin checks, attachment validation, size limits, controlled proxy trust.

## Supported agents

| Agent | Local session source | Control | Attachments |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | Compatibility resume via `claude --resume` | Authorize run temp dir; paths in prompt |
| Codex | `~/.codex/sessions` | **Full control** via `codex app-server`; `exec resume` fallback | Native images and files when app-server is healthy |
| Kimi CLI | `.kimi-code` (legacy `.kimi`) | Compatibility resume via `kimi --session` | Paths in prompt; CLI file permissions apply |
| Grok Build | `~/.grok/sessions` | Compatibility resume via `grok --resume` | Paths in prompt; non-interactive safe mode |

### Capability implementation status (live v0.2)

Legend: ✅ implemented and advertised · ⚙️ implemented with a runtime, probe, or fallback limitation · ❌ not implemented/advertised on the phone path.

| Capability | Claude Code | Codex | Kimi CLI | Grok Build |
|---|---|---|---|---|
| Discover / list | ✅ | ✅ | ✅ | ✅ |
| Ownership gate | ✅ | ✅ | ✅ | ✅ |
| History | ✅ | ✅ | ✅ | ✅ |
| Send + stream | ✅ | ✅ | ✅ | ✅ |
| Interrupt | ✅ | ✅ | ✅ | ✅ |
| Start native thread | ⚙️ starter probe | ⚙️ healthy app-server | ⚙️ ACP starter probe | ⚙️ starter probe |
| Image / file attachments | ⚙️ path best-effort | ⚙️ native image + same-turn materialized file path; fallback is image-only | ⚙️ path best-effort | ⚙️ path best-effort |
| Approve / deny | ❌ | ⚙️ app-server only | ❌ | ❌ |
| Steer active turn | ❌ | ⚙️ app-server only | ❌ | ❌ |
| NekoNest durable FIFO | ⚙️ CLI + writable queue journal | ⚙️ app-server or exec fallback + writable journal | ⚙️ installed CLI + writable journal | ⚙️ installed CLI + writable journal |
| Waiting-state signals | ❌ without a bridge signal | ⚙️ approval + structured user input via app-server | ❌ unless a valid ACP event is observed | ❌ unless a valid vendor event is observed |

Runtime-gated rows are advertised only after the installed CLI/control path passes its probe. Native thread start is a device-level `start_capabilities` entry with its own `attachment_mode`, not a promise that every existing session has `capabilities.spawn=true`. Protocol 1.2 sends every boolean explicitly plus stable `unavailable_reasons`; a new PWA infers legacy send/interrupt only from a confirmed 1.1 daemon producer. Unknown sources fail closed. Codex falls back to `exec resume` when app-server is unhealthy; non-Codex `steer` remains disabled.

A missing CLI or empty store for one agent does not disable the others.

Active wire ids: `claude_code`, `codex`, `kimi_cli`, `grok_build`. Protocol 1.x still parses the retired `kilo` id so mixed-version peers fail closed instead of breaking the connection; current catalogs never advertise it.

**Full per-harness matrix** (live flags, start probes, attachment wiring, live vs v1): [docs/agent-capability-matrix.md](docs/agent-capability-matrix.md) · [中文](docs/agent-capability-matrix.zh-CN.md).

## Quick start

### 1. Install and run the server (VPS)

[GHCR](https://github.com/klarkxy/nekonest/pkgs/container/nekonest-server)
publishes a non-root `linux/amd64` + `linux/arm64` image containing both the
Server and matching PWA. The daemon is intentionally **not** containerized.

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest
cp docker.env.example .env
# Edit .env, then keep it private.
chmod 600 .env
sudo install -d -m 700 -o 10001 -g 10001 data
docker compose pull
docker compose up -d
docker compose logs -f server
```

Compose refuses to create a missing host data path. On Linux, the Server keeps
the data root at mode `0700` and SQLite DB/WAL/SHM files at `0600`; startup
fails before listening if those private permissions cannot be enforced.

[GitHub Releases](https://github.com/klarkxy/nekonest/releases/latest) provide
Linux amd64 and arm64 Server archives. Each archive includes the matching
`pwa-dist`, English/Chinese READMEs, licenses, and the version marker; no
Node.js or Go toolchain is needed for the prebuilt path.

```bash
# Replace amd64 with arm64 on an ARM VPS.
asset=nekonest-server-linux-amd64.tar.gz
base=https://github.com/klarkxy/nekonest/releases/latest/download
curl -fLO "$base/$asset"
curl -fLO "$base/checksums.txt"
grep "  $asset$" checksums.txt | sha256sum -c -

mkdir -p nekonest-server
tar -xzf "$asset" -C nekonest-server
cd nekonest-server
./nekonest-server -version

export NEKONEST_ADMIN_SECRET='long-random-string'
export NEKONEST_BOOTSTRAP_TOKEN='another-long-random-string'
./nekonest-server -port 8080 -data ./data -pwa ./pwa-dist
```

To build from source instead, install Go 1.22+, Node.js, and pnpm:

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest/pwa
pnpm install --frozen-lockfile
pnpm build

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server
export NEKONEST_ADMIN_SECRET='long-random-string'
export NEKONEST_BOOTSTRAP_TOKEN='another-long-random-string'
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

A new data directory is initialized as `sealed`. Set `NEKONEST_TRANSPORT_MODE=open` only on the first start when intentionally creating an administrator-selected open nest. Later values are assertions and must match the persisted mode.

Terminate public HTTPS/WSS at the reverse proxy to `127.0.0.1:8080`. Full systemd/Caddy/Nginx notes: [docs/deploy-vps.md](docs/deploy-vps.md).

### 2. Install, register, and run the daemon (Windows/Linux)

Install and use at least one supported agent CLI so native threads exist.

`nekonest-daemon-windows-amd64.zip` is the Windows package. Linux hosts use
`nekonest-daemon-linux-amd64.tar.gz` or
`nekonest-daemon-linux-arm64.tar.gz`.

```powershell
$asset = "nekonest-daemon-windows-amd64.zip"
$base = "https://github.com/klarkxy/nekonest/releases/latest/download"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt

$line = Get-Content .\checksums.txt | Where-Object { $_.EndsWith("  $asset") }
$expected = ($line -split '\s+')[0].ToLowerInvariant()
$actual = (Get-FileHash $asset -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "SHA-256 checksum mismatch" }

Expand-Archive $asset -DestinationPath .\nekonest-daemon -Force
Set-Location .\nekonest-daemon
.\nekonest-daemon.exe -version

$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "same-bootstrap-token-as-vps"
.\nekonest-daemon.exe -register -name "Study PC"
.\nekonest-daemon.exe
```

Linux installation uses the same checksum file and archive layout:

```bash
# Replace amd64 with arm64 on an ARM host.
asset=nekonest-daemon-linux-amd64.tar.gz
base=https://github.com/klarkxy/nekonest/releases/latest/download
curl -fLO "$base/$asset"
curl -fLO "$base/checksums.txt"
grep "  $asset$" checksums.txt | sha256sum -c -
mkdir -p nekonest-daemon
tar -xzf "$asset" -C nekonest-daemon
cd nekonest-daemon
./nekonest-daemon -version
export NEKONEST_SERVER='https://nekonest.example.com'
export NEKONEST_BOOTSTRAP_TOKEN='same-bootstrap-token-as-vps'
./nekonest-daemon -register -name 'Study PC'
./nekonest-daemon
```

For a source build, clone the repository and run
`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-daemon ./cmd/daemon`
from `daemon/`.

Registration writes one stable service endpoint to the host config. For
self-hosting that endpoint is the standalone Server; a managed service may use
the same origin while placement is still provisioning. The daemon never polls
a second control plane or accepts a replacement Relay URL. Generate a new code with
`.\nekonest-daemon.exe -pair gen` on Windows or
`./nekonest-daemon -pair gen` on Linux. Autostart:
[Windows](docs/deploy-windows.md) · [Linux](docs/deploy-linux.md).

The daemon also signs each registration request with its long-term Ed25519
identity. Managed Cloud uses that proof only when restoring a previously
revoked host record: keep `identity.json`, remove the obsolete `config.json`,
and register with a fresh one-time Cloud credential. Losing `identity.json`
creates a new host identity instead of silently taking over the old record.

The open standalone Server is Cloud-unaware and does not read account,
subscription, slot, placement, or tenant-manifest state. Managed deployments
compose the same open Relay Core behind their own authorization and placement
layer; see [Relay Core boundary](docs/relay-core.md).

### 3. Pair the phone

1. Open `https://nekonest.example.com` and enter `NEKONEST_ADMIN_SECRET`. The PWA probes `GET /api/devices` before entering; a wrong key stays on setup. An intentional open nest asks for an explicit confirm.
2. **Pair computer** with the 6-digit code.
3. Confirm the device is online; open **directory → agent → thread**.
4. Send prompts and optional images / TXT / Markdown / PDF / JSON.

Acceptance checklist: [docs/e2e-smoke.md](docs/e2e-smoke.md).

## Configuration (summary)

| Variable | Role |
|---|---|
| `NEKONEST_ADMIN_SECRET` | Admin bootstrap and phone-token minting (**required** on public VPS) |
| `NEKONEST_ADMIN_SECRET_FILE` | Cloud-managed private-file alternative to the inline admin secret |
| `NEKONEST_PHONE_SECRET` | Deprecated compatibility alias for the admin secret |
| `NEKONEST_BOOTSTRAP_TOKEN` | Daemon register gate (**required** on public VPS; ≠ phone secret) |
| `NEKONEST_TRANSPORT_MODE` | Optional first-start choice / later assertion; new DB defaults `sealed`, legacy DB is fixed `open` |
| `NEKONEST_ALLOWED_ORIGINS` | Browser origin allowlist |
| `NEKONEST_TRUST_PROXY` | `1` only behind a proxy that **overwrites** XFF |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | Trusted proxy CIDRs when proxy is not loopback |
| `NEKONEST_VAPID_*` | Optional Web Push |
| `NEKONEST_SERVER` | Daemon register: VPS URL |
| `NEKONEST_LOG_FORMAT` | `text` (default) or one-JSON-object-per-line `json` |
| `NEKONEST_LOG_LEVEL` | `debug`, `info` (default), `warn`, or `error` |

> [!WARNING]
> Without an admin secret the server binds **loopback only** for local development. Do not expose unauthenticated mode publicly.

Full flags, `config.json` fields, routes, and limits: [docs/configuration.md](docs/configuration.md). Trust model: [docs/security.md](docs/security.md).

The Server persists the selected mode in SQLite and exposes it from `/health`; the PWA reads that runtime value before opening WebSocket. Existing open nests remain open. Moving one to sealed requires the offline backup-and-wipe migration and re-pairing; setting an environment variable cannot silently convert it.

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

Three Go modules (`relaycore/`, `server/`, `daemon/`) share a root Go workspace, alongside one pnpm app (`pwa/`); there is no root Go module. Protocol types are manual—see [docs/protocol.md](docs/protocol.md).

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
- Codex is the only full-control agent (send, approve/deny, interrupt, steer, and full native attachments); the other three remain compatibility-resume adapters even when they advertise native thread start.
- New nests default to sealed; existing databases/configs without mode metadata are classified once as open. One nest has one persisted mode and never downgrades automatically.
- Kimi CLI and Grok Build receive attachment **paths** in the prompt; reads depend on CLI permissions.
- Web Push needs VAPID; without it, no real push is sent.
- Daemon supports **Windows and Linux**; macOS remains later.
- In open mode the VPS stores application plaintext; treat it as sensitive.

## License

**Star And Thank Author License (SATA) 2.0**.

- Legal text: English [LICENSE](LICENSE)
- Convenience translation: [LICENSE_zh](LICENSE_zh) (not independently binding)

Please star this repository and thank the author before use, distribution, or modification.
