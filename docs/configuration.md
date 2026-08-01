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
| `NEKONEST_PHONE_SECRET` | Compatibility | Deprecated one-release alias for `NEKONEST_ADMIN_SECRET`. |
| `NEKONEST_BOOTSTRAP_TOKEN` | **Yes** | Protects `POST /api/devices/register` via `X-Neko-Bootstrap`. Must differ from the admin secret. |
| `NEKONEST_TRANSPORT_MODE` | No | Nest-wide `open` \| `sealed`; v0.2 defaults to `open`. Sealed is an explicit preview mode and every peer must match. |
| `NEKONEST_ALLOWED_ORIGINS` | Recommended | Comma-separated browser origin allowlist (e.g. `https://nekonest.example.com`). |
| `NEKONEST_TRUST_PROXY` | If behind reverse proxy | Set to `1` or `true` only when the reverse proxy **overwrites** `X-Forwarded-For` / `X-Real-IP`. Used for rate-limit client IP. |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | If proxy not on loopback | Comma-separated CIDRs/IPs of trusted reverse proxies when they are not loopback. |
| `NEKONEST_VAPID_PUBLIC_KEY` | Optional | Web Push VAPID public key (base64url). |
| `NEKONEST_VAPID_PRIVATE_KEY` | Optional | Web Push VAPID private key. |
| `NEKONEST_VAPID_SUBJECT` | Optional | Web Push contact, e.g. `mailto:you@example.com`. |

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
| `GET /health` | Liveness; body `{"status":"nyan~"}` | None |
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

### Environment variables (registration)

| Variable | When | Description |
|---|---|---|
| `NEKONEST_SERVER` | `-register` | VPS base URL, e.g. `https://nekonest.example.com` (http(s) is normalized to ws(s) for the dial) |
| `NEKONEST_BOOTSTRAP_TOKEN` | `-register` on public VPS | Same value as server `NEKONEST_BOOTSTRAP_TOKEN`; sent as `X-Neko-Bootstrap` |
| `NEKONEST_TRANSPORT_MODE` | All runs | `open` (v0.2 default) or `sealed`; must match the server and PWA build. |

Steady-state runs load credentials from the config file, not from these env vars.

### Config file

Default path: `%USERPROFILE%\.nekonest\config.json`

| Field | JSON key | Description |
|---|---|---|
| Server URL | `server_url` | `wss://…` or `ws://…` (http(s) accepted and normalized) |
| Device ID | `device_id` | Assigned at registration |
| Token | `token` | Device auth token; **secret** |
| Work dir | `work_dir` | Optional base directory hint for agent sessions |

Example shape (placeholders only):

```json
{
  "server_url": "wss://nekonest.example.com",
  "device_id": "device_…",
  "token": "…",
  "work_dir": ""
}
```

### Instance lock and journal

| Path | Purpose |
|---|---|
| `<config>.daemon.lock` | Single-instance lock; a second process with the same config is refused |
| Prompt journal (beside config, device-scoped) | Durable prompt accept/commit state for at-most-once delivery |

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
| `VITE_NEKONEST_TRANSPORT_MODE` | `open` | Must match the server and daemon. Set `sealed` only for an explicitly configured sealed preview nest. |

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

## Related docs

- [Security model](./security.md)
- [Architecture](./architecture.md)
- [VPS deploy](./deploy-vps.md)
- [Windows deploy](./deploy-windows.md)
- [Troubleshooting](./troubleshooting.md)
