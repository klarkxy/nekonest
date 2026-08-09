> English | [简体中文](./protocol.zh-CN.md)

# Protocol overview

Language-neutral wire contract for NekoNest phone ↔ server ↔ daemon communication. The normative schema file is [`protocol/protocol.json`](../protocol/protocol.json). Types are **manually maintained** in parallel:

- `protocol/protocol.json`
- `server/internal/protocol/types.go`
- `pwa/src/types/protocol.ts`
- Daemon dispatch / payload construction

Keep JSON field names, enums, optionality, timestamps, and meanings identical across surfaces.

## Version and transport mode

| Field | Rule |
|---|---|
| `protocol_version` | `major.minor` (current **1.1**). Major mismatch rejects; minor is backward compatible (unknown optional fields ignored and absent capability flags are false). |
| `transport_mode` | Nest-wide `sealed` \| `open`. One persisted mode per nest; **no** sealed→open automatic downgrade. New databases default sealed; legacy databases without metadata are classified once as open. |

First frames (`register_device` for daemon, `subscribe` for phone) **must** include both fields. Every later frame is validated against the negotiated major version, transport mode, and its explicit routing-vs-application body policy. In sealed mode application frames require `sealed_payload`; open mode rejects it; mixed bodies are always rejected. Server returns negotiated version/mode on `auth_response` / `subscribe_ack`. Stable error codes: `version_mismatch`, `transport_mode_mismatch`, `invalid_envelope`.

### Application release versions

Application release identity is separate from `protocol_version`: a release
mismatch is diagnostic and does not by itself reject an otherwise compatible
connection.

| Field | Direction / meaning |
|---|---|
| `daemon_version` | Daemon sends its release in `register_device.payload`; server returns the live value in `auth_response`, `subscribe_ack`, `device_online`, and online `Device` snapshots. Absent means offline or an older unreporting daemon. |
| `pwa_version` | PWA sends its build release in `subscribe.payload`; server echoes the accepted value in `subscribe_ack`. |
| `server_version` | Server application release in `auth_response`, `subscribe_ack`, `/health`, and `/api/devices`. It is **not** the wire protocol version. |
| `refresh_required` | `subscribe_ack` boolean; true when a reported PWA release differs from the server release. The PWA offers a user-triggered service-worker update/reload and never auto-loops on this signal. |
| `update_required` | `auth_response` boolean; true when a reported daemon release differs from the server release. |

Current builds use SemVer release strings. `pwa_version` and `daemon_version`
remain optional for compatibility with older clients; missing values are shown
as unknown rather than assumed current.

The PWA treats the live `subscribe_ack.server_version` as authoritative after
the WebSocket connects. Dynamic version-bearing HTTP responses (`/health` and
`/api/devices`) use `Cache-Control: no-store`, and the service worker does not
intercept them. The page-level panel compares PWA and Server only; daemon
versions and update notices belong to their individual device cards.

## Envelope

Every WebSocket application message is a `NekoMessage`:

| Field | Required | Description |
|---|---|---|
| `protocol_version` | first-frame yes | `major.minor` |
| `transport_mode` | first-frame yes | `sealed` \| `open` |
| `type` | yes | One of `MessageType` enum strings |
| `device_id` | yes | Device identifier (daemon identity or routing context) |
| `timestamp` | yes | Unix timestamp in **seconds** |
| `session_id` | no | Agent session / thread id when message is session-scoped |
| `client_msg_id` | no | Idempotency id for prompt/start (relay-visible) |
| `payload` | no | Open-mode or plaintext control body; **must be absent** when `sealed_payload` is set |
| `sealed_payload` | no | Ciphertext envelope; **must be absent** in open mode |

`payload` and `sealed_payload` are mutually exclusive. `additionalProperties` is false on the envelope in the schema.

## Agent type identifiers

| Wire id | Product name |
|---|---|
| `claude_code` | Claude Code |
| `codex` | Codex |
| `kilo` | Kilo |
| `kimi_cli` | Kimi CLI |
| `grok_build` | Grok Build |

Adding an agent requires adapter + registry, server types, PWA catalog/assets, schema, tests, and docs to agree.

## Core shared objects (schema)

### Device

| Field | Notes |
|---|---|
| `id`, `name` | Identity and display |
| `os` | Formal v1: `windows` \| `linux` |
| `status` | `online` \| `offline` |
| `last_seen` | Unix seconds |
| `active_agents` | Session-count hint (not distinct agent types); PWA labels it as threads |
| `daemon_version` | Release reported by the current live daemon; omitted when offline/unreported |

### AgentSession

