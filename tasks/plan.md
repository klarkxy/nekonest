# Implementation Plan: Review Findings Remediation

## Overview

Fix the confirmed security, delivery-reliability, daemon launch/process, server resource, push, and PWA async-isolation defects without discarding the existing working-tree changes.

## Architecture Decisions

- Default server startup is secure: public phone access requires a secret; secret-free development is loopback-bound with local origins only.
- Prompt delivery uses a stable `client_msg_id` with durable server-side idempotency and a daemon acceptance result before the PWA clears its outbox.
- Runtime daemon configuration is read from immutable snapshots; credential changes remain unapplied until restart.
- Browser async work is scoped by connection, device, session, and route generations.
- Push subscriptions model one browser endpoint mapped to multiple devices.

## Task List

### Phase 1: Security and delivery foundations

- [x] Task 1: Secure server defaults and phone WebSocket lifecycle.
- [x] Task 2: Add durable prompt idempotency and daemon acceptance acknowledgements.
- [x] Task 3: Align the PWA outbox with queued/accepted/failed delivery states.

### Checkpoint: Security and delivery

- [x] Server and PWA tests cover unauthorized defaults, replay, reconnect, and failure recovery.

### Phase 2: Daemon correctness

- [x] Task 4: Resolve real npm Windows shims and add fixture coverage.
- [x] Task 5: Make config reload race-free and prevent mixed credentials.
- [x] Task 6: Repair process-tree handling, Claude fallback routing, formatting, and non-Windows builds.

### Checkpoint: Daemon

- [x] Windows tests, race-relevant tests, vet, formatting, and Linux cross-build pass.

### Phase 3: Server and PWA isolation

- [x] Task 7: Bound history reads and harden resubscription/resource cleanup.
- [x] Task 8: Migrate Push storage to endpoint/device mappings.
- [x] Task 9: Fix reconnect timers and route/session-scoped async state.

### Checkpoint: Complete

- [x] All Go and PWA tests pass.
- [x] PWA type-check and production build pass.
- [x] Vet, formatting, diff check, dependency audit, and Linux cross-build pass.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Existing SQLite databases use the old Push schema | High | Perform an idempotent transactional migration and test legacy data |
| ACK contract changes span daemon/server/PWA | High | Keep backward-compatible message fields and add end-to-end state tests |
| Dirty working tree contains user changes | High | Patch narrowly and inspect every touched diff before final verification |
| Windows process behavior is platform-specific | Medium | Keep Windows-only helpers isolated and verify non-Windows cross-build |

## Open Questions

- None blocking; the confirmed review findings define the accepted scope.
