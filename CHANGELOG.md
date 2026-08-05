# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add capability-gated native thread creation for Claude Code, Codex, Kilo,
  Kimi CLI, and Grok Build in directories discovered from native sessions.

### Fixed

- Make thread creation fail closed with a durable operation journal and require
  positive native-store ownership before reporting a new thread as owned.

## [0.2.3] - 2026-08-04

### Changed

- Reduce repeated device-page chrome once a machine has resumable threads,
  while keeping concise version and total-thread status visible.
- Clarify empty search results and Codex thread-starting feedback in the mobile
  client.
- Restyle the five built-in agent catgirl avatars as one cohesive NekoNest
  ensemble while preserving each agent's visual identity.

### Fixed

- Explain approval-gated composing directly above the prompt bar and prevent
  stale thread-start operations from leaving the composer in a busy state.
- Keep indeterminate Codex thread starts fail-closed: do not navigate or
  synthesize owned history, disable duplicate starts, and wait for native
  discovery reconciliation.
- Keep phone-only archive semantics visible to touch and assistive-technology
  users instead of hiding the explanation in hover text.

## [0.2.2] - 2026-08-04

### Added

- Whole-project phone-local archive controls alongside per-thread archive,
  with persisted project preferences and updated device-tree visual coverage.
- VisualViewport-driven mobile shell sizing so the prompt composer remains
  above overlay software keyboards on affected Android browsers and WebViews.

### Changed

- The page-level version panel now compares only the loaded web app and the
  live Server; every machine card reports its own Daemon version and update
  state.
- Dynamic version-bearing HTTP requests and responses bypass browser and
  Service Worker caches, while the live WebSocket acknowledgement remains the
  authoritative Server version after connection.

### Fixed

- Make Codex approval decisions idempotent so a delayed duplicate phone tap
  cannot turn a successful approval into a misleading `approval_unavailable`
  error.
- Keep approval controls disabled until their pending request changes, and
  make the visible attachment button reliably open the picker for local Codex
  draft threads.

## [0.2.1] - 2026-08-03

### Added

- Cross-component application release reporting: PWA, server, and live daemons
  expose independent app versions; the device screen shows alignment, requests
  a frontend refresh when its served release differs, and flags daemon updates.
- `-version` output for server and daemon plus version fields on `/health` and
  `nekonest-daemon -doctor` diagnostics.
- Deterministic Chromium visual-regression coverage for the primary PWA states,
  responsive layouts, themes, locales, and prompt lifecycle.

### Fixed

- Preserve the first prompt bubble when a newly started Codex thread becomes
  owned and native history is synchronized.
- Refresh the reported daemon release when a new live connection replaces an
  older connection for the same device.

## [0.2.0] - 2026-08-01

### Added

- PWA Chinese/English i18n (`vue-i18n`): language switch on Setup and device list;
  locale persisted in `localStorage`
- PWA light/dark/system theme preference with CSS tokens and Naive dark theme
- Thread list local search (summary, folder, agent)
- v1.0.0 target product contract (`docs/v1-product.md` + zh-CN): Codex-first
  full control, sealed E2E default, Windows+Linux hosts, frozen decisions
- Protocol v1 scaffolding: `protocol_version` / `transport_mode` handshake,
  `sealed_payload` shape, expanded statuses (`waiting_user`, `error`),
  session capabilities, control/lifecycle message types (`steer`,
  `start_thread` / `thread_*`, pair/key/attention)
- Server nest transport mode (`NEKONEST_TRANSPORT_MODE`, v0.2 default open;
  sealed opt-in preview) with version/mode negotiation on daemon and phone first frames
- PWA full prompt lifecycle types (`prompt_accepted` / `prompt_committed` /
  `prompt_not_seen`); durable outbox clears on committed
- Daemon register reports host OS; device `os` accepts `windows` | `linux`
- Independent phone identities: `/api/phones/bootstrap`, grants on pair consume,
  revoke, grant-scoped device list/WS subscribe; `NEKONEST_ADMIN_SECRET`
  (legacy `NEKONEST_PHONE_SECRET` alias)
- Sealed crypto package (Go server+daemon, PWA Web Crypto): Ed25519/X25519/HKDF/
  AES-256-GCM primitives and golden vector generation under `protocol/testdata/`
- Daemon long-term E2E identity (`~/.nekonest/identity.json`); pair generate
  prints QR JSON + fingerprint; register/upload device public keys
- PWA pair screen accepts QR JSON paste, shows host fingerprint, rejects
  fingerprint mismatch; phone identity in IndexedDB (`@noble/curves`)
- Daemon advertises per-session `capabilities` (control/attachment modes);
  statuses include `waiting_user` / `error`
- Daemon `-doctor` diagnostics (config, identity, adapters, nest health)
- Linux agent processes run in their own process group; interrupt uses
  SIGINT then SIGKILL on the group
- PWA gates interrupt/approve/deny by advertised capabilities; shows approve
  buttons when `approve`+`deny` are true
- Daemon sealed-keys manager + sealed `session_message` path (AES-GCM under
  per-session content keys when `NEKONEST_TRANSPORT_MODE=sealed`)
- Server persists sealed messages as opaque ciphertext; open-mode push heuristics
  skipped for sealed frames
- Codex `app-server` JSON-RPC client with health/doctor probing, native thread
  resume/start, live turn tracking, and server-request handling
- Offline `nekonest-server -migrate-v1 -data … -backup …` destructive content
  wipe preserving device tokens; docs `migration-v1.md` (+ zh-CN)
- Pair key exchange: phone pubs on consume; daemon `pair_ready` wraps catalog
  key; `/api/keys` + `/api/keys/upload` + grant listing; PWA unwrap store
- PWA sealed send_prompt encrypt + session_message decrypt; key_package ingest
- Daemon sealed send_prompt decrypt; Codex app-server approve/deny/interrupt/
  steer/start_thread with capability raise when healthy
- `attention_event` generic push (no plaintext details)

### Changed

- Device list shows session-count hint as threads (not “N 位猫娘”)
- Session header layout, larger interrupt/send touch targets, improved contrast
- Document titles include device/session context when available
- Reorganized documentation: English primary (`README.md`, `docs/*.md`) with
  full Simplified Chinese mirrors (`README.zh-CN.md`, `docs/*.zh-CN.md`)
- Expanded operator/contributor guides: configuration, security, architecture,
  protocol overview, development, troubleshooting, and a docs index
- `AGENTS.md` v1 invariants: Codex-only `start_thread` exception; sealed default;
  Windows+Linux formal hosts
- Session activity UI keys for waiting_user / error

### Fixed

- Naive UI label `for` targets real inputs (phone key, pair code)
- UUID-like session summaries fall back to untitled-thread label
- WebSocket reconnect copy no longer implies the home PC is offline
- Codex app-server approval state, capability promotion, turn completion, and
  PWA replying indicators stay synchronized across discovery refreshes
- Server, daemon, and PWA now consistently default to `open` transport in
  v0.2; sealed transport remains an explicit preview mode before the v1 cutover

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

[Unreleased]: https://github.com/klarkxy/nekonest/compare/v0.2.3...HEAD
[0.2.3]: https://github.com/klarkxy/nekonest/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/klarkxy/nekonest/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/klarkxy/nekonest/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/klarkxy/nekonest/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/klarkxy/nekonest/releases/tag/v0.1.0
