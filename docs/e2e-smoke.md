> English | [简体中文](./e2e-smoke.zh-CN.md)

# End-to-end smoke checklist

Acceptance path after deploy or deploy-sensitive changes. Product target: [v1-product.md](./v1-product.md). Release cut: [release.md](./release.md). Migration: [migration-v1.md](./migration-v1.md).

Run this checklist against the **updated live build**, not only a local mock or
pre-deploy binary. For runtime-affecting maintenance of the maintained nest,
deployment plus the relevant checks below is the default final acceptance unless
the task explicitly says local-only / no-deploy. Record the exact commit and
artifact hashes, preserve rollback copies before replacement, and include public
health, daemon reconnect, and the changed user workflow in the evidence. A deploy
does not by itself authorize a tag or GitHub Release.

## Modes

| Mode | Env | When |
|---|---|---|
| **Sealed (new-nest default)** | Leave Server mode unset on a new DB; daemon registration persists the returned mode; PWA reads `/health` | Normal new installation after pair with QR JSON + key packages |
| **Open (admin-selected / legacy)** | Set `NEKONEST_TRANSPORT_MODE=open` only when initializing a new open DB, or keep an existing open DB/config | Trusted relay operation where VPS plaintext access is accepted |

One nest = one persisted mode. An environment/build override is only an assertion after initialization. Mismatch rejects startup/connection (no sealed→open downgrade).

## Preconditions (open mode)

- [ ] VPS server running; `GET /health` → `status=nyan~` plus expected `server_version` / `protocol_version`
- [ ] `NEKONEST_ADMIN_SECRET` set (or legacy `NEKONEST_PHONE_SECRET`)
- [ ] `NEKONEST_BOOTSTRAP_TOKEN` set and used at daemon register
- [ ] `NEKONEST_TRANSPORT_MODE=open` on server **and** daemon
- [ ] HTTPS / WSS work through the reverse proxy
- [ ] `NEKONEST_ALLOWED_ORIGINS` includes the public origin (recommended)
- [ ] Host registered (`nekonest-daemon -register`); `config.json` has device token
- [ ] `nekonest-daemon -doctor` critical checks green (or only expected missing CLIs)
- [ ] Daemon process online (single instance for that config)
- [ ] At least one supported agent CLI has a recent main-thread session on the host

## A. Open-mode core path

1. Open PWA; enter the nest admin secret (setup). A wrong key stays on the setup page. An intentional open nest asks for a Chinese confirm, not a “mismatch”.
2. Pair: run `nekonest-daemon -pair gen` on the host; paste **QR JSON** (preferred) or 6-digit code; compare **fingerprint** with the PC screen.  
3. Device list shows the host **online**. The page-level PWA / Server releases align; each device card shows that machine's Daemon release. A deliberately stale PWA shows **Refresh now**; only the stale machine is marked for a Daemon update.
4. On the host, open/use a supported agent so a recent thread exists.  
5. Refresh the phone PWA: it first shows its cached catalog, then the online daemon promptly rescans and pushes the current **directory → agent → thread** list. Only agents with threads render as agent groups; startable missing agents stay in the project's **New** menu. Session capabilities appear when advertised.
6. Open a thread; history loads; send a short prompt; stream appears.  
7. Delivery UX: outbox moves toward **committed** (not cleared only on bare WS write).  
8. Attachments (optional): one small PNG and one text file; agent reads or clear error.  
9. Interrupt a long run if the session advertises `interrupt`; process tree does not linger (Windows Job Object / Linux process group).  
10. Stop daemon → phone shows **offline**; start → **online**.  
11. Wrong secret / revoked phone token → 401 / cannot operate.  
12. Reconnect mid-send: same `client_msg_id`; no double agent turn.

## B. Codex control path (when CLI present)

Full-control baseline: **codex-cli 0.146.0+** with `codex app-server`.

1. `nekonest-daemon -doctor` reports installed/minimum versions and probes initialize, thread/start, turn/start, steer, interrupt, approval decision shape, and requestUserInput fields.
2. If capabilities show `control_mode=app_server` and `approve=true`:  
   - Trigger a real approval on host; phone shows approval UI; Approve/Deny resolve.  
