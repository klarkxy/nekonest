> English | [简体中文](./troubleshooting.zh-CN.md)

# Troubleshooting

Start with the first failing boundary: browser → reverse proxy → Server →
daemon → native agent. Do not expose secrets or native transcripts while
collecting evidence.

## PWA does not open or rejects setup

1. Check `https://your-nest/health`.
2. Confirm the reverse proxy supports WebSocket upgrades and routes to the
   private Server port.
3. Confirm `NEKONEST_ALLOWED_ORIGINS` exactly includes the browser origin.
4. Re-enter the admin secret. A rejected value must not be stored as a valid
   phone credential.
5. If the PWA looks stale after an upgrade, fully close it, reopen it, and then
   hard-refresh once.

An intentional first connection to an open nest asks for confirmation; it is
not a transport mismatch.

## Daemon cannot register

- `NEKONEST_SERVER` must be the reachable HTTPS base URL.
- `NEKONEST_BOOTSTRAP_TOKEN` must match the Server value.
- A public Server with an admin secret but no bootstrap token refuses new
  registrations.
- The host clock and TLS trust store must be valid.
- A transport-mode assertion, if set, must match the Server's persisted mode.

Run `nekonest-daemon -doctor` before editing any files.

## Host stays offline

1. Confirm exactly one daemon process uses the config.
2. Check daemon logs for authentication, TLS, or reconnect errors.
3. Confirm the host can make outbound WSS connections to the nest.
4. Confirm the Server is healthy and not restarting.
5. If credentials were revoked or copied from another installation, revoke the
   old host and register again instead of hand-editing `config.json`.

## No projects or threads appear

- Use a supported agent on the host first so a native thread exists.
- Confirm the agent CLI and its native store belong to the same OS user as the
  daemon.
- Run `-doctor` and inspect the unavailable reason shown by the PWA.
- Refresh the device page after the daemon is online.
- Older, subagent, sidechain, or synthetic-only records may be intentionally
  hidden. Reopen an old main thread on the host to make it active again.
- Threads without a recognized directory appear under **Uncategorized**.

NekoNest does not browse arbitrary folders or create nest-only ghost threads.
Phone-side new thread creation appears only when the selected agent advertises
it for an already discovered project.

## A control is disabled

The running daemon's advertised capability is authoritative. Common causes are
a missing CLI, a failed native probe, a compatibility-only agent path, or an
agent request that can only be completed in the host terminal.

Do not bypass the disabled state. Run `-doctor`, update the relevant agent CLI
if appropriate, and reconnect the daemon.

## Prompt is stuck or delivery is uncertain

1. Check whether the thread is already running or waiting for input/approval.
2. Use Interrupt only when the PWA enables it; otherwise inspect the host
   process and terminal.
3. Keep the daemon running through a reconnect. Do not resend the same action
   with a newly invented id when the first prompt may already have crossed the
   agent boundary.
4. If NekoNest reports an indeterminate or blocked queue item, follow the
   explicit Resume/Skip action shown by the PWA instead of editing state files.

## Attachments fail

- Limit each prompt to 5 files and each file to 4 MB.
- Use JPEG, PNG, WebP, GIF, TXT, Markdown, PDF, or JSON.
- Check Server disk space and daemon access to the temporary download location.
- Agent sandboxes may reject local paths even after upload succeeds. The PWA
  shows only the attachment tier advertised by that agent.

## Phone approvals or questions do not appear

Only native, current agent events create approval or structured-input UI. On a
compatibility path, finish the request in the host terminal. If the PWA had the
control before a reconnect, run `-doctor` and wait for a fresh capability
catalog rather than assuming it is still available.

## Web Push does not arrive

- Configure all three VAPID variables on the Server.
- Use HTTPS and grant browser notification permission.
- Reopen the PWA and recreate the subscription after rotating VAPID keys.
- Push is optional; verify the in-app workflow separately.

## Server will not start

| Symptom | Likely cause |
|---|---|
| Binds only to loopback | No admin secret; this is the safe development mode. |
| Registration disabled | Admin secret is set but bootstrap token is empty. |
| Transport mismatch | Environment assertion differs from the mode stored in the data directory. |
| Permission error | The Server identity cannot privately own the data directory or secret file. |
| Rate limits use the wrong client | Proxy trust is enabled without correct forwarding-header replacement or trusted CIDRs. |

## Collect useful logs

1. Reproduce once with `NEKONEST_LOG_FORMAT=json`.
2. Use `NEKONEST_LOG_LEVEL=debug` only briefly.
3. Record component versions, `/health`, daemon `-doctor`, and the first failing
   boundary.
4. Redact secrets, tokens, private keys, paths, prompts, attachments, and native
   transcripts before sharing.

## Related

- [Configuration](./configuration.md)
- [Security](./security.md)
- [Windows host](./deploy-windows.md)
- [Linux host](./deploy-linux.md)
- [Acceptance checklist](./e2e-smoke.md)
