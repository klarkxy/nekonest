> English | [简体中文](./e2e-smoke.zh-CN.md)

# Acceptance checklist

Run this after a new installation, an upgrade, or any change that depends on the
real reverse proxy, service worker, host daemon, native agent store, or reconnect
path. Local tests are useful preflight, not a substitute for this workflow.

## Before changing a live nest

- [ ] Record the deployed Server, PWA, and daemon versions or artifact hashes.
- [ ] Back up the Server data directory, Server secrets, and host daemon state.
- [ ] Preserve the previous Server image/binary, PWA, and daemon binary as
      rollback material.
- [ ] Confirm the current `/health` and host online state.

## Core path

- [ ] Public `https://your-nest/health` succeeds through the reverse proxy.
- [ ] A wrong admin secret is rejected; the correct one completes setup.
- [ ] `nekonest-daemon -doctor` has no critical config or network error.
- [ ] After `install` and `start`, `nekonest-daemon status` reports the host
      service installed and the process lock held.
- [ ] A fresh pair code connects the intended host and the phone shows it online.
- [ ] A recent native main thread appears under **directory → agent → thread**.
- [ ] Opening the thread loads native history without duplicate turns.
- [ ] A short prompt streams output and reaches a final delivery state.
- [ ] One small image or text attachment is delivered, or a clear unsupported
      reason is shown.
- [ ] If Interrupt is advertised, a long turn stops and the host process does not
      remain running.
- [ ] Stopping the daemon makes the host offline; restarting it reconnects and
      restores the thread view.
- [ ] Closing and reopening the PWA loads the current build without a stale loop.

## Capability-driven checks

Test only controls currently enabled by the PWA:

- [ ] Approval, denial, and steering complete a real native request when advertised.
- [ ] A Codex Plan-mode prompt raises a real structured question in the PWA;
      answering it resumes the same native turn.
- [ ] Queue controls preserve order across reconnect; a queued item can be
      cancelled before native execution, and uncertain work requires an explicit
      user decision.
- [ ] New thread creation targets an already discovered project and navigates to
      the thread only after native creation is confirmed.
- [ ] A disabled control explains why it is unavailable instead of doing nothing.

Codex normally provides the broadest control surface. Other agents may expose a
smaller compatibility set; the running capability catalog, not a static table,
decides this checklist.

## Transport checks

### Sealed nest

- [ ] `/health` reports `sealed` and every client accepts that mode.
- [ ] Pairing completes with the expected phone/host identity.
- [ ] A unique test prompt, response, path, approval detail, and attachment name
      do not appear as plaintext in Server logs or the Server database.
- [ ] Reconnect does not create a second native turn for one user action.

### Open nest

- [ ] `/health` reports `open` and the first-use plaintext warning is explicitly
      accepted.
- [ ] Operators understand that the VPS can read application content.

Never run sealed tests against an existing open data directory or try to change
a nest's stored mode during acceptance.

## Upgrade closeout

- [ ] Server and daemon stay healthy after several reconnect/discovery cycles.
- [ ] The PWA and component version indicators match the intended deployment.
- [ ] The changed user workflow passes on the real phone/host path.
- [ ] Logs contain no new crash loop, repeated authentication failure, or secret
      material.
- [ ] Rollback material and backup locations are recorded until acceptance is
      complete.

## Related

- [VPS deploy](./deploy-vps.md)
- [Windows host](./deploy-windows.md)
- [Linux host](./deploy-linux.md)
- [Troubleshooting](./troubleshooting.md)
- [Security](./security.md)
