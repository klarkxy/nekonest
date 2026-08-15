# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.8-rc.2] - 2026-08-15

### Fixed

- Register the Windows daemon's unlimited scheduled-task runtime through the
  Task Scheduler COM API as `PT0S`, avoiding Windows builds that reject the
  `00:00:00` value emitted for a zero .NET `TimeSpan`.

## [0.2.8-rc.1] - 2026-08-15

### Changed

- Fold thinking/reasoning blocks in the thread view so Grok (and other agents)
  no longer dump long chain-of-thought into the phone chat. Tap the header to
  expand; the latest block stays marked as in progress while it streams.

### Added

- Add host daemon `install`, `uninstall`, `start`, `stop`, and `status`
  commands that register a current-user Windows logon task or Linux systemd
  user unit. The OS supervises the process; the CLI does not stay resident.
- Add an explicit Codex Plan-mode composer control so structured
  `requestUserInput` questions can be answered from the PWA without changing
  normal coding turns into planning turns.

### Fixed

- Wake durable prompts after a native busy session becomes idle, bind sealed
  queue/interrupt controls to their target message id, and allow failed or
  interrupted blockers to be removed from the queue UI.
- Keep long Codex native turns busy while their rollout is still active; orphan
  fallback now uses a conservative inactivity window instead of turn age.

## [0.2.7] - 2026-08-15

### Changed

- Align operator docs with current behavior: new nests default to sealed, Windows and
  Linux hosts are first-class, and the three Go modules plus `go.work` are
  described consistently. The repository's Linux systemd template no longer
  forces `NEKONEST_TRANSPORT_MODE=open`; binary upgrades do not rewrite an
  existing unit, so operators must compare template changes explicitly.
- Keep live documentation under `docs/` (plus the root landing files). Move the
  frozen v1.0.0 target contract and unused v0.1→v1.0 migration runbook into
  `docs/archive/`.
- Replace the README and architecture ASCII topology with a labeled catgirl
  illustration (`docs/images/how-it-works.*.jpg`).
- Reorganize live docs by audience, keep only supported operator actions, and
  make runtime capabilities, binary help, the wire schema, and release workflow
  authoritative instead of copying version-sensitive internals into prose.

### Added

- Add a single Chinese Zhihu intro draft under `docs/zhihu-intro.zh-CN.md`.

## [0.2.6] - 2026-08-14

### Added

- Sign daemon registration requests with the long-term Ed25519 identity so a
  managed Cloud can require private-key proof before restoring a revoked host
  ID, while first-time and direct/self-hosted registration remain compatible.

### Changed

- Complete the Chinese terminology transition to “乐园” across product copy,
  metadata, tests, and active documentation.
- Probe the phone key against `GET /api/devices` before entering the nest, and
  require a stored credential on private routes instead of `setup_done`.
- Treat first-use open relay as an explicit Chinese consent, not a transport
  mismatch. Pair help now lists Windows and Linux commands.
- Open a listed thread from the cached REST catalog without waiting for a
  WebSocket `session_list`. REST snapshots no longer wipe start capabilities.
- Show thread summaries, relative activity, and honest empty/offline counts on
  the device tree; hide Grok-style opaque ids as titles.

### Fixed

- Treat `device_already_connected` as a same-endpoint retry so a stale live
  lease cannot permanently stop the daemon read loop.
- Restore Relay Core / standalone Server regression coverage for prompt
  acknowledgement, sealed replay, first-frame limits, attachments, and
  connection-manager generation ordering.

## [0.2.5] - 2026-08-11

### Added

- Add an official non-root Server + PWA container, private `0700`/`0600` data
  handling, hardened Docker Compose deployment, real container smoke test, and
  tag-driven multi-architecture GHCR publication with digest-pinned immutable
  exact tags, rollback-safe stable aliases, SBOM, and provenance.
- Add privacy-safe Server/Daemon operator logging with validated text/JSON
  formats, configurable levels, stable component/event fields, and runtime-owned
  persistence/rotation.
- Add GitHub Actions checks for Server, Daemon, PWA, and all supported release
  packages, plus tag-driven publication of five prebuilt archives and SHA-256
  checksums.
