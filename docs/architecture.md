> English | [简体中文](./architecture.zh-CN.md)

# Architecture

How NekoNest’s three runtimes cooperate to resume coding-agent threads from a phone. Engineering invariants for contributors: [AGENTS.md](../AGENTS.md).

## Big picture

```text
┌─────────────┐       HTTPS / WSS       ┌──────────────────┐
│  Phone PWA  │ ◄─────────────────────► │  VPS Server      │
│ Vue3+Pinia  │                         │  Go + SQLite     │
└─────────────┘                         └────────┬─────────┘
                                                 │ WSS
                                                 │ outbound from PC
                                        ┌────────▼─────────┐
                                        │ Windows Daemon   │
                                        │ discover/history │
                                        │ journal / exec   │
                                        └────────┬─────────┘
                                                 │ native store + CLI
                    ┌────────────┬───────────┬───┴────────┬────────────┐
                    │Claude Code │   Codex   │ Kimi CLI   │ Grok Build │
                    └────────────┴───────────┴────────────┴────────────┘
```

| Layer | Module | Responsibility |
|---|---|---|
| PWA | `pwa/` | Auth UI, pairing, session tree, drafts, outbox, Markdown render, optional push |
| Server | `server/` | Phone/daemon auth, pairing, WS relay, SQLite durability, attachments, Web Push |
| Daemon | `daemon/` | Outbound WS, adapter discovery/history, prompt journal, headless CLI control |
| Protocol | `protocol/protocol.json` | Language-neutral envelope/schema (manually mirrored in Go/TS) |

There is **no inbound** connection to the home PC. The daemon dials the VPS.

## Session presentation model

Sessions are a **derived view**, not a rewritten store:

```text
directory (project path)  →  agent type  →  thread (native session id)
```

- Threads without a recognizable working directory appear under **未分类** (Uncategorized).
- Empty agent groups under a directory are omitted.
- Active wire agent IDs: `claude_code`, `codex`, `kimi_cli`, `grok_build`.
- Protocol 1.x parses retired `kilo` values only for fail-closed mixed-version compatibility; active catalogs filter them.

Per-harness **live** capability matrix (control flags, attachments, start probes, live vs v1):
[agent-capability-matrix.md](./agent-capability-matrix.md).

## Discovery and ownership

1. Daemon **Discovers** sessions from each registered adapter at startup/force events and 30 seconds after each completed periodic scan; slow scans never accumulate ticker debt.
2. An adapter claims a session only with **positive ownership** against its native store (empty transcript ≠ ownership).
3. Subagents, sidechains, primers, and synthetic/system-only records are **excluded** from the phone-visible main thread list.
4. A missing CLI is a **non-fatal** unavailable adapter; other agents keep working.
5. The phone-visible catalog contains threads active in the last 7 days, plus any positively running/waiting thread. Old native records are not deleted; projects backed only by old threads leave the current directory union until host-side activity makes a thread recent again.
6. Discovered lists are pushed toward the server/phone as session list/update traffic. File-backed adapters reuse compact path/size/mtime metadata and reparse only changed files; transcript bodies are never cached by discovery.

Native stores remain authoritative; the VPS cache is for relay and durability of nest-side messages, not a replacement agent DB.

## Prompt delivery path

```text
PWA composer
  → client_msg_id + optional attachments
  → phone WS send_prompt
  → server validates, may persist, forwards to daemon
  → daemon prompt journal (dispatching → accepted → committed | failed paths)
  → agentexec headless CLI (resume/session flags + attachment wiring)
  → streamed session_message / status events back to phone
```

### Delivery states (conceptual)

| State / signal | Meaning |
|---|---|
| Transport OK | Frame reached the peer; **not** proof the agent accepted work |
| `prompt_accepted` | Daemon accepted the prompt into its execution pipeline |
| `prompt_committed` | Work reached a durable committed point in the journal sense |
| `prompt_failed` / errors | Visible failure; safe to surface to the user |
| `prompt_sent` (PWA) | Outbox entry may clear only on the appropriate ack (see PWA outbox rules) |

