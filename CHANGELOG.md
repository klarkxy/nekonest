# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Reorganized documentation: English primary (`README.md`, `docs/*.md`) with
  full Simplified Chinese mirrors (`README.zh-CN.md`, `docs/*.zh-CN.md`)
- Expanded operator/contributor guides: configuration, security, architecture,
  protocol overview, development, troubleshooting, and a docs index

## [0.1.0] - 2026-07-30

First public release of NekoNest (猫娘窝): a self-hosted bridge for resuming
existing coding-agent threads on a home Windows PC from a phone PWA.

### Added

- VPS server (Go + SQLite): phone authentication, daemon bootstrap registration,
  pairing codes, WebSocket relay, durable messages, attachments, optional Web Push
- Windows daemon (Go): outbound reconnecting WebSocket, native-store discovery,
  history import, prompt journal, headless CLI execution and process control
- Mobile PWA (Vue 3 + TypeScript + Pinia): installable client, device pairing,
  directory → agent → thread navigation, drafts, sanitized Markdown, reconnect
  outbox, session activity indicators, onboarding polish
- Supported agents: Claude Code, Codex, Kilo, Kimi CLI, Grok Build
- Language-neutral wire schema under `protocol/protocol.json`
- Operator guides: VPS deploy, Windows daemon deploy, end-to-end smoke checklist
- Maintainer brand-asset rebuild notes under `docs/brand-art.md`
- SATA 2.0 license (`LICENSE`, `LICENSE_zh`)
- Maintainer release guide (`docs/release.md`) and archived construction records
  under `docs/archive/`

### Fixed

- Clearer device list load/error states when the nest server is unreachable
- Pair-code input normalization; block sending while the current thread is busy
- Friendlier failure copy when an agent run is still in progress

### Known limitations

- Phone clients resume existing PC threads only; they do not create remote threads
- Tool approval depends on each agent’s non-interactive CLI; blocked work may
  need to be handled on the PC
- Kimi CLI and Grok Build receive attachment local paths in the prompt; read
  access depends on the CLI’s file permissions
- Web Push requires VAPID configuration; without it, no real push is sent
- Daemon targets Windows
- The VPS relays and persists device metadata, messages, and attachments; there
  is no end-to-end encryption between phone and home PC

[0.1.0]: https://github.com/klarkxy/nekonest/releases/tag/v0.1.0
