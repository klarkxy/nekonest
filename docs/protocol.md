> English | [简体中文](./protocol.zh-CN.md)

# Protocol overview

Language-neutral wire contract for NekoNest phone ↔ server ↔ daemon communication. The normative schema file is [`protocol/protocol.json`](../protocol/protocol.json). Types are **manually maintained** in parallel:

- `protocol/protocol.json`
- `server/internal/protocol/types.go`
- `pwa/src/types/protocol.ts`
- Daemon dispatch / payload construction

Keep JSON field names, enums, optionality, timestamps, and meanings identical across surfaces.

## Envelope

Every WebSocket application message is a `NekoMessage`:

| Field | Required | Description |
|---|---|---|
| `type` | yes | One of `MessageType` enum strings |
| `device_id` | yes | Device identifier (daemon identity or routing context) |
| `timestamp` | yes | Unix timestamp in **seconds** |
| `session_id` | no | Agent session / thread id when message is session-scoped |
| `payload` | no | Type-specific object |

`additionalProperties` is false on the envelope in the schema—unknown top-level keys should not be invented casually.

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
| `os` | Currently `"windows"` |
| `status` | `online` \| `offline` |
| `last_seen` | Unix seconds |
| `active_agents` | Session-count hint (not distinct agent types); PWA labels it as threads |

### AgentSession

| Field | Notes |
|---|---|
| `id` | Public/wire session id |
| `device_id` | Owning device |
| `agent_type` | Wire agent id |
| `status` | `running` \| `idle` \| `waiting_approval` |
| `summary`, `last_activity` | List UX |
| `project_dir` / `project` | Directory grouping |
| `pending_approval` | Optional tool approval blob |

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
| `prompt_accepted` | Accepted into daemon pipeline |
| `prompt_committed` | Journal committed |
| `prompt_failed` | Failed visibly |
| `prompt_sent` | Sent/ack signal used by clients (outbox clearing) |

**Do not** collapse “WebSocket write succeeded” into `prompt_accepted` / business success.

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

- **No phone-side `create_session`** (or equivalent) in the supported product contract. Threads are created on the PC in the native agent UI/CLI first.
- Do not reintroduce remote thread creation without an explicit product decision and full-stack update.

## REST companion APIs

WebSocket carries most interactive traffic. REST (phone secret unless noted):

| Method | Path | Notes |
|---|---|---|
| GET | `/health` | Unauthenticated liveness |
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
