# NekoNest Agent Guide

## Scope

This file applies to the entire repository.

NekoNest is a self-hosted bridge for continuing coding-agent sessions from a
phone:

```text
Phone PWA <-> VPS Server <-> outbound Host Daemon (Windows/Linux) <-> local agent CLI/store
```

The active supported wire identifiers are `claude_code`, `codex`, `kimi_cli`,
and `grok_build`. Protocol 1.x may still parse the retired legacy `kilo` id for
backward compatibility, but current daemon/PWA catalogs must not advertise it.

**Shipped v0.1** behavior is described by `README.md` / operator docs under
`docs/` (except the v1 contract). **Target v1.0.0** product meaning is defined
by `docs/v1-product.md` and `docs/v1-product.zh-CN.md`. When building toward
v1, prefer the v1 contract over v0.1 launch compromises.

Keep these product boundaries intact unless the user explicitly changes them
(and updates the v1 contract in both languages when applicable):

- The phone primarily resumes sessions that already exist on the host from
  each agent's **native** local store.
- Each agent's native local store is authoritative for session discovery and
  transcript history (and for ownership after a successful agent-scoped thread
  start).
- The daemon initiates the connection to the server. Do not require an inbound
  connection to the home host.
- Sessions are presented as `directory -> agent -> thread`; sessions without a
  directory belong to `未分类`.
- **Thread creation:** do **not** reintroduce generic `create_session`,
  nest-only / ghost threads, or arbitrary filesystem browsing. Phone-side
  creation is an **agent-scoped** `start_thread` path for any supported agent
  whose installed/probed native starter advertises `spawn=true`. The phone first
  opens a local-only draft; the daemon creates the native thread only when its
  first prompt is sent. A starter may target only a directory in the daemon's
  **current union of discovered native project directories**, never an arbitrary
  path. Lifecycle must be `thread_starting → thread_owned | thread_failed |
  thread_indeterminate` with fail-closed journaling; never persist a permanent
  nest-only row. `thread_owned` requires both positive initial-prompt
  acknowledgement and ownership in that agent's native store; otherwise report
  `thread_indeterminate`. Navigate the phone to
  the session only after `thread_owned`.
- **Agent roles (v1):** Codex is the only full-control agent (send, approve/deny,
  interrupt, steer, full image+file attachments). Claude Code, Kimi CLI, and
  Grok Build are compatibility-resume adapters (discover, ownership, history,
  send/stream, interrupt, attachments per advertised capability). All four may
  advertise agent-scoped `spawn` only after their native starter is
  installed and probed; this does **not** imply approval, steer, queue, or any
  other full-control capability.
- **Transport (v1):** sealed E2E is the default for new nests; open relay is
  admin-only, one mode per nest, no automatic sealed→open downgrade.
- **Formal host OS (v1):** Windows and Linux. macOS is later.

## Read Before Editing

1. Read `README.md` (English primary) or `README.zh-CN.md` for the **shipped
   v0.1** product contract and repository layout. For v1 target scope read
   `docs/v1-product.md` / `docs/v1-product.zh-CN.md`. Doc index: `docs/README.md`.
2. Check `git status --short --branch` and preserve unrelated user changes.
3. Check `codegraph status`. When the index is current, use `codegraph query`,
   `codegraph explore`, `codegraph node`, and `codegraph impact` to trace
   behavior before editing. Run `codegraph sync` after source changes.
4. Read the relevant tests together with the implementation.
5. For cross-layer work, trace the complete path from PWA to server to daemon
   and the selected adapter instead of fixing only the first visible symptom.

Do not treat `_archive/`, `go-sdk/`, `gocache/`, `.pnpm-store/`, `bin/`,
`data/`, built PWA output, archives, databases, coverage files, or local agent
stores as application source. Do not edit `.codegraph/codegraph.db` directly.

Historical construction and multi-agent delivery snapshots live under
`docs/archive/`. They are frozen records, not the current product contract.
Verify any claim there against live code, `README.md`, this guide, and (for v1
work) `docs/v1-product.md`.

Operator and contributor docs live under `docs/`: English short paths
(`docs/foo.md`) with Simplified Chinese mirrors (`docs/foo.zh-CN.md`). Start
from `docs/README.md`. This file (`AGENTS.md`) and `CHANGELOG.md` remain
English-only.

Release hygiene for maintainers: `CHANGELOG.md` and `docs/release.md`.

## Repository Map

- `protocol/protocol.json`: language-neutral JSON envelope/schema.
- `server/`: Go VPS service; authentication, registration/pairing, WebSocket
  relay, SQLite durability, attachments, and Web Push.
- `daemon/`: Go host service (Windows/Linux target for v1); config, reconnecting
  outbound connection, agent discovery/history, prompt journal, and agent
  process control.
