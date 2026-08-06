> English | [简体中文](./e2e-smoke.zh-CN.md)

# End-to-end smoke checklist

Acceptance path after deploy or deploy-sensitive changes. Product target: [v1-product.md](./v1-product.md). Release cut: [release.md](./release.md). Migration: [migration-v1.md](./migration-v1.md).

## Modes

| Mode | Env | When |
|---|---|---|
| **Open (recommended first)** | `NEKONEST_TRANSPORT_MODE=open` on **server and daemon**; PWA default open | Daily use while sealing is validated |
| **Sealed** | `NEKONEST_TRANSPORT_MODE=sealed` on server and daemon; PWA `VITE_NEKONEST_TRANSPORT_MODE=sealed` | After pair with QR JSON + key packages |

One nest = one mode. Mismatch rejects the handshake (no sealed→open downgrade).

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

1. Open PWA; enter the nest admin secret (setup).  
2. Pair: run `nekonest-daemon -pair gen` on the host; paste **QR JSON** (preferred) or 6-digit code; compare **fingerprint** with the PC screen.  
3. Device list shows the host **online**. The page-level PWA / Server releases align; each device card shows that machine's Daemon release. A deliberately stale PWA shows **Refresh now**; only the stale machine is marked for a Daemon update.
4. On the host, open/use a supported agent so a recent thread exists.  
5. Phone: **directory → agent → thread** visible; session capabilities appear when advertised.  
6. Open a thread; history loads; send a short prompt; stream appears.  
7. Delivery UX: outbox moves toward **committed** (not cleared only on bare WS write).  
8. Attachments (optional): one small PNG and one text file; agent reads or clear error.  
9. Interrupt a long run if the session advertises `interrupt`; process tree does not linger (Windows Job Object / Linux process group).  
10. Stop daemon → phone shows **offline**; start → **online**.  
11. Wrong secret / revoked phone token → 401 / cannot operate.  
12. Reconnect mid-send: same `client_msg_id`; no double agent turn.

## B. Codex control path (when CLI present)

Local baseline used in development: **codex-cli 0.144.1** with `codex app-server`.

1. `nekonest-daemon -doctor` logs app-server `available` / `ensure`.  
2. If capabilities show `control_mode=app_server` and `approve=true`:  
   - Trigger a real approval on host; phone shows approval UI; Approve/Deny resolve.  
3. If `steer=true`: steer mid-turn; agent incorporates correction.  
4. For every agent advertising `spawn=true`: start_thread only for a directory in the daemon's **currently discovered** native project union; lifecycle `thread_starting → thread_owned | failed | indeterminate`; no ghost nest-only row.
5. If app-server unhealthy: Codex stays `exec_resume` (send/history/interrupt only); no fake approve/spawn.

## C. Sealed mode (optional second pass)

1. Set sealed on server + daemon + PWA build/env; restart all.  
2. Re-pair with QR JSON so wrap keys match.  
3. Confirm nest DB/logs do not contain prompt plaintext for new sealed `session_message` traffic.  
4. Chat still works after key_package delivery.  
5. Open-mode client against sealed nest is **rejected** (and the reverse).

## D. Migration smoke (if upgrading from v0.1)

1. Stop writers; `nekonest-server -migrate-v1 -data ./data -backup ./data-backup-v1`.  
2. Device tokens still authenticate; old plaintext messages gone from live DB.  
3. Phones re-login and re-pair.

## Known limitations (not failures)

- Sealed default is the **product target**; operators may run open until sealed e2e is signed off  
- Codex app-server JSON-RPC method names vary by CLI version — doctor/probe may show available while a specific method alias fails  
- Non-Codex agents: compatibility resume only (no approval/steer/queue promise; `start_thread` only when `start_capabilities.spawn=true`) — see [agent-capability-matrix.md](./agent-capability-matrix.md)
- Max 5 attachments, 4 MB each (open path)  
- Web Push needs VAPID; sealed push bodies stay generic  
- Formal hosts: Windows + Linux; macOS later  
- Open mode: VPS can read application plaintext  

## Related

- [Troubleshooting](./troubleshooting.md)
- [VPS deploy](./deploy-vps.md)
- [Windows deploy](./deploy-windows.md)
- [Linux deploy](./deploy-linux.md)
- [Security](./security.md)
- [v1 product contract](./v1-product.md)
