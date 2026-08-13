> English | [简体中文](./configuration.zh-CN.md)

# Configuration reference

Canonical list of environment variables, CLI flags, config files, and operational limits for NekoNest v0.2.x. Product boundaries live in the root [README](../README.md); engineering invariants live in [AGENTS.md](../AGENTS.md).

## Server

Binary: `nekonest-server` (`server/cmd/server`).

### Flags

| Flag | Default | Description |
|---|---|---|
| `-port` | `8080` | Listen port |
| `-data` | `./data` | Data directory (SQLite DB + attachments) |
| `-pwa` | `./pwa-dist` | Built PWA static files directory |
| `-version` | — | Print the server application release and exit |

On POSIX hosts, the Server sets a private `0077` process umask, keeps the data
root at `0700`, and keeps SQLite DB/WAL/SHM files at `0600`. It exits before
opening the listener if those permissions cannot be enforced. Windows keeps
using the ACL of the account that runs the Server; POSIX mode bits are not a
substitute for a private Windows service-account ACL.

### Listen address

| Admin secret (`NEKONEST_ADMIN_SECRET` or compatibility alias) | Bind address |
|---|---|
| **Unset / empty** | `127.0.0.1:<port>` only (local development) |
| **Set** | `0.0.0.0:<port>` (`:<port>`) for public reverse-proxy deployment |

Do not put an unauthenticated server behind a LAN-facing proxy.

### Environment variables

| Variable | Required (public) | Description |
|---|---|---|
| `NEKONEST_ADMIN_SECRET` | **Yes** | Preferred admin bootstrap secret. It can authenticate directly and mint independent phone identities/tokens. |
| `NEKONEST_ADMIN_SECRET_FILE` | Managed runtime | Path to a private regular file containing the admin secret. Cannot be combined with either inline admin-secret variable; on non-Windows hosts group/other permissions are rejected. |
| `NEKONEST_PHONE_SECRET` | Compatibility | Deprecated one-release alias for `NEKONEST_ADMIN_SECRET`. |
| `NEKONEST_BOOTSTRAP_TOKEN` | **Yes** | Protects `POST /api/devices/register` via `X-Neko-Bootstrap`. Must differ from the admin secret. |
| `NEKONEST_TRANSPORT_MODE` | No | First start: optional `open` \| `sealed` selection (new DB defaults sealed). Later starts: an assertion that must match the immutable SQLite value. A legacy DB without metadata is persisted as open and cannot be switched by env. |
| `NEKONEST_ALLOWED_ORIGINS` | Recommended | Comma-separated browser origin allowlist (e.g. `https://nekonest.example.com`). |
| `NEKONEST_TRUST_PROXY` | If behind reverse proxy | Set to `1` or `true` only when the reverse proxy **overwrites** `X-Forwarded-For` / `X-Real-IP`. Used for rate-limit client IP. |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | If proxy not on loopback | Comma-separated CIDRs/IPs of trusted reverse proxies when they are not loopback. |
| `NEKONEST_VAPID_PUBLIC_KEY` | Optional | Web Push VAPID public key (base64url). |
| `NEKONEST_VAPID_PRIVATE_KEY` | Optional | Web Push VAPID private key. |
| `NEKONEST_VAPID_SUBJECT` | Optional | Web Push contact, e.g. `mailto:you@example.com`. |
| `NEKONEST_LOG_FORMAT` | No | Operator log format: `text` (default) or one JSON object per line (`json`). Invalid values stop startup. |
| `NEKONEST_LOG_LEVEL` | No | Minimum operator level: `debug`, `info` (default), `warn`, or `error`. Invalid values stop startup. |

#### Bootstrap token behavior

| Admin secret | Bootstrap token | Registration |
|---|---|---|
| Set | Set | Requires `X-Neko-Bootstrap` |
| Set | Empty | Registration **disabled** (503-style misconfig) |
| Empty (dev) | Empty | Registration open (dev only, loopback bind) |
| Empty (dev) | Set | Bootstrap still enforced if presented by server logic when configured |

### Data layout (server)

```text
<data>/
  nekonest.db          # SQLite
  attachments/         # uploaded files
```

Treat this directory as sensitive. Back it up with the same care as device tokens and message content.

### HTTP and WebSocket surfaces

