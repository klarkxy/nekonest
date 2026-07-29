# NekoNest Agent Guide

## Scope

This file applies to the entire repository.

NekoNest is a self-hosted bridge for continuing coding-agent sessions from a
phone:

```text
Phone PWA <-> VPS Server <-> outbound Windows Daemon <-> local agent CLI/store
```

The supported wire identifiers are `claude_code`, `codex`, `kilo`,
`kimi_cli`, and `grok_build`.

Keep these product boundaries intact unless the user explicitly changes them:

- The phone resumes sessions that already exist on the PC; it does not create
  remote sessions.
- Each agent's native local store is authoritative for session discovery and
  transcript history.
- The daemon initiates the connection to the server. Do not require an inbound
  connection to the home PC.
- Sessions are presented as `directory -> agent -> thread`; sessions without a
  directory belong to `未分类`.

## Read Before Editing

1. Read `README.md` for the current product contract and repository layout.
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

`tasks/plan.md`, `tasks/todo.md`, and older review reports are historical
delivery records. Verify their claims against the live code before relying on
them.

## Repository Map

- `protocol/protocol.json`: language-neutral JSON envelope/schema.
- `server/`: Go VPS service; authentication, registration/pairing, WebSocket
  relay, SQLite durability, attachments, and Web Push.
- `daemon/`: Go Windows service; config, reconnecting outbound connection,
  agent discovery/history, prompt journal, and agent process control.
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
- `docs/`: deployment and manual end-to-end acceptance instructions.
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

Do not reintroduce `create_session` or any phone-side thread-creation surface
without an explicit product decision.

### Prompt delivery and history

- `client_msg_id` and the accepted/committed/failed states provide
  at-most-once safety across reconnects. Do not collapse transport success into
  business acknowledgement.
- The daemon prompt journal must fail closed when it cannot safely determine
  whether a prompt was already accepted.
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

### Server security and durability

- Unauthenticated mode is local-development-only and must stay loopback-bound.
- Public use requires phone authentication; device bootstrap registration,
  origin checks, trusted-proxy handling, attachment validation, and body/frame
  size limits must not be weakened casually.
- Never log or commit device tokens, phone secrets, bootstrap tokens, local
  transcripts, uploaded attachments, database contents, or `.env` files.
- Keep SQLite mutations and replay/ack transitions ordered and test failure
  paths, not only happy paths.

### PWA behavior

- Keep WebSocket handlers named and removable; route changes/unmounts must not
  leak handlers or create duplicate message processing.
- Reconnect must re-establish an acknowledged subscription before session
  traffic is treated as ready.
- Keep the prompt outbox idempotent across reconnects.
- Session grouping remains a derived view; do not rewrite source session data
  to build the directory/agent tree.
- Encode route parameters and cover Windows, UNC, non-ASCII, reserved
  characters, and missing-directory cases.
- Render agent/remote content as untrusted. Markdown output must remain
  sanitized with DOMPurify.
- Preserve keyboard focus, reduced-motion behavior, safe-area ownership, and
  approximately 44 px mobile touch targets.

## Editing Rules

- Keep changes scoped to the user's request; preserve unrelated dirty work.
- Prefer the existing abstraction over parallel one-off implementations.
- Add or update a regression test with every behavior fix.
- Run `gofmt` on changed Go files and follow the existing TypeScript/Vue style.
- Do not hand-edit lockfiles unless dependency declarations changed.
- Do not commit build outputs, local databases, generated caches, packaged
  archives, secrets, or CodeGraph database files.
- When behavior, environment variables, supported agents, deployment, or the
  manual acceptance path changes, update `README.md` and the relevant file in
  `docs/`.
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
smoke test.

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

In the final handoff, state which files changed, which verification commands
passed, and any checks that could not be run.
