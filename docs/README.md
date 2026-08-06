> English | [简体中文](./README.zh-CN.md)

# NekoNest documentation

Live operator and contributor docs for NekoNest. Product overview: [../README.md](../README.md) · [../README.zh-CN.md](../README.zh-CN.md).

English files use the short path (`docs/foo.md`). Simplified Chinese mirrors use the `.zh-CN.md` suffix.

## Start here

| Doc | Description |
|---|---|
| [../README.md](../README.md) | Product, quick start, **v0.2 live boundaries** |
| [v1-product.md](./v1-product.md) | **v1.0.0 frozen target contract** (Codex-first E2E; beyond the live v0.2 milestone) |
| [deploy-vps.md](./deploy-vps.md) | Build and run the VPS server |
| [deploy-windows.md](./deploy-windows.md) | Register and run the Windows daemon |
| [deploy-linux.md](./deploy-linux.md) | Register and run the Linux daemon (systemd user) |
| [migration-v1.md](./migration-v1.md) | Breaking v0.1 → v1.0 offline migration |
| [e2e-smoke.md](./e2e-smoke.md) | Post-deploy acceptance checklist |
| [troubleshooting.md](./troubleshooting.md) | Symptom → fix guide |

## Reference

| Doc | Description |
|---|---|
| [v1-product.md](./v1-product.md) | Frozen v1.0.0 catalog: Windows+Linux, Codex full-control, sealed default |
| [configuration.md](./configuration.md) | Env vars, flags, config files, limits |
| [security.md](./security.md) | Trust model, secrets, hardening |
| [agent-capability-matrix.md](./agent-capability-matrix.md) | **Per-harness live capability matrix** (Codex / Claude / Kilo / Kimi / Grok) |
| [architecture.md](./architecture.md) | Components, discovery, prompt path |
| [protocol.md](./protocol.md) | Wire envelope, message types, REST |
| [development.md](./development.md) | Local dev and tests |
| [release.md](./release.md) | Maintainer release cut |
| [brand-art.md](./brand-art.md) | Rebuild PWA brand assets |
| [../CHANGELOG.md](../CHANGELOG.md) | User-visible history |
| [../AGENTS.md](../AGENTS.md) | Engineering invariants (EN) |

## Chinese mirrors

| English | 简体中文 |
|---|---|
| [README.md](../README.md) | [README.zh-CN.md](../README.zh-CN.md) |
| [v1-product.md](./v1-product.md) | [v1-product.zh-CN.md](./v1-product.zh-CN.md) |
| [deploy-vps.md](./deploy-vps.md) | [deploy-vps.zh-CN.md](./deploy-vps.zh-CN.md) |
| [deploy-linux.md](./deploy-linux.md) | [deploy-linux.zh-CN.md](./deploy-linux.zh-CN.md) |
| [migration-v1.md](./migration-v1.md) | [migration-v1.zh-CN.md](./migration-v1.zh-CN.md) |
| [deploy-windows.md](./deploy-windows.md) | [deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md) |
| [e2e-smoke.md](./e2e-smoke.md) | [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) |
| [configuration.md](./configuration.md) | [configuration.zh-CN.md](./configuration.zh-CN.md) |
| [security.md](./security.md) | [security.zh-CN.md](./security.zh-CN.md) |
| [agent-capability-matrix.md](./agent-capability-matrix.md) | [agent-capability-matrix.zh-CN.md](./agent-capability-matrix.zh-CN.md) |
| [architecture.md](./architecture.md) | [architecture.zh-CN.md](./architecture.zh-CN.md) |
| [protocol.md](./protocol.md) | [protocol.zh-CN.md](./protocol.zh-CN.md) |
| [development.md](./development.md) | [development.zh-CN.md](./development.zh-CN.md) |
| [troubleshooting.md](./troubleshooting.md) | [troubleshooting.zh-CN.md](./troubleshooting.zh-CN.md) |
| [release.md](./release.md) | [release.zh-CN.md](./release.zh-CN.md) |
| [brand-art.md](./brand-art.md) | [brand-art.zh-CN.md](./brand-art.zh-CN.md) |
| [README.md](./README.md) (this index) | [README.zh-CN.md](./README.zh-CN.md) |

## Archive and contracts

- **Live v0.2 product boundaries:** [../README.md](../README.md) “Current boundaries (v0.2)” and today’s operator guides.
- **Target v1.0.0 product contract:** [v1-product.md](./v1-product.md) — supersedes v0.x compromises when building toward the complete release.
- **Frozen construction snapshots:** [archive/](./archive/). **Not** a product contract. Verify any claim there against current code, README, AGENTS.md, and (for v1 work) `v1-product.md`.