- `daemon/internal/adapters/`: native-store discovery, ownership, history, and
  normalized output for each supported agent.
- `daemon/internal/agentexec/`: headless CLI invocation and process handling.
- `pwa/`: Vue 3 + TypeScript + Pinia mobile client.
- `pwa/src/types/protocol.ts`: browser-side wire types.
- `pwa/src/config/agents.ts`: central presentation catalog for agent names and
  assets.
- `pwa/src/stores/`: durable UI state, subscriptions, prompt outbox, and
  session state.
- `pwa/src/utils/`: pure grouping, sorting, merging, attachment, and Markdown
  helpers.
- `docs/`: operator and contributor guides (deploy, configuration, security,
  architecture, protocol, development, troubleshooting, e2e, release); Chinese
  mirrors as `*.zh-CN.md`; frozen history under `docs/archive/`; v1 target
  contract in `docs/v1-product.md` (+ `.zh-CN.md`).
- `README.md` / `README.zh-CN.md`: shipped v0.1 product contract and quick start
  until the v1.0.0 release rewrite.
- `CHANGELOG.md`, `LICENSE`, `LICENSE_zh`: user-visible history and SATA 2.0.
- `tools/build_brand_assets.py`: reproducible derivation of PWA brand assets.

There are two independent Go modules (`server/go.mod` and `daemon/go.mod`) and
one pnpm project (`pwa/package.json`); there is no root Go module.

## Cross-Layer Invariants

### Wire protocol

Protocol types are manually maintained, not generated. A wire-contract change
must be checked across all applicable surfaces:

- `protocol/protocol.json`
- `server/internal/protocol/types.go`
- `pwa/src/types/protocol.ts`
- daemon message dispatch/payload construction
- server handlers, persistence, and integration tests
- PWA stores/API code and tests

Keep JSON field names, message strings, enum values, optionality, timestamps,
and payload meanings identical. Adding an agent also requires the daemon
registry/adapter, server types, PWA type/catalog/assets, schema, tests, and
documentation to agree.

Do not reintroduce generic `create_session` or nest-only phone-created
sessions. The sole product exception is agent-scoped `start_thread`, which
begins as a phone-local draft and creates a native thread only with its first
prompt. It is available only when that agent's native starter has been
installed/probed and advertises `spawn=true`, and only for a directory in the
daemon's current union of native-discovered project directories, as specified
in `docs/v1-product.md`. Capability flags are authoritative and default
false/unsupported when absent. Protocol versioning is `major.minor`: reject
major mismatch; minor is backward compatible for unknown optional fields.

### Prompt delivery and history

- `client_msg_id` and the accepted/committed/failed states provide
  at-most-once safety across reconnects. Do not collapse transport success into
  business acknowledgement. Expose not_seen/indeterminate when acceptance is
  unknown.
- The daemon prompt journal must fail closed when it cannot safely determine
  whether a prompt was already accepted.
- In sealed mode, persist and resend the exact same sealed command envelope for
  one `client_msg_id`; never re-encrypt a retry and treat it as the same
  command.
- Preserve stable message IDs and deterministic merge behavior so replayed
  history and streamed output do not duplicate turns.
- Server persistence failures must remain visible; do not acknowledge durable
  work before the required database write succeeds.

### WebSocket and concurrency

- `gorilla/websocket` permits one concurrent reader and one concurrent writer.
  Route writes through the existing serialization/locking helpers.
- Preserve read deadlines, pong handling, bounded message sizes, reconnect
  generation checks, and cleanup on disconnect.
- Do not hold registry/session locks while invoking slow adapter, network, or
  filesystem operations.
- Config endpoint changes, callbacks, and connection publication must remain
  linearized; old connection generations must not overwrite new state.

### Agent adapters

Live per-harness capability matrix (control flags, attachments, start probes):
`docs/agent-capability-matrix.md` (+ `.zh-CN.md`). Prefer advertised
`session.capabilities` / `start_capabilities` over README short tables when debugging.

- Route a session only after positive ownership against that adapter's native
  store. An empty transcript is not proof of ownership.
- Exclude subagents, sidechains, primers, and synthetic/system-only records
  from the phone-visible main-thread list/history.
- Treat stderr as diagnostics, not assistant text.
- Use public/wire session IDs consistently at adapter boundaries; preserve
  native IDs only where a CLI/store requires them.
- Watchers and child processes must stop on adapter close or daemon shutdown.
- A missing agent CLI is a non-fatal unavailable adapter and must not disable
  discovery for other agents.
- Add fixture-based regression tests for each native store shape or streaming
  format that changes.
