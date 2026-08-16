> English | [简体中文](./security.zh-CN.md)

# Security

NekoNest is a single-operator, self-hosted bridge. It lets a phone send work to
coding agents that already run with the host user's permissions. Treat access
to the PWA as access to those agents and their reachable files.

## Trust path

```text
Phone PWA  ⇄  HTTPS/WSS  ⇄  VPS Server  ⇄  outbound daemon  ⇄  local agent CLI
```

- The host daemon initiates the connection; the home network needs no inbound
  port.
- The Server authenticates phones and hosts, relays traffic, and stores required
  state.
- Native agent stores and credentials remain on the host.
- TLS is required on public networks in every transport mode.

## Transport modes

| Mode | What the VPS can read |
|---|---|
| `sealed` | Routing metadata and encrypted application bodies. Prompt, response, approval, path, and title are encrypted between phone and daemon. Attachment bytes still transit the VPS as opaque blobs (no MIME sniffing in sealed mode); they are not yet end-to-end encrypted. |
| `open` | Application bodies stored or relayed by the Server are plaintext to the VPS operator. |

New data directories default to `sealed`. One data directory has one persisted
mode; changing an environment variable later does not convert it. Use `open`
only when you explicitly trust the VPS with application content.

Sealed mode reduces VPS exposure but does not hide connection timing, sizes,
device/session routing identifiers, or service availability. It also does not
protect against a compromised phone or host.

## Credentials

| Credential | Scope | Handling |
|---|---|---|
| Admin secret | Initial administration and phone bootstrap | Long, random, distinct from every other secret; prefer a private secret file where available. |
| Bootstrap token | New host registration | Keep off phones and rotate after suspected disclosure. |
| Phone token and keys | One phone identity and its paired hosts | Revoke a lost phone and pair a new identity. |
| Daemon token and identity | One registered host | Keep `~/.nekonest` private; revoke and re-register after compromise. |
| Agent credentials | Native CLI access on the host | Never copy them to the VPS or NekoNest config. |

Never commit or paste credentials, private keys, databases, attachments, native
transcripts, or daemon config into issues or logs.

## Public deployment checklist

- [ ] HTTPS/WSS terminates at a maintained reverse proxy.
- [ ] Only ports 80/443 are public; the NekoNest application port stays private.
- [ ] Admin secret and bootstrap token are long, random, and different.
- [ ] `NEKONEST_ALLOWED_ORIGINS` contains only the intended HTTPS origin.
- [ ] `NEKONEST_TRUST_PROXY=1` is used only with a proxy that overwrites client
      forwarding headers.
- [ ] The Server runs as an unprivileged user or the hardened non-root container.
- [ ] The Server data directory and secret files are private to that identity.
- [ ] Backups are encrypted, access-controlled, and restoration-tested.
- [ ] Daemon config and identity files are readable only by the host user.
- [ ] Debug logging is temporary and logs are access-controlled.
- [ ] Web Push keys are generated and stored like other service credentials.

The supplied Compose file uses a non-root image, read-only root filesystem,
dropped capabilities, and a private data mount. Preserve those controls when
customizing it.

## Data and content

- The Server data directory contains security-sensitive database and attachment
  state even in sealed mode. Back it up as one unit.
- Uploaded files and rendered Markdown are untrusted input. The PWA sanitizes
  Markdown, but operators must still keep browsers and dependencies updated.
- Agent tools execute on the host with the daemon user's permissions. Use a
  dedicated OS account or restricted agent sandbox when the project requires a
  smaller filesystem boundary.
- A missing or unsupported control must stay disabled. Do not bypass capability
  checks to force approval, steering, interruption, or file access.

## If a credential is exposed

1. Remove public access or stop the affected component.
2. Revoke the affected phone or host identity where possible.
3. Rotate the admin secret or bootstrap token as applicable and restart the
   Server.
4. Re-register and re-pair affected devices.
5. Review access-controlled logs and restore only from a known-good backup.

## Related

- [VPS deploy](./deploy-vps.md)
- [Configuration](./configuration.md)
- [Troubleshooting](./troubleshooting.md)
- [Acceptance checklist](./e2e-smoke.md)
