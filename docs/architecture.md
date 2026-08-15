> English | [简体中文](./architecture.zh-CN.md)

# Architecture

This document describes stable component and ownership boundaries. Package
details and wire fields belong to the source tree and protocol schema.

## System shape

<div align="center">
  <img src="./images/how-it-works.en.jpg" width="920" alt="The phone connects to a VPS while the Windows or Linux host daemon connects outbound and controls local coding agents">
</div>

| Component | Responsibility | Does not own |
|---|---|---|
| Phone PWA | Authentication UI, pairing, session view, prompts, attachments, and capability-gated controls | Native sessions or agent credentials |
| VPS Server | Authentication, relay, required durability, attachments, and optional Web Push | Native agent history or host filesystem access |
| Host daemon | Outbound connection, native discovery/history, delivery journal, and agent process control | Product accounts or public ingress |
| Native agent | Thread store and actual coding-agent execution | Phone/VPS transport |

The daemon always connects outward. The PWA and daemon never connect directly
to each other across the home network.

## Data ownership

- Native agent stores are authoritative for thread discovery, history, and
  ownership.
- The phone presents a derived **directory → agent → thread** tree. It does not
  rewrite native session data.
- The Server persists only the state needed for authentication, relay,
  durability, attachments, and notifications.
- Sealed transport keeps application bodies encrypted between phone and daemon;
  open transport allows Server plaintext access.

## Interaction flow

```text
phone action
  → authenticated Server relay
  → daemon validation and durable delivery boundary
  → selected native agent
  → streamed result through the same path
```

A network write is not the same as agent acceptance. NekoNest keeps stable
message identities across reconnects and fails closed when it cannot safely
decide whether work already crossed the native agent boundary.

New thread creation follows the same rule: the phone starts with a local draft,
the first prompt invokes a supported native starter, and the UI enters the real
thread only after native ownership is confirmed.

## Capability boundary

The daemon is the capability producer. The Server relays the catalog and the
PWA gates every control from it. Compatibility adapters must not imitate
approval, steering, structured input, queueing, file support, or thread creation
that the installed native path did not confirm.

See [agent support](./agent-capability-matrix.md) for the user-facing policy.

## Reliability and security rules

- One reader and one serialized writer per WebSocket connection.
- Slow filesystem, agent, or network work runs outside shared registry locks.
- Reconnect generations prevent stale connections from replacing newer state.
- Prompt retries reuse the same identity; uncertain native execution is not
  silently replayed.
- Remote and agent content is untrusted; PWA Markdown is sanitized.
- Host child processes must stop on interrupt, adapter close, or daemon shutdown.

## Source map

| Path | Authority |
|---|---|
| `protocol/protocol.json` | Wire schema |
| `relaycore/` | Shared single-nest relay engine |
| `server/` | Self-hosted VPS service and storage |
| `daemon/` | Host connection, adapters, and agent execution |
| `pwa/` | Mobile client |

For implementation invariants and required cross-layer checks, read
[AGENTS.md](../AGENTS.md). For wire compatibility, read
[protocol.md](./protocol.md).