### At-most-once and reconnect

- Stable **`client_msg_id`** ties phone outbox entries to server/daemon handling.
- Reconnect **reuses** the same id rather than silently minting a new one for the same user action.
- If the journal cannot determine prior acceptance, the daemon **fail-closes** (prefer error over double run).
- PWA outbox is capped (40); full queue blocks new sends until acks drain.

## History and streaming merge

When a thread opens:

1. Client may `fetch_history` / receive `session_history`.
2. Daemon pulls from the **native** adapter history (default window ~40 messages, content often truncated ~4k runes).
3. Server-persisted nest messages and live `session_message` streams merge in the PWA with **stable message ids**.
4. Merge logic drops only the appropriate pending outbox / optimistic locals so replay does not duplicate turns.
5. CLI **stderr is diagnostics**, not assistant bubble text.

Empty sessions can re-sync history when the user re-enters.

## Subscriptions

Phone clients must establish an acknowledged **subscribe** for the active device/session context before treating session traffic as ready:

```text
subscribe  →  subscribe_ack  →  session traffic / prompts
```

Route changes and component unmounts must remove WS handlers to avoid duplicate processing (PWA invariant).

## Attachments path

```text
Phone multipart upload → server data/attachments
  → prompt references attachment ids/urls
  → daemon downloads to per-run temp dir
  → agent-specific wiring (native --file / image flags / path-in-prompt)
```

Limits and MIME allowlist: [configuration.md](./configuration.md).

## Approvals and control

Wire types include `approve`, `deny`, and `interrupt`. Real behavior depends on whether the agent’s **non-interactive** CLI can host the approval:

- If the CLI cannot carry the approval UX, the phone must not fake success—user returns to the PC terminal.
- Interrupt/stop uses process control (on Windows, Job Object + kill-on-job-close) to tear down child trees.

## Concurrency and connection lifecycle (high level)

| Concern | Approach |
|---|---|
| WebSocket writers | Serialize writes; one reader + one writer per connection |
| Registry locks | Do not hold locks across slow adapter, network, or filesystem calls |
| Daemon reconnect | Generation checks so stale connections cannot overwrite newer state |
| Config reload | Immutable config snapshots; credentials fixed until process restart |
| Instance lock | One daemon process per config path |

## Repository map (code)

```text
protocol/     JSON schema (manual)
relaycore/    reusable single-nest data-plane Engine and public ports
server/       VPS service module (github.com/nekonest/server)
daemon/       Windows daemon module (github.com/nekonest/daemon)
  internal/adapters/   per-agent native store + normalize
  internal/agentexec/  CLI invocation / process
pwa/          Vue 3 + TS + Pinia
docs/         operator and maintainer docs
```

Go module paths are `github.com/klarkxy/nekonest/relaycore`, `github.com/nekonest/server`, and `github.com/nekonest/daemon`. The two legacy application module paths remain stable; do not “fix” them solely because the GitHub repo is `klarkxy/nekonest`.

## Protocol 1.2+ control plane

- The daemon is the capability producer. The Server preserves that producer protocol version in live forwarding and rebuilt open-mode snapshots instead of restamping the catalog as its own release.
- A turn is bound to one control generation, public session id, `client_msg_id`, and native request id. Accepted/success/failure/interrupted/indeterminate events from an older generation are rejected, and interrupt must echo the current advertised generation plus `client_msg_id` under the same per-session dispatch lock.
- Every reliable installed send path may enter the NekoNest queue v2 before crossing the native boundary. Unknown running work after restart becomes `blocked_indeterminate`; it is never replayed.
- Native thread start stays `thread_starting` until both first-prompt success and native-store ownership are positive. The PWA has no local timeout that invents a terminal result.

## Related docs

- [Agent capability matrix](./agent-capability-matrix.md)
- [Protocol overview](./protocol.md)
- [Security model](./security.md)
- [Configuration](./configuration.md)
- [Development](./development.md)
- [AGENTS.md](../AGENTS.md)
