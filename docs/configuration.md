> English | [简体中文](./configuration.zh-CN.md)

# Configuration

This page lists supported operator settings. The installed binary's `-help`
output is authoritative for flags; `compose.yaml` and `docker.env.example` are
authoritative for the container deployment.

## Server

Common flags:

| Flag | Default | Purpose |
|---|---|---|
| `-port` | `8080` | HTTP/WebSocket listen port |
| `-data` | `./data` | SQLite and attachment directory |
| `-pwa` | `./pwa-dist` | Built PWA directory |
| `-version` | — | Print the application version |

### Authentication for a public server

| Variable | Purpose |
|---|---|
| `NEKONEST_ADMIN_SECRET` | Initial administrator credential. Use a long random value. |
| `NEKONEST_ADMIN_SECRET_FILE` | Read the administrator credential from a private regular file instead of an inline variable. Do not set both forms. |
| `NEKONEST_BOOTSTRAP_TOKEN` | Authorizes new host registration. It must differ from the admin secret. |
| `NEKONEST_ALLOWED_ORIGINS` | Comma-separated browser origins, normally the public HTTPS URL. |

Set exactly one of `NEKONEST_ADMIN_SECRET` or
`NEKONEST_ADMIN_SECRET_FILE`, plus the bootstrap token and allowed origin.
Without an admin secret the Server binds to loopback for local development.
With an admin secret but no bootstrap token, new host registration is disabled.
`NEKONEST_PHONE_SECRET` is a deprecated compatibility alias; new deployments
should not use it.

### Optional

| Variable | Purpose |
|---|---|
| `NEKONEST_TRANSPORT_MODE` | Select `sealed` or `open` only when creating a new data directory; later it must match the stored mode. New data defaults to `sealed`. |
| `NEKONEST_TRUST_PROXY` | Set to `1` only behind a trusted proxy that overwrites client forwarding headers. |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | Trusted proxy networks when the proxy connection is not from loopback. |
| `NEKONEST_VAPID_PUBLIC_KEY` | Web Push public key. |
| `NEKONEST_VAPID_PRIVATE_KEY` | Web Push private key. |
| `NEKONEST_VAPID_SUBJECT` | Web Push contact, such as `mailto:operator@example.com`. |
| `NEKONEST_LOG_FORMAT` | `text` (default) or `json`. |
| `NEKONEST_LOG_LEVEL` | `debug`, `info` (default), `warn`, or `error`. |

Configure all three VAPID values or leave Web Push disabled. Use `debug` only
for a bounded troubleshooting window.

## Host daemon

Common commands:

| Command | Purpose |
|---|---|
| `nekonest-daemon -register -name "Home PC"` | Register a host and print pairing material |
| `nekonest-daemon -pair gen` | Generate a new phone pairing code |
| `nekonest-daemon -doctor` | Check config, server reachability, and installed agents |
| `nekonest-daemon install` | Register current-user autostart (Windows logon task or Linux systemd user unit) |
| `nekonest-daemon start` | Start the installed supervisor |
| `nekonest-daemon stop` | Stop the installed supervisor |
| `nekonest-daemon status` | Show supervisor and process-lock state |
| `nekonest-daemon uninstall` | Remove the current-user autostart registration |
| `nekonest-daemon -config <path>` | Use a non-default config file |
| `nekonest-daemon -version` | Print the application version |

`install` / `start` / `stop` / `status` / `uninstall` talk to the current-user
supervisor. They do not keep a second CLI process resident. A non-default
`-config` path gets its own task or unit name.

Native thread-start probes are activity-aware. A running, attention-blocked, or
very recently active agent is refreshed every five minutes; other agents used
within seven days are refreshed hourly; agents without a visible thread in the
last seven days are refreshed daily. Daemon startup performs one initial probe.
Session discovery and phone reconnects do not bypass these intervals. An actual
`start_thread` request always performs a fresh fail-closed probe before launch.

Registration reads these variables:

| Variable | Purpose |
|---|---|
| `NEKONEST_SERVER` | Public nest URL, for example `https://nekonest.example.com`. |
| `NEKONEST_BOOTSTRAP_TOKEN` | Same registration token configured on the Server. |
| `NEKONEST_TRANSPORT_MODE` | Optional assertion; if set, it must match the Server. |

Optional compatibility-adapter overrides:

| Variable | Purpose |
|---|---|
| `NEKONEST_CURSOR_CLI` | Absolute path to Cursor Agent CLI (`cursor-agent`). Do not point this at `cursor.exe` or a generic `agent.exe` such as Grok Build. Cursor IDE's `CURSOR_AGENT` marker is ignored. |
| `ZCODE_CLI` | Optional path to the ZCode CLI (`zcode` / `zcode.cjs`, not `ZCode.exe`). Current catalogs still do not advertise ZCode while headless login is broken upstream. |

Both Server and daemon also accept `NEKONEST_LOG_FORMAT` and
`NEKONEST_LOG_LEVEL`. Normal daemon runs use credentials already stored in the
config file; registration variables do not need to remain in the launcher.

## Data and backup

| Location | Contains | Backup rule |
|---|---|---|
| Server `-data` directory | Database and uploaded attachments | Stop or quiesce the Server and back up the directory as one unit. |
| `~/.nekonest/config.json` | Server URL and device credential | Keep private; back up before daemon upgrades. |
| `~/.nekonest/identity.json` | Host identity key | Keep private and preserve across re-registration or recovery. |
| Files beside the daemon config | Prompt journal, queue, and instance lock | Preserve them with the config; do not edit by hand. |

The default daemon directory is `%USERPROFILE%\.nekonest` on Windows and
`~/.nekonest` on Linux. A custom `-config` path keeps related identity and state
beside that file. Only one daemon process may use a config at a time.

Do not hand-edit device ids, tokens, transport mode, journals, or queue files.
Re-register when credentials must change.

## User-visible limits

- Up to 5 attachments per prompt, 4 MB each.
- Supported uploads: JPEG, PNG, WebP, GIF, TXT, Markdown, PDF, and JSON.
- Native agent capabilities still decide how each attachment is delivered.
- The phone only enables controls currently advertised by the daemon.

## Related

- [VPS deploy](./deploy-vps.md)
- [Windows host](./deploy-windows.md)
- [Linux host](./deploy-linux.md)
- [Security](./security.md)
- [Troubleshooting](./troubleshooting.md)
