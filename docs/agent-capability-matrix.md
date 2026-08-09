> English | [简体中文](./agent-capability-matrix.zh-CN.md)

# Agent harness capability matrix

This is the live, normative matrix for the four active NekoNest harnesses.
The phone must gate every control on the daemon's current
`session.capabilities` or `session_list.start_capabilities`; absent fields are
false or unsupported.

Kilo is retired. Protocol 1.x still parses its legacy wire id so a mixed-version
peer can fail closed, but current daemon and PWA catalogs do not discover,
advertise, start, or display Kilo sessions. Native Kilo data is never modified.

## Legend

| Cell | Meaning |
|---|---|
| **Yes** | Implemented and advertised when the path is live |
| **Probe** | Enabled only after the installed native path passes its probe |
| **Fallback** | Available only on the documented degraded path |
| **No** | Not advertised; the phone disables the control and shows a reason |
| **Not live-verified** | Code/fixture exists, but this installed version has not produced the required event |

`queue` always means NekoNest's durable FIFO, never an agent-native queue.
Codex remains the only agent with a guaranteed full-control path. Arbitrary
filesystem browsing, generic `create_session`, and simulated `steer` remain out
of scope.

## Active identities and stores

| | Claude Code | Codex | Kimi CLI | Grok Build |
|---|---|---|---|---|
| Wire id | `claude_code` | `codex` | `kimi_cli` | `grok_build` |
| Role | Compatibility resume | Full control when app-server is healthy | Compatibility resume | Compatibility resume |
| Native store | `~/.claude/projects/<encoded-path>/*.jsonl` | `~/.codex/sessions/…/rollout-*.jsonl` | `~/.kimi-code` (legacy `~/.kimi`) | `~/.grok/sessions` |
| Primary control path | CLI resume; optional SDK bridge | `codex app-server` | CLI resume; ACP is start-only | CLI resume / start |
| Healthy `control_mode` | `compatibility` | `app_server` | `compatibility` | `compatibility` |
| Degraded `control_mode` | `compatibility` | `exec_resume` | `compatibility` | `compatibility` |

Every adapter must prove ownership from its native store before routing a
session. Empty history is not ownership. stderr is diagnostics only, and
subagents/sidechains/synthetic-only records are excluded when detectable.

## Session controls

| Capability | Claude Code | Codex app-server | Codex exec fallback | Kimi CLI | Grok Build |
|---|---|---|---|---|---|
| Discover / ownership / history | Yes | Yes | Yes | Yes | Yes |
| `send` + stream | Yes | Yes | Fallback | Yes | Yes |
| `interrupt` | Yes | Yes | Fallback | Yes, process-tree fallback if native cancel is absent | Yes |
| `approve` / `deny` | Probe, bridge only | Yes | No | No on the current CLI-resume path | No on the current CLI-resume path |
| Structured `user_input` | Probe, bridge only | Yes | No | No | No on the current CLI-resume path |
| `steer` | No | Yes | No | No | No |
| NekoNest durable FIFO | Yes when journal is writable | Yes when journal is writable | No | Yes when journal is writable | Yes when journal is writable |
| Per-session `spawn` | No; device catalog only | Probe | No | No; device catalog only | No; device catalog only |
| `attachment_mode` | `path_best_effort`; bridge may probe native image | `native_image_and_file` | `native_image` | `path_best_effort`; ACP start currently advertises no attachment input | `path_best_effort`; images remain No |

Waiting states are created only by a schema-valid, current-generation native
event. Transcript guesses never create actionable approval or question UI.
Permission and question responses bind to their current native request when
available. Interrupt additionally echoes the advertised daemon generation and
`client_msg_id`; a delayed command for an older turn is rejected.

## Native thread start

`start_capabilities` is a device-level catalog. Every entry includes explicit
`available`, `spawn`, `attachment_mode`, and, when known, `control_path` and
`control_version`.

| Harness | Probe / confirmation | Result |
|---|---|---|
| Claude Code | Starter/bridge handshake; first-turn assistant/result signal plus native-store ownership | Probe |
| Codex | Healthy app-server; turn-started plus native-store ownership | Probe |
| Kimi CLI | ACP initialize and prompt terminal success plus native-store ownership; existing-session turns still use CLI resume | Probe |
| Grok Build | CLI start with positive initial output plus native-store ownership | Probe |

The target directory must be in the daemon's current union of native-discovered
project directories. Lifecycle is `thread_starting → thread_owned |
thread_failed | thread_indeterminate`. `thread_owned` requires both positive
first-prompt confirmation and native-store ownership. Once a native boundary
may have been crossed, an unknown result becomes `thread_indeterminate` and is
never replayed through another control path.

## Durable queue

All reliable installed send paths may advertise the v2 FIFO while its journal
is writable. Items move through `queued → running → completed` or one of
`blocked_failed`, `blocked_interrupted`, and `blocked_indeterminate`.

- The prompt is journaled before crossing the agent boundary.
- Sealed mode reuses the exact original envelope bytes.
- Failure, interruption, or indeterminate completion pauses subsequent items;
  the active prompt is never replayed.
- `resume_prompt_queue` continues only later items. An indeterminate blocker
  requires explicit `skip_prompt_queue_item` confirmation.
- Restart converts every unconfirmed running item to
  `blocked_indeterminate`.
- Completed/cancelled/skipped payloads are cleared, retaining only bounded
  deduplication tombstones.

## Compatibility and unavailable reasons

Protocol 1.2 emits all capability booleans explicitly and includes stable
`unavailable_reasons` for `send`, `approve`, `deny`, `interrupt`, `steer`,
`queue`, `spawn`, `user_input`, and `attachment`. A new PWA infers legacy
`send`/`interrupt=true` only when the catalog is confirmed to originate from a
daemon at protocol 1.1 or earlier. Unknown producer versions fail closed.

A missing CLI is non-fatal: native history may remain browsable for active
agents, while sending and other controls are disabled with a concrete reason.

## Operator checks

Use `nekonest-daemon -doctor` to see the installed CLI, control path/version,
authentication/probe state, attachment tier, and effective flags. Live flags
win over this table. See [troubleshooting.md](./troubleshooting.md) and
[e2e-smoke.md](./e2e-smoke.md) for failure and acceptance paths.