3. On a Codex Plan-mode thread, trigger `requestUserInput`: answer options, Other/free text, and a Secret question; confirm expiry disables submission and uncertain/stale requests are not retried. NekoNest does not add a Plan-mode selector in this release.
4. Start a long turn, send two follow-ups with the main Send action, then verify FIFO order, cancel a waiting item, pause on interrupt/failure, and explicitly resume. Use Steer separately and confirm it modifies the active turn.
5. Start a native thread with one image and one ordinary file. Both must be present in the same first `turn/start`; navigate only after prompt acceptance plus native-store ownership.
6. Kill app-server during work: capability immediately degrades, the session becomes error, queue pauses, a generic failure attention event is emitted, and bounded re-initialize restores capability without replaying the uncertain turn/request.
7. For every agent advertising `spawn=true`: target only the daemon's **currently discovered** native project union; lifecycle `thread_starting → thread_owned | failed | indeterminate`; no ghost nest-only row.
8. A CLI below 0.146.0 or failed method probe stays `exec_resume`; no fake approval, user input, queue, steer, ordinary-file, or spawn capability.

## C. Sealed mode and notification pass

1. Create a fresh data directory without a mode override; `/health.transport_mode` is `sealed`. Register/re-pair so the daemon config and wrap keys match.
2. Configure real VAPID and subscribe the phone. Trigger approval, structured input, failure, and completion; Push text is generic and each deep link opens the referenced session where details decrypt.
3. Exercise prompt send/reconnect and queued retry. The Server replays the exact stored sealed envelope (same nonce/ciphertext/AAD) for one `client_msg_id`.
4. Scan Server DB and logs for the unique prompt, answers, approval detail, attachment filename/path, and tool body; none may appear as plaintext.
5. Existing open DB upgrades still report open. Open/ sealed environment, daemon-config, and PWA build mismatches are all **rejected**.

## D. Migration smoke (if upgrading from v0.1)

1. Stop writers; `nekonest-server -migrate-v1 -data ./data -backup ./data-backup-v1`.  
2. Device tokens still authenticate; old plaintext messages gone from live DB.  
3. Phones re-login and re-pair.

## Known limitations (not failures)

- Codex app-server is capability-gated by the 0.146.0 minimum and live schema/initialize probe
- Non-Codex agents: compatibility resume only (no approval/steer/queue promise; `start_thread` only when `start_capabilities.spawn=true`) — see [agent-capability-matrix.md](./agent-capability-matrix.md)
- Max 5 attachments, 4 MB each (open path)  
- Web Push needs VAPID; sealed push bodies stay generic  
- Formal hosts: Windows + Linux; macOS later  
- Open mode: VPS can read application plaintext  

## Protocol 1.2 capability acceptance

1. Capture a live and reconnect `session_list`; confirm both retain the daemon producer version and explicitly include every boolean capability plus `unavailable_reasons`.
2. Against an isolated 1.1 fixture, confirm legacy send/interrupt remains usable. Remove the producer version or use 1.2 with missing flags and confirm the controls stay closed.
3. Queue two prompts on each reliable installed path. Verify FIFO success advances automatically; failure/interrupt pauses later items; restart converts an unconfirmed running item to `blocked_indeterminate`; explicit Skip advances without replaying its `client_msg_id`.
4. Start a native thread and independently exercise the four prompt-success/ownership quadrants. Only the both-positive quadrant may become `thread_owned`; long first turns must not be terminated by a PWA timer.
5. Keep the maintained production nest in its persisted transport mode. Run sealed acceptance only on an isolated fresh data directory; scan Server DB/logs for prompt, response, path, approval, and attachment plaintext.

## Related

- [Troubleshooting](./troubleshooting.md)
- [VPS deploy](./deploy-vps.md)
- [Windows deploy](./deploy-windows.md)
- [Linux deploy](./deploy-linux.md)
- [Security](./security.md)
- [v1 product contract](./v1-product.md)