- Document direct installation from GitHub Release archives in both READMEs;
  Server packages include the matching PWA build.

### Changed

- Rename Chinese setup, device, and product copy to “猫娘乐园”.

### Fixed

- Normalize both slash styles before attachment filename sanitization so Linux
  and Windows Servers derive the same safe basename from uploaded paths.
- Refresh a phone's cached session catalog from an online daemon's native stores,
  and avoid rendering empty agent groups by placing enabled missing agents in
  the project's New menu.
- Make immutable GHCR tag inspection fail closed, prevent stable aliases from
  rolling backward, and keep exact tagged-source builds reproducible.

## [0.2.4] - 2026-08-09

### Added

- Document a per-harness live capability matrix for Claude Code, Codex, Kimi CLI, and Grok Build.
- Add capability-gated native thread creation for Claude Code, Codex, Kimi CLI,
  and Grok Build in directories discovered from native sessions.
- Complete the Codex 0.146.0 full-control path with structured
  `requestUserInput` forms, password-safe secret answers, expiry and
  idempotent/indeterminate response handling.
- Add a durable per-session NekoNest FIFO queue for every reliable installed
  agent path, with generation-bound native outcomes, explicit blockers,
  resume/skip controls, restart-safe v1-to-v2 migration, payload-free
  tombstones, and exact sealed-envelope reuse. This is not an agent-native queue.
- Add atomic Codex first turns with image and ordinary-file attachments plus
  fail-closed ownership/acceptance lifecycle results.
- Supervise `codex app-server` with generation guards, immediate capability
  downgrade and attention events, bounded recovery, and no replay of uncertain
  active work.
- Persist the nest transport mode, default new databases to sealed, classify
  legacy databases/configs as open, expose the live mode from `/health`, and
  make the PWA resolve it before WebSocket connection.
- Upgrade the wire protocol to backward-compatible 1.2 with explicit
  capabilities, stable unavailable-reason codes, producer-version-preserving
  snapshots, queue control, structured user input, and sealed-safe events.

### Changed

- Retire Kilo from active daemon and PWA catalogs while preserving protocol
  1.x parse compatibility for its old wire id. Existing native Kilo data is
  left untouched.

### Fixed

- Limit all four active native thread catalogs to a shared 7-day activity window while
  preserving ownership and native data, keep actionable running/waiting threads
  visible, and give old deep links an explicit hidden/removed state.
- Cache compact file discovery metadata by path/size/mtime and schedule daemon
  discovery 30 seconds after each completed scan, avoiding repeated transcript
  parsing and ticker catch-up under large native histories.
- Make thread creation fail closed with a durable operation journal and require
  positive native-store ownership before reporting a new thread as owned.
- Keep sealed prompt commands opaque on the Server, persist and replay their
  exact envelope, and clear the phone outbox only after durable native commit.
- Make transport-mode mismatches fail closed across Server startup, Daemon
  registration/config, and PWA runtime/build assertions.

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
- v1.0.0 target product contract (`docs/archive/v1-product.md` + zh-CN): Codex-first
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

First public release of NekoNest (猫娘乐园): a self-hosted bridge for resuming
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

[Unreleased]: https://github.com/klarkxy/nekonest/compare/v0.2.8-rc.2...HEAD
[0.2.8-rc.2]: https://github.com/klarkxy/nekonest/compare/v0.2.8-rc.1...v0.2.8-rc.2
[0.2.8-rc.1]: https://github.com/klarkxy/nekonest/compare/v0.2.7...v0.2.8-rc.1
[0.2.7]: https://github.com/klarkxy/nekonest/compare/v0.2.6...v0.2.7
[0.2.6]: https://github.com/klarkxy/nekonest/compare/v0.2.5...v0.2.6
[0.2.5]: https://github.com/klarkxy/nekonest/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/klarkxy/nekonest/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/klarkxy/nekonest/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/klarkxy/nekonest/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/klarkxy/nekonest/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/klarkxy/nekonest/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/klarkxy/nekonest/releases/tag/v0.1.0