| Path | Role | Auth |
|---|---|---|
| `GET /health` | Liveness plus `server_version`, `protocol_version`, and authoritative `transport_mode` | None |
| `GET /ws/phone` | Phone WebSocket | Phone secret |
| `GET /ws/daemon` | Daemon WebSocket | Device token after register |
| `GET /api/devices` | List devices | Phone secret |
| `POST /api/devices/register` | Daemon bootstrap register | Bootstrap token (public) |
| `GET /api/devices/sessions` | Sessions for a device | Phone secret |
| `GET /api/messages` | Message history API | Phone secret |
| `POST /api/attachments` | Upload attachment | Phone secret |
| `GET /api/attachments/{id}` | Download attachment | Phone secret (and daemon download path as implemented) |
| `POST /api/push/subscribe` | Web Push subscription | Phone secret |
| `GET /api/push/vapid-public-key` | Public VAPID key | Phone secret |
| `POST /api/pair/generate` | Issue pair code (daemon-side flow uses this) | Device auth as implemented |
| `POST /api/pair/consume` | Phone consumes 6-digit pair code | Phone secret |
| `/` and SPA assets | Built PWA when `-pwa` exists | N/A |

Full message types: [protocol.md](./protocol.md). Deploy guide: [deploy-vps.md](./deploy-vps.md).

---

## Daemon (Windows/Linux)

Binary: `nekonest-daemon.exe` (`daemon/cmd/daemon`).

### Flags

| Flag | Description |
|---|---|
| `-register` | Register this PC with the server (needs `NEKONEST_SERVER`) |
| `-name <string>` | Device display name used at registration |
| `-pair gen` | Generate a new 6-digit phone pair code for an already-registered device |
| `-config <path>` | Config file path (default: `%USERPROFILE%\.nekonest\config.json`) |
| `-doctor` | Run diagnostics, including daemon/server application-version alignment |
| `-version` | Print the daemon application release and exit |

### Environment variables (registration)

| Variable | When | Description |
|---|---|---|
| `NEKONEST_SERVER` | `-register` | Stable service base URL, e.g. `https://nekonest.example.com`; registration and `/ws/daemon` remain on this origin. |
| `NEKONEST_BOOTSTRAP_TOKEN` | `-register` on public VPS | Same value as server `NEKONEST_BOOTSTRAP_TOKEN`; sent as `X-Neko-Bootstrap` |
| `NEKONEST_TRANSPORT_MODE` | Registration / optional assertion | Registration reads the Server mode and persists it. If supplied, the value must match. Existing daemon configs without the field are legacy open. |
| `NEKONEST_LOG_FORMAT` | Every run | `text` (default) or one JSON object per line (`json`). |
| `NEKONEST_LOG_LEVEL` | Every run | `debug`, `info` (default), `warn`, or `error`. |

Registration requests include `registration_proof`, an Ed25519 signature over
a domain-separated, length-prefixed transcript containing the one-time
bootstrap credential, OS, daemon public keys, identity fingerprint, and
transport mode. Direct/self-hosted servers safely ignore this extra field.
Managed Cloud requires a valid proof when a fresh pairing credential attempts
to restore a previously revoked host record; first-time registration remains
compatible with older daemons.

Steady-state runs load credentials from the config file, not from these env vars.

Server and Daemon write operator logs only to stdout/stderr. Docker or journald
owns persistence and rotation; Windows launchers should redirect those streams.
JSON records always carry `time`, `level`, `msg`, `component`, and `event`.

### Config file

Default path: `%USERPROFILE%\.nekonest\config.json`

| Field | JSON key | Description |
|---|---|---|
| Service URL | `server_url` | Stable service endpoint (`wss://…` / `ws://…`) used for the daemon WebSocket |
| Device ID | `device_id` | Assigned at registration |
| Token | `token` | Device auth token; **secret** |
| Work dir | `work_dir` | Optional base directory hint for agent sessions |
| Transport mode | `transport_mode` | Immutable mode received from Server at registration; missing legacy field means open |

Example shape (placeholders only):

```json
{
  "server_url": "wss://nekonest.example.com",
  "device_id": "device_…",
  "token": "…",
  "work_dir": "",
  "transport_mode": "sealed"
}
```

The unreleased `control_plane_url`, `activation_poll_path`, and
`relay_generation` configuration shape is no longer supported. Loading one
fails with an explicit re-registration instruction. A normal v0.2.5
self-hosted `server_url` configuration continues to load unchanged.

