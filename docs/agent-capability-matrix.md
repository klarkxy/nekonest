> English | [简体中文](./agent-capability-matrix.zh-CN.md)

# Agent support and runtime capabilities

NekoNest detects capabilities from the installed agent path. This page defines
the stable support policy; it is not a version-by-version control matrix.

## Support tiers

| Agent | Stable NekoNest role |
|---|---|
| Claude Code | Compatibility resume of native threads |
| Codex | Full-control path through `codex app-server`, with compatibility fallback |
| Kimi CLI | Compatibility resume of native threads |
| Grok Build | Compatibility resume of native threads |

Compatibility resume covers discovery, native ownership, history, prompt
execution/streaming, interruption, and attachments only when the installed path
advertises them. It does not promise phone approval, steering, structured input,
queueing, new-thread creation, or a particular attachment method.

Codex is the only agent with a supported full-control role. Even for Codex, the
PWA enables each control only after the installed app-server path passes its
runtime probes. A fallback may expose fewer controls.

Structured Codex questions are available on explicit **Plan mode** turns. The
PWA keeps normal execution as the default because Plan mode plans and asks for
decisions instead of performing implementation work.

## Runtime is authoritative

For every thread, the PWA uses the daemon's current capability catalog. Missing
or unknown flags are unsupported. This protects mixed-version installs and
agents whose CLI behavior changes independently of NekoNest.

When a control is unavailable:

1. Read the reason shown by the PWA.
2. Run `nekonest-daemon -doctor` on the host.
3. Confirm the intended agent CLI is installed for the same OS user as the
   daemon.
4. Update or repair that CLI if appropriate, then reconnect the daemon.

Do not override the UI gate based on this document.

## Stable guarantees

- Native agent stores remain authoritative for discovery, history, and
  ownership.
- One missing or broken agent does not disable the others.
- A thread is routed only after the selected adapter proves native ownership.
- Agent requests are never inferred from transcript text; approval and input UI
  require a current native signal.
- New thread creation is limited to already discovered projects and appears only
  when the selected native starter advertises it.
- Unsupported controls show a reason instead of silently doing nothing.
- Agent stderr is diagnostic output, not assistant content.

## Out of scope

NekoNest does not provide arbitrary filesystem browsing, generic nest-only
sessions, simulated approvals, or guessed steering. If a compatibility CLI
requires interaction that it cannot expose headlessly, complete that step in
the host terminal.

## Related

- [Troubleshooting](./troubleshooting.md)
- [Acceptance checklist](./e2e-smoke.md)
- [Architecture](./architecture.md)