- Status transitions to `waiting_user` or `waiting_approval` require a positive
  agent/app-server signal. Unsupported adapters remain running/idle/error
  instead of guessing.
- Advertise per-session capabilities honestly. Non-Codex adapters must not
  imply approval, steer, queue, or stronger attachment tiers than implemented;
  `spawn/start_thread` is true only after their native starter is installed,
  probed, and available for the selected discovered directory.

### Server security and durability

- Unauthenticated mode is local-development-only and must stay loopback-bound.
- Public use requires phone authentication; device bootstrap registration,
  origin checks, trusted-proxy handling, attachment validation, and body/frame
  size limits must not be weakened casually.
- v1 phone auth uses independent revocable phone tokens and device grants;
  admin bootstrap is separate from general phone access.
- Never log or commit device tokens, phone secrets/tokens, bootstrap tokens,
  local transcripts, uploaded attachments, database contents, private keys, or
  `.env` files.
- Keep SQLite mutations and replay/ack transitions ordered and test failure
  paths, not only happy paths.
- In sealed mode the server must not need application plaintext; do not log or
  persist prompt/response/tool bodies, paths, titles, approval details, or
  attachment plaintext. Documented routing metadata may remain visible.

### PWA behavior

- Keep WebSocket handlers named and removable; route changes/unmounts must not
  leak handlers or create duplicate message processing.
- Reconnect must re-establish an acknowledged subscription before session
  traffic is treated as ready.
- Keep the prompt outbox idempotent across reconnects; clear durable outbox
  state on committed, not on bare transport success.
- Session grouping remains a derived view; do not rewrite source session data
  to build the directory/agent tree.
- Encode route parameters and cover Windows, UNC, non-ASCII, reserved
  characters, and missing-directory cases.
- Render agent/remote content as untrusted. Markdown output must remain
  sanitized with DOMPurify.
- Preserve keyboard focus, reduced-motion behavior, safe-area ownership, and
  approximately 44 px mobile touch targets.
- Gate interrupt, approve/deny, steer, queue, spawn/start, and attachment
  controls by advertised capability; show a concrete unavailable reason instead
  of no-op controls.

## Editing Rules

- Keep changes scoped to the user's request; preserve unrelated dirty work.
- Prefer the existing abstraction over parallel one-off implementations.
- Add or update a regression test with every behavior fix.
- Run `gofmt` on changed Go files and follow the existing TypeScript/Vue style.
- Do not hand-edit lockfiles unless dependency declarations changed.
- Do not commit build outputs, local databases, generated caches, packaged
  archives, secrets, or CodeGraph database files.
- When behavior, environment variables, supported agents, deployment, or the
  manual acceptance path changes, update `README.md`, `README.zh-CN.md`, and the
  matching English and `*.zh-CN.md` files under `docs/`. For v1 product-scope
  changes, update `docs/v1-product.md` and `docs/v1-product.zh-CN.md` first and
  keep EN/ZH parity.
- Do not create commits, push branches, or rewrite history unless the user asks.

## Verification

Run the smallest focused test first, then the full suite for every affected
module.

### Server

From `server/`:

```powershell
go test -count=1 ./...
go vet ./...
```

### Daemon

From `daemon/`:

```powershell
go test -count=1 ./...
go vet ./...
```

For Windows process, locking, config, or adapter-discovery changes, also run the
focused Windows tests and perform a read-only smoke check against an installed
agent store when available. Never mutate a user's native agent store during a
smoke test. Linux process-group, XDG path, and systemd changes need Linux CI or
baseline smoke when available.

### PWA

From `pwa/`:

```powershell
pnpm test
pnpm type-check
pnpm build
```

Use the committed `pnpm-lock.yaml`. Only install dependencies when they are
missing or dependency declarations changed.

### Cross-layer and final checks

For protocol, delivery, subscription, session-discovery, or agent-catalog
changes, run all three module suites. Then, from the repository root:

```powershell
git diff --check
codegraph sync
codegraph status
git status --short
```

Use `docs/e2e-smoke.md` for deployment-sensitive changes. The root `Makefile`
is a Unix-oriented convenience; on Windows, prefer the explicit module commands
above.

For changes whose acceptance depends on the real VPS, PWA service-worker/cache,
host daemon, native agent stores, reconnect behavior, or runtime resource use,
local suites are preflight rather than final acceptance. Unless the user
explicitly scopes the work to local-only / no-deploy, finish the maintained live
nest path: merge and push the approved commit, build from that exact commit,
preserve rollback copies, update Server/PWA and the current host daemon, then run
the applicable live checks in `docs/e2e-smoke.md`. Do not infer permission to tag
or publish a GitHub Release from this deployment default.

In the final handoff, state which files changed, which verification commands
passed, and any checks that could not be run.