| Field | Notes |
|---|---|
| `id` | Public/wire session id |
| `device_id` | Owning device |
| `agent_type` | Wire agent id |
| `status` | `idle` \| `running` \| `waiting_user` \| `waiting_approval` \| `error` |
| `summary`, `last_activity` | List UX |
| `project_dir` / `project` | Directory grouping |
| `capabilities` | Optional; absent fields default false/unsupported |
| `pending_approval` | Optional tool approval blob |
| `pending_user_input` | Optional structured Codex question request; distinct from approval |

### SessionCapabilities

| Field | Notes |
|---|---|
| `control_mode` | `app_server` \| `exec_resume` \| `compatibility` |
| `approve`, `deny`, `interrupt`, `steer`, `queue`, `spawn` | bool; default false. `spawn` may be true only for an installed/probed native starter and a permitted discovered directory; it does not imply any other control capability. |
| `attachment_mode` | `native_image_and_file` \| `native_image` \| `path_best_effort` \| `unsupported` |

Live values per harness (what the daemon actually stamps today): [agent-capability-matrix.md](./agent-capability-matrix.md).

### `session_list` start capability catalog

`session_list.payload.start_capabilities` is an optional device-level catalog.
Its absence disables device-level creation; during the minor-version transition,
an older daemon may still expose Codex creation only through an explicit
per-session `capabilities.spawn=true`. Each
entry has `agent_type`, `available`, `spawn`, an optional display `reason`, and
optional `control_path` / `control_version`. The PWA may offer a local draft
only for `available=true` and `spawn=true`; it must use the daemon's current
union of native-discovered project directories, never an arbitrary path.

### Native thread-start payloads

`start_thread.payload` uses `agent_type`, `operation_id`, `project_dir`, and
`prompt`; `cwd` and `initial_prompt` remain optional legacy aliases. The
prompt is the native thread's first prompt, not a prior phone-created session.
In sealed mode this entire body is encrypted with the device-catalog key; the
relay sees only routing metadata and the stable operation id. Before sending,
the PWA durably binds the local draft to that operation id; a reload must never
mint a replacement operation or retry an unresolved start.
`thread_*` payloads retain `operation_id`, `session_id`, `thread_id`, `error`,
and `message` for compatibility. In sealed mode those result payloads are also
device-catalog encrypted; the outer state, operation id, and native session id
remain routing metadata. Visible routing metadata is not an authenticated
business result: a missing or invalid sealed result must resolve locally as
`thread_indeterminate`, never as outer `thread_owned` or `thread_failed`.
`thread_owned` is valid only after both native-store ownership and positive
first-prompt acknowledgement are established, so its `prompt_accepted` field
must be `true`. If either fact is missing, use `thread_indeterminate` and retain
the first prompt in the local draft instead of navigating or synthesizing it.
Attachments belong to that same first native turn: the daemon downloads and
validates them before `thread/start`, then sends prompt + attachments in the
first `turn/start`. A pre-create download failure is `thread_failed`; an
unknown result after native creation is `thread_indeterminate`.

### Structured user input

`pending_user_input` preserves the app-server `request_id`, `item_id`, expiry,
and every question's `id`, header, prompt, options, `isOther`, and `isSecret`.
The phone replies with `respond_user_input` and the exact 0.146 answer map:
`question_id -> { "answers": ["..."] }`. `user_input_result` reports
`accepted`, `expired`, `stale`, or `indeterminate`. Request ids are idempotent;
an uncertain app-server handoff is never automatically retried. Secret values
are never written into drafts/history/logs/server persistence or attention
events.

### SessionMessage

| Field | Notes |
|---|---|
| `id` | Stable id for merge/dedupe |
| `role` | `assistant` \| `user` \| `tool` \| `system` |
| `content` | Text body |
| `type` | `thinking` \| `text` \| `assistant` \| `tool_call` \| `tool_result` \| `error` \| `system` |
| `timestamp` | Unix seconds |
| `metadata` | Optional object |

### AttachmentRef

| Field | Notes |
|---|---|
| `url` | Required reference |
| `id`, `name`, `mime`, `size`, `key` | Optional metadata |

## Message type catalog

Grouped by role. Payload shapes for critical flows are implemented in Go/TS—when integrating, read those types alongside this list. Schema enum is authoritative for **type string** names.

### Device lifecycle

| Type | Direction (typical) | Role |
|---|---|---|
| `device_online` | daemon → server → phone | Device present |
| `device_offline` | server → phone | Device gone |
| `device_list` | server → phone | Snapshot of devices |
| `register_device` | control plane | Registration-related |
| `auth_response` | server → peer | Auth result |

### Sessions

| Type | Role |
|---|---|
| `session_list` | Full or bulk session snapshot |
| `session_update` | Incremental session metadata |
| `session_message` | Streamed or stored turn content |

### Prompt lifecycle

