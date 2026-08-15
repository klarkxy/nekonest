> English | [简体中文](./README.zh-CN.md)

# NekoNest documentation

These are the live docs for the current self-hosted product. Historical target
contracts and unused migration notes are frozen under [archive/](./archive/).

## Choose your path

| Goal | Read |
|---|---|
| Understand NekoNest and try it | [Project README](../README.md) |
| Install a public nest | [VPS](./deploy-vps.md) → [Windows host](./deploy-windows.md) or [Linux host](./deploy-linux.md) → [acceptance](./e2e-smoke.md) |
| Operate an existing nest | [Configuration](./configuration.md), [security](./security.md), and [troubleshooting](./troubleshooting.md) |
| Change the project | [Development](./development.md), [architecture](./architecture.md), and [protocol](./protocol.md) |
| Cut a release | [Release process](./release.md) |

## User and operator guides

| Document | Purpose |
|---|---|
| [VPS deploy](./deploy-vps.md) | Run the Server and PWA behind HTTPS |
| [Windows host](./deploy-windows.md) | Register the host and manage autostart with `nekonest-daemon install` |
| [Linux host](./deploy-linux.md) | Register the host and manage the systemd user unit with `nekonest-daemon install` |
| [Configuration](./configuration.md) | Supported operator settings and data locations |
| [Security](./security.md) | Trust model, secrets, backups, and hardening |
| [Troubleshooting](./troubleshooting.md) | Symptom-first recovery steps |
| [Acceptance checklist](./e2e-smoke.md) | Verify an installed or upgraded nest |
| [Agent support](./agent-capability-matrix.md) | Stable support tiers and runtime capability rules |

## Contributor and maintainer references

| Document | Purpose |
|---|---|
| [Architecture](./architecture.md) | Stable component and data-ownership boundaries |
| [Protocol](./protocol.md) | Compatibility rules and authoritative schema locations |
| [Development](./development.md) | Local setup and verification commands |
| [Release](./release.md) | Maintainer release gates |
| [Relay Core](./relay-core.md) | Shared self-hosted/managed data-plane boundary |
| [Brand assets](./brand-art.md) | Rebuild shipped images from approved source art |

The [Chinese community introduction](./zhihu-intro.zh-CN.md) is publication
copy, not an operational reference.

## Sources of truth

| Subject | Authority |
|---|---|
| Product behavior | Root README and the running UI |
| Available controls | PWA capability state and `nekonest-daemon -doctor` |
| Server/daemon flags | The installed binary's `-help` output |
| Container settings | `compose.yaml` and `docker.env.example` |
| Wire fields and messages | `protocol/protocol.json` |
| Release automation | `.github/workflows/release.yml` |

Docs explain how to use those surfaces; they do not duplicate every internal
field or implementation branch. English files are primary. Operator-facing
changes must update the matching `.zh-CN.md` mirror in the same change.
