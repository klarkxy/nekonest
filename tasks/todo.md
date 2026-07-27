# Review Findings Remediation Checklist

## Task 1: Secure server defaults and WebSocket lifecycle

- [x] Restrict secret-free development mode to loopback and local origins.
- [x] Require a phone secret before enabling a public bind.
- [x] Close phone sockets and bound/rescope device subscriptions.
- [x] Verify with focused server tests.

## Task 2: Durable prompt idempotency and acceptance

- [x] Persist commands by `(device_id, client_msg_id)` before forwarding.
- [x] Suppress replay and return the existing state.
- [x] Have daemon report accepted or failed execution start.
- [x] Verify lost/replayed ACK behavior.

## Task 3: PWA delivery-state outbox

- [x] Keep queued messages visible without restoring a second draft.
- [x] Clear only on daemon acceptance.
- [x] Preserve failed messages for explicit retry.
- [x] Add store/WebSocket tests.

## Task 4: Real npm shim support

- [x] Resolve standard npm `%dp0%` and `%_prog%` wrappers without `cmd.exe`.
- [x] Add a real npm-style fixture test.
- [x] Verify adapter smoke launch resolution.

## Task 5: Race-free config reload

- [x] Publish immutable config snapshots.
- [x] Do not apply credential changes until restart.
- [x] Make URL reconnect generation-safe.
- [x] Add focused tests.

## Task 6: Process and adapter correctness

- [x] Remove duplicate non-Windows helper.
- [x] Make Job Object failures visible and terminate the full tree reliably.
- [x] Route unknown UUID sessions without assuming Codex.
- [x] Verify Windows tests and Linux cross-build.

## Task 7: Bounded history and phone resources

- [x] Clamp and validate WS history limits.
- [x] Remove empty subscription keys and validate/rescope subscriptions.
- [x] Bound limiter cleanup work.
- [x] Add regression tests.

## Task 8: Multi-device Push

- [x] Migrate from global endpoint uniqueness to endpoint/device uniqueness.
- [x] Return truthful subscription results.
- [x] Test one endpoint subscribed to multiple devices.

## Task 9: PWA async isolation

- [x] Cancel stale reconnect timers and ignore stale socket events.
- [x] Abort or generation-guard device/session fetches and uploads.
- [x] Scope error handling to the active session.
- [x] Verify IME-safe send and notification tags while touching the flow.

## Final verification

- [x] `go test -count=1 ./...` in daemon and server.
- [x] `go vet ./...` in daemon and server.
- [x] Linux daemon cross-build.
- [x] PWA tests, type-check, production build, and production audit.
- [x] `gofmt -l`, `git diff --check`, and focused diff review.