| Type | Role |
|---|---|
| `send_prompt` | Phone → … → daemon: user prompt (+ attachments) |
| `prompt_status_query` | Ask current delivery state |
| `prompt_not_seen` | Daemon/server has no record of id |
| `prompt_queued` | Durably admitted to the Codex FIFO, but not yet accepted by native `turn/start` |
| `prompt_accepted` | Positively accepted by the native agent control path (outbox **not** cleared) |
| `prompt_committed` | Journal committed — **clear durable outbox here** |
| `prompt_failed` | Failed visibly |
| `prompt_sent` | **Deprecated** transitional alias; clients should clear on `prompt_committed` |

**Do not** collapse “WebSocket write succeeded” into `prompt_accepted` / business success.
`prompt_queued` reports the FIFO position without starting acceptance polling.
The durable outbox still clears only after the item receives native
`turn/start` acceptance and `prompt_committed`. Receivers retain compatibility
with the older `prompt_accepted{queued:true}` admission shape.

Reconnect/status recovery reuses the exact same sealed envelope for one
`client_msg_id`. After a definitive retryable `prompt_failed`, an explicit
user retry creates a **new** `client_msg_id` and a freshly sealed command; an
indeterminate result is never exposed as an ordinary retry.

### Control and lifecycle (v1)

| Type | Role |
|---|---|
| `approve` / `deny` | Tool approval (Codex app-server when capable) |
| `interrupt` | Stop running work |
| `steer` | Mid-turn correction (Codex) |
| `respond_user_input` / `user_input_result` | Structured Codex question response and terminal request status |
| `queue_update` | FIFO snapshot including queued/running/paused entries |
| `cancel_prompt` / `prompt_cancelled` | Cancel a not-yet-started queue entry |
| `resume_prompt_queue` | Explicitly resume a paused per-session queue |
| `start_thread` / `thread_*` | Agent-scoped phone-local draft, first-prompt native start into permitted discovered dirs |
| `pair_*` / `key_package` / `phone_revoked` | Pairing and E2E key distribution |
| `attention_event` | Generic push-driving event class (sealed-safe) |

### History and subscription

| Type | Role |
|---|---|
| `subscribe` | Phone requests device/session subscription |
| `subscribe_ack` | Server acknowledges subscription readiness |
| `fetch_history` | Request native/server history window |
| `session_history` | History payload response |

### Pairing

| Type | Role |
|---|---|
| `pair_request` | Start/request pairing material |
| `pair_confirm` | Confirm pairing |

(HTTP pair generate/consume APIs also exist; see [configuration.md](./configuration.md).)

### Control

| Type | Role |
|---|---|
| `approve` | Approve pending tool (when CLI supports it) |
| `deny` | Deny pending tool |
| `interrupt` | Interrupt running agent work |
| `heartbeat` | Liveness |
| `error` | Error envelope |

## Explicit non-goals on the wire

- **No generic phone-side `create_session`** or nest-only ghost threads.
- The **only** allowed phone creation path is agent-scoped `start_thread`: first open a phone-local draft, then create natively with its first prompt only when the selected agent's starter is installed/probed and advertises `spawn=true`. Its directory must be in the daemon's **current union of native-discovered project directories**.
- The lifecycle is `thread_starting → thread_owned | thread_failed | thread_indeterminate`; set `thread_owned` only after positive first-prompt acknowledgement and ownership from the selected agent's native store, otherwise report `thread_indeterminate`.
- Do not invent permanent nest-only session rows.

## REST companion APIs

WebSocket carries most interactive traffic. REST (phone secret unless noted):

| Method | Path | Notes |
|---|---|---|
| GET | `/health` | Unauthenticated liveness plus `protocol_version`, `server_version`, and authoritative `transport_mode` |
| GET | `/api/devices` | Device list |
| POST | `/api/devices/register` | Bootstrap token header |
| GET | `/api/devices/sessions` | Sessions |
| GET | `/api/messages` | Messages |
| POST/GET | `/api/attachments`… | Upload / download |
| POST | `/api/push/subscribe` | Push |
| GET | `/api/push/vapid-public-key` | VAPID public |
| POST | `/api/pair/generate` | Pair code issue |
| POST | `/api/pair/consume` | Pair code consume |
| GET | `/ws/phone` | Phone WS upgrade |
| GET | `/ws/daemon` | Daemon WS upgrade |

Auth header details: [security.md](./security.md), [configuration.md](./configuration.md).

## Change checklist

When the wire contract changes:

1. Update `protocol/protocol.json`
2. Update `server/internal/protocol/types.go` and handlers/persistence/tests
3. Update `pwa/src/types/protocol.ts` and stores/API/tests
4. Update daemon message dispatch and adapter boundaries
5. Update this doc + README agent tables if enums or product behavior changed
6. Run **server**, **daemon**, and **pwa** test suites

## Related docs

- [Architecture](./architecture.md)
- [Development](./development.md)
- [AGENTS.md](../AGENTS.md)
