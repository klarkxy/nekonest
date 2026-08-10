> English | [简体中文](./troubleshooting.zh-CN.md)

# Troubleshooting

Symptom-oriented checks for self-hosted NekoNest. Configuration details: [configuration.md](./configuration.md). Security context: [security.md](./security.md).

## Cannot open PWA / immediate auth failure

| Check | Expectation |
|---|---|
| URL | HTTPS public origin (or loopback for dev) |
| Phone secret | Exactly matches `NEKONEST_PHONE_SECRET` |
| Reverse proxy | WebSocket upgrade headers present (Nginx) |
| Origins | `NEKONEST_ALLOWED_ORIGINS` includes the browser origin |
| `/health` | `{"status":"nyan~"}` on the server |

401 / unable to operate usually means wrong secret or missing auth headers on API calls.

## Daemon will not register

| Check | Expectation |
|---|---|
| `NEKONEST_SERVER` | Reachable base URL (`https://…` or local `http://…`) |
| `NEKONEST_BOOTSTRAP_TOKEN` | Identical to server value on public VPS |
| Server bootstrap | Set when phone secret is set; otherwise register may be disabled |
| TLS | System trust store accepts the certificate |
| Clock | Not wildly skewed |

## Device stays offline

| Check | Expectation |
|---|---|
| Daemon process | Running; log shows authenticated device id |
| Second instance | Refused by `.daemon.lock`—only one process per config |
| `config.json` | Valid `server_url`, `device_id`, `token` |
| Network | PC can open outbound WSS to VPS |
| Server | Up; not crash-looping |

Phone list should flip online within a short reconnect window after daemon start.

## Pair code rejected

| Check | Expectation |
|---|---|
| Fresh code | Codes expire (~5 minutes)—run `-pair gen` |
| Digits | 6 digits; PWA normalizes input—avoid extra spaces/letters |
| Phone auth | Already logged in with correct phone secret |
| Same nest | Code issued by a daemon registered to **this** server |

## No sessions / empty directory tree

| Check | Expectation |
|---|---|
| PC threads exist | Create/use sessions in the native agent **on the PC** first |
| CLI installed | Agent on PATH; adapter not silently unavailable |
| Native store paths | Default locations under user profile (see agents table in README) |
| Discover interval | Initial scan starts after a few seconds; normal periodic updates may take about 30 seconds |
| Recent window | Threads inactive for more than 7 days, and projects backed only by those threads, are hidden without deleting native data; use the thread again on the PC to restore it |
| Ownership filters | Subagents/sidechains hidden by design |
| Directory grouping | Orphans appear under **未分类** |

Phone never creates remote threads.

## Prompt stuck, busy, or “still running”

| Check | Expectation |
|---|---|
| Session status | `running` blocks overlapping sends in UI |
| Outbox | Pending `client_msg_id` entries in localStorage; reconnect resends **same** id |
| Outbox full | Cap 40—wait for acks |
| Daemon journal | Fail-closed indeterminate state surfaces as error, not silent success |
| CLI hang | Interrupt from UI if supported; else stop process on PC |

Do not manually mint a new message id to “retry” the same user action if the first may have been accepted.

## Duplicate or missing messages after reconnect

| Check | Expectation |
|---|---|
| Stable ids | History merge uses message ids; optimistic locals should drop when server/native catch up |
| SW update | After major PWA upgrade, one full close/reopen may be required |
| fetch_history | Re-open thread to resync empty/partial views |

## Attachments fail

| Check | Expectation |
|---|---|
| Count / size | ≤ 5 files, ≤ 4 MB each |
| MIME | Images (jpeg/png/webp/gif), txt, markdown, pdf, json |
| Upload | Phone secret valid; server disk space in `data/attachments` |
| Daemon download | Device online; can GET attachment URL from VPS |
| Agent wiring | Claude/Codex use native file/image mechanisms where advertised; **Kimi CLI / Grok Build** may only get local paths in the prompt—CLI sandbox may block temp paths |

## Approvals never complete on phone

Expected for agents whose non-interactive CLI cannot host approval UX. Finish the approval in the **PC terminal**, then continue from the phone if the session is idle again.

## Web Push never arrives

| Check | Expectation |
|---|---|
| All three VAPID env vars | Public, private, subject set on server |
| Browser permission | Granted; subscription posted to `/api/push/subscribe` |
| HTTPS | Required for Push API on real devices |

Without VAPID, the server skips real push sends.

## After upgrade: blank UI or old client

1. Fully close the PWA / browser tab.
2. Reopen once so the service worker can activate.
3. Hard-refresh if your browser keeps a stuck worker.
4. Confirm VPS is serving the new `pwa/dist` assets.

## Server won’t start / binds wrong interface

| Symptom | Cause |
|---|---|
| Only on 127.0.0.1 | Phone secret unset (by design) |
| Registration 503 / disabled | Phone secret set but bootstrap token empty |
| Proxy rate-limit weirdness | `TRUST_PROXY` on without overwriting XFF |

## Windows Defender / AV kills the daemon

Self-hosters sometimes add path/process exclusions (see [deploy-windows.md](./deploy-windows.md)). Understand the security tradeoff first.

## Still stuck

1. Capture **non-secret** logs from `journalctl -u nekonest`,
   `docker compose logs server`, or the daemon console. Set
   `NEKONEST_LOG_FORMAT=json` for machine parsing and use
   `NEKONEST_LOG_LEVEL=debug` only for a bounded diagnostic window.
2. Verify `/health` and device online state.
3. Run [e2e-smoke.md](./e2e-smoke.md) checklist items until the first failure.
4. For contributors, trace PWA → server → daemon → adapter end-to-end ([architecture.md](./architecture.md)).

Never paste device tokens, phone secrets, or bootstrap tokens into public issues.
JSON logs deliberately omit raw upstream errors and user content; correlate by
`component`, `event`, and stable pseudonyms for available device/session/message identifiers. Raw identifiers are not emitted, so matching events remain correlatable without turning an attacker-controlled wire field into log content.

## Protocol 1.2 controls and blockers

- **History is visible but Send is disabled:** inspect the session `unavailable_reasons`. `cli_missing` is expected when only a native store remains; do not bypass the flag.
- **A 1.2 control disappeared after reconnect:** verify the catalog producer version. Only confirmed 1.1 daemon catalogs receive legacy send/interrupt inference; an absent/unknown producer fails closed.
- **Queue shows `blocked_failed` or `blocked_interrupted`:** Resume continues only later FIFO items and never replays the blocker. `blocked_indeterminate` requires the explicit Skip action and warning.
- **Start remains `thread_starting`:** the daemon is still waiting for prompt outcome and native-store ownership. Do not resend or fabricate an indeterminate result from a phone timer.

## Related

- [Agent capability matrix](./agent-capability-matrix.md)