### Instance lock and journal

| Path | Purpose |
|---|---|
| `<config>.daemon.lock` | Single-instance lock; a second process with the same config is refused |
| Prompt journal (beside config, device-scoped) | Durable prompt accept/commit state for at-most-once delivery |
| Prompt queue (beside config, device-scoped) | Durable per-session FIFO; at most 20 entries per session; running entries restart paused |

The phone pins the verified transport mode per web origin. A previously sealed
origin refuses open-mode downgrade, and the first connection to an
administrator-selected open relay requires an explicit in-app confirmation.

### Config hot-reload

The daemon watches the config path and replaces the in-memory `*Config` snapshot on change.

- Non-credential operational fields can take effect via snapshot replace.
- **Device identity and token are fixed for the lifetime of the process** at startup. Changing `device_id` / `token` requires a **restart** so connection auth and message identity cannot diverge.

### URL normalization

| Input | Stored / dial form |
|---|---|
| `https://host` | `wss://host` |
| `http://host` | `ws://host` |
| `wss://` / `ws://` | unchanged |
| bare `host:port` | `ws://host:port` |

REST calls use the http(s) form derived from the ws(s) URL.

Deploy guide: [deploy-windows.md](./deploy-windows.md).

---

## PWA

| Build variable | Default | Description |
|---|---|---|
| `VITE_NEKONEST_TRANSPORT_MODE` | unset | Development/build assertion only. The PWA reads `/health.transport_mode` before WebSocket; a supplied override that differs displays an error and stops connection. |
| `VITE_NEKONEST_MANAGED` | unset | Set to `true` only for an official managed build. Such a build requires deploy-time runtime config and refuses an open Relay. |

At startup the PWA reads `/runtime-config.json` from its own static origin with
`no-store`. The self-hosted file is `{}` and preserves same-origin behavior.
A managed deployment supplies:

```json
{
  "api_base": "https://connect.example.com",
  "ws_base": "wss://connect.example.com",
  "attachment_base": "https://connect.example.com",
  "push_base": "https://connect.example.com",
  "managed": true,
  "handoff_exchange_path": "/api/pwa/handoff/exchange"
}
```

`api_base`, `ws_base`, `attachment_base`, and `push_base` must be exact origins
without credentials, paths, query strings, or fragments. The latter three are
optional and fall back to `api_base`. The file is excluded from PWA precaching,
so placement never requires a client-visible backend URL or a PWA rebuild.

---

## Attachments and client limits

| Limit | Value |
|---|---|
| Max files per send | **5** |
| Max size per file | **4 MB** |
| Allowed MIME (server) | `image/jpeg`, `image/jpg`, `image/png`, `image/webp`, `image/gif`, `text/plain`, `text/markdown`, `application/pdf`, `application/json` |
| PWA image downscale edge | 1920 px (client-side, before upload when applicable) |
| PWA prompt outbox cap | 40 pending prompts |
| History fetch default/max (daemon) | up to **40** messages; content often truncated ~**4000** runes per item |

Agent-specific attachment wiring (native image flags vs path-in-prompt) is summarized in the [README](../README.md) agents table and [deploy-windows.md](./deploy-windows.md).

---

## Pairing

- Pair codes are **6 digits**.
- Server-side generation uses a short TTL (on the order of **5 minutes**).
- Phone consumes via pair API with phone secret; daemon prints the code after `-register` or `-pair gen`.

---

## Capability and queue runtime notes

- No agent CLI is installed or upgraded by NekoNest. A missing executable may leave native history browsable while `send`, `interrupt`, `queue`, and `spawn` are false with reason `cli_missing`.
- Queue v2 lives beside the daemon config and is security-sensitive. Back it up together with the daemon config before upgrades. Do not start an older daemon while a v2 queue has active or blocked items.
- Claude bridge-only capabilities are not configured by this release: the self-contained Bun/SDK packaging gate did not pass, so the existing Claude CLI fallback remains active and no Node/Bun runtime is required.

## Related docs

- [Security model](./security.md)
- [Architecture](./architecture.md)
- [VPS deploy](./deploy-vps.md)
- [Windows deploy](./deploy-windows.md)
- [Troubleshooting](./troubleshooting.md)
