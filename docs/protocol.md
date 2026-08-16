> English | [简体中文](./protocol.zh-CN.md)

# Wire protocol

This is a contributor guide to compatibility and ownership. It intentionally
does not duplicate the message catalog or object fields.

## Authoritative surfaces

| Surface | Role |
|---|---|
| `protocol/protocol.json` | Language-neutral wire schema and enum values |
| `relaycore/protocol/` | Shared Go protocol behavior |
| `server/internal/protocol/` | Server-facing Go aliases and compatibility |
| `pwa/src/types/protocol.ts` | Browser wire types |
| Daemon dispatch and adapter tests | Host payload construction and native boundaries |

The JSON schema is maintained manually. A doc example is never a substitute for
checking these surfaces together.

## Compatibility rules

- Versions use `major.minor`.
- A major mismatch is rejected.
- A minor change may add optional fields or message types without changing the
  meaning of existing required data.
- Unknown optional data is ignored safely. Missing capability flags are false.
- Application release versions are separate from the wire version.
- Each nest has one persisted transport mode: `sealed` or `open`. Clients must
  agree with it; there is no automatic downgrade.

## Stable product invariants

- Active agent identities are `claude_code`, `codex`, `kimi_cli`,
  `grok_build`, `zcode`, and `cursor`. Current catalogs parse `zcode` but
  must not advertise it while headless ZCode login is broken upstream.
- Native agent stores remain authoritative for thread ownership and history.
- Phone controls are capability-gated and fail closed when absent or unknown.
- Stable device, thread, message, and client action identities prevent duplicate
  work across replay and reconnect.
- Transport success, daemon acceptance, and durable completion are distinct
  outcomes.
- Generic phone-created or nest-only sessions are not part of the wire. Native
  thread creation is agent-scoped, project-scoped, and confirmed by native
  ownership.
- Sealed retries preserve the original encrypted command identity; they are not
  re-encrypted as a new copy of the same action.

## Changing the protocol

1. Decide whether the change is backward-compatible and update the version only
   when required.
2. Change `protocol/protocol.json` first.
3. Update every applicable Go, daemon, and PWA surface.
4. Add positive, negative, mixed-version, reconnect, and persistence tests for
   the changed behavior.
5. Update operator docs only when user-visible behavior, configuration, or
   recovery changes.
6. Run the full Server, daemon, and PWA verification suites.

Do not add a second prose copy of new enums or payload fields here. Link to the
schema or a focused test instead.

## Related

- [Architecture](./architecture.md)
- [Development](./development.md)
- [AGENTS.md](../AGENTS.md)
