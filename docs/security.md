> English | [简体中文](./security.zh-CN.md)

# Security model

How NekoNest trusts components, what secrets protect which surfaces, and what operators must assume about the VPS. This is an operator guide, not a formal threat model paper.

## Trust topology

```text
Phone PWA  ──HTTPS/WSS──►  VPS Server  ◄──outbound WSS──  Windows Daemon  ──►  local agent CLIs/stores
```

| Component | Trust role |
|---|---|
| **Phone** | Holds the phone shared secret; sees paired devices and session traffic for this nest |
| **VPS** | Authenticates phone and daemons; relays WebSocket traffic; **persists** devices, messages, and attachments |
| **Home PC / Daemon** | Initiates outbound connection only; reads native agent stores; runs headless CLIs |
| **Agent CLIs** | Authoritative session/history stores; execute tools with the user’s local privileges |

**There is no end-to-end encryption between phone and home PC.** The VPS can read metadata, message bodies, and attachment bytes it stores. Treat the VPS host, `data/` directory, backups, and reverse-proxy logs as **sensitive systems**.

## Product boundaries that affect security

- The phone **resumes** existing PC threads; it does not create remote agent sessions.
- The daemon **never requires inbound** ports on the home PC.
- Each agent’s **native local store** is authoritative for discovery and history.
- Tool approval depends on each agent’s **non-interactive** CLI; blocked work may require the PC terminal.

## Secrets and credentials

| Secret | Who holds it | Protects |
|---|---|---|
| `NEKONEST_PHONE_SECRET` | Operator + phone clients | Phone REST (`Authorization: Bearer` or `X-Neko-Secret`) and phone WebSocket |
| `NEKONEST_BOOTSTRAP_TOKEN` | Operator + daemon at register time | `POST /api/devices/register` (`X-Neko-Bootstrap`) |
| Device `token` in daemon `config.json` | Home PC only | Daemon WebSocket identity after registration |
| Pair code (6 digits) | Short-lived, printed by daemon | Binding a phone UI to a registered device |
| VAPID keys | Operator | Optional Web Push |

### Rules

1. **Phone secret ≠ bootstrap token.** Use two independent long random strings.
2. **Never commit** secrets, `config.json`, `data/`, attachment blobs, or native agent transcripts.
3. **Do not log** device tokens, phone secrets, bootstrap tokens, or full prompt bodies in shared logs.
4. Rotate phone secret and bootstrap token by coordinated redeploy; rotating device tokens means re-registering the daemon (new `config.json`).
5. Pair codes expire quickly (~5 minutes server-side). Prefer `-pair gen` over leaving old codes around.

## Development vs public mode

| Mode | When | Bind | Registration |
|---|---|---|---|
| **Local dev** | `NEKONEST_PHONE_SECRET` empty | Loopback only (`127.0.0.1`) | May be open if bootstrap also empty |
| **Public** | Phone secret set | All interfaces (behind TLS proxy) | Bootstrap token **required**; without it registration is disabled |

> [!WARNING]
> Never expose unauthenticated / open-registration mode to a public interface by forcing a non-loopback bind or by proxying loopback carelessly without auth.

## Authentication surfaces

### Phone

- REST: `Authorization: Bearer <NEKONEST_PHONE_SECRET>` or `X-Neko-Secret: <secret>`
- WebSocket: same secret (query `secret=` supported in client flows)
- Origin checks: when `NEKONEST_ALLOWED_ORIGINS` is set, only listed origins are accepted

### Daemon registration

- `POST /api/devices/register` with JSON body `{"name":"…"}` and header `X-Neko-Bootstrap: <token>` on public servers
- Response yields `device_id` + device token stored under `%USERPROFILE%\.nekonest\config.json`

### Daemon runtime

- Outbound WebSocket to `/ws/daemon` authenticates with the stored device token
- Single-instance lock on the config path reduces accidental double daemons sharing one identity

### Pairing

1. Daemon obtains a 6-digit code (register or `-pair gen`).
2. Phone user enters the code while authenticated with the phone secret.
3. Server associates the phone session with that device for listing and messaging.

## Reverse proxy and client IP

Rate limiting and related logic use `clientIP` helpers:

- By default, **`X-Forwarded-For` is ignored** (uses the direct connection address).
- Set `NEKONEST_TRUST_PROXY=1` **only** when the reverse proxy **overwrites** forwarded headers with the real client address (do not append untrusted client-supplied chains blindly).
- Prefer taking a **single trusted hop** (rightmost / proxy-controlled value as implemented).
- If the proxy is not on loopback, declare it with `NEKONEST_TRUSTED_PROXY_CIDRS`.

Caddy/Nginx examples: [deploy-vps.md](./deploy-vps.md).

## Attachments

- Phone upload authenticated with phone secret.
- Server enforces **4 MB** max and an allowlist of image/text/PDF/JSON MIME types.
- Client allows at most **5** files per send.
- Daemon downloads into a **per-run temporary directory** on Windows, then passes paths or native flags to the agent CLI.
- Attachment bytes reside on the VPS until cleaned up by operators/process lifecycle—assume durable exposure on disk.

## WebSocket and abuse controls

- Message size limits on authenticated frames (on the order of a few MiB for history-heavy payloads).
- REST bodies capped (e.g. ~1 MiB on general handlers; attachments have their own limit).
- One concurrent reader and one writer per gorilla/websocket connection (server design).
- Do not weaken body/frame limits without a deliberate capacity review.

## Prompt delivery integrity

Security-relevant delivery properties (not cryptography):

- `client_msg_id` plus accepted / committed / failed states give **at-most-once** semantics across reconnects.
- Transport success is **not** the same as agent acceptance.
- Daemon prompt journal **fail-closes** when it cannot safely tell whether a prompt was already accepted—prefer a visible failure over silent double execution.

Details: [architecture.md](./architecture.md).

## Home PC surface

- Daemon runs as the logged-in user and inherits that user’s access to agent stores and project directories.
- Headless CLIs may run tools with full local privileges of that user.
- Windows Job Objects are used so stop/interrupt can kill the process tree and avoid orphans.
- Optional Defender exclusions are a convenience for self-hosters; they **widen** local attack surface—use only if you understand the tradeoff.

## Operational hardening checklist

- [ ] TLS terminated at Caddy/Nginx; HSTS as appropriate for your domain
- [ ] `NEKONEST_PHONE_SECRET` and `NEKONEST_BOOTSTRAP_TOKEN` set, long, distinct
- [ ] `NEKONEST_ALLOWED_ORIGINS` set to the public HTTPS origin
- [ ] `NEKONEST_TRUST_PROXY=1` only with header-overwriting proxy config
- [ ] systemd (or equivalent) runs as a dedicated user; `data/` not world-readable
- [ ] Secrets via `EnvironmentFile` with restricted permissions, not world-readable unit files
- [ ] Firewall: only 80/443 public; app port (e.g. 8080) localhost-only
- [ ] Backups of `data/` encrypted and access-controlled
- [ ] Daemon `config.json` ACL limited to the PC user
- [ ] VAPID keys generated offline if Web Push is enabled
- [ ] No debug builds or open registration on the public host

## What NekoNest does not claim

- End-to-end encryption phone ↔ PC
- Zero-trust isolation between co-tenants on one VPS (single-operator self-host design)
- Perfect hiding of prompts from the VPS operator
- Phone-side sandboxing of agent tool execution (tools run on the PC)

## Related docs

- [Configuration](./configuration.md)
- [VPS deploy](./deploy-vps.md)
- [Troubleshooting](./troubleshooting.md)
- [Protocol overview](./protocol.md)
