> English | [简体中文](./v1-product.zh-CN.md)

# NekoNest v1.0.0 — Product feature contract

**Status:** frozen target product contract for the first complete release  
**Audience:** maintainers, contributors, coding agents  
**Relationship to v0.1:** v0.1.0 was a *launch slice* optimized for shipping. Operator docs under `docs/*.md` (except this file), `README.md` boundaries, and `docs/archive/` describe that slice. **They are reference only.** This document defines what v1.0.0 must be when it ships.

When implementation and this contract disagree during the v1 effort, **update the contract deliberately** — do not silently re-shrink to v0.1 limits. Any change to frozen decisions must update **both** language mirrors before implementation continues.

---

## 1. One-sentence product

NekoNest is a **self-hosted nest** that lets you continue, steer, and finish **real local coding-agent threads** on your home machines from a **phone-first client**, without exposing inbound ports on the home host and without replacing each agent’s native session store. **Codex is the only full-control v1 agent**; Claude Code, Kimi CLI, and Grok Build ship as **compatibility-resume** adapters. Kilo is retired from active support; protocol 1.x retains only legacy parse compatibility for its old wire id.

## 2. Problem and jobs-to-be-done

| Job | Success looks like |
|---|---|
| **Away-from-desk resume** | Open phone → see the same threads that exist on the host → read history → send the next prompt → watch stream |
| **Don’t lose the turn** | Reconnect, app kill, flaky mobile network: prompts are at-most-once; user sees honest delivery state |
| **Unblock without walking to the host** | When Codex waits on the user (permission, question, idle-after-error), phone is notified and can act |
| **Multi-agent life** | One nest surfaces Codex (full control) plus Claude Code, Kimi, and Grok (compatibility resume) without one missing CLI killing the others |
| **Trust the nest** | Operator owns the VPS; **sealed E2E is default** so the relay cannot read application bodies; open relay is admin-only |
| **Home network reality** | Host only dials out; works behind CGNAT / no public IP / no port-forward |

Non-jobs (explicit):

- Not a remote desktop or full IDE
- Not a hosted coding agent that runs in the cloud instead of your CLI
- Not a multi-tenant SaaS control plane for teams of strangers
- Not a tmux/xterm substitute as the primary UX (chat-first; optional terminal escape hatch only if it stays secondary)
- Not equal full-control promises for every supported agent in v1

## 3. Product principles (non-negotiable DNA)

These survive v1 and constrain every feature below.

1. **Native store is authority** for discovery and transcript history, and for ownership after an agent-scoped thread start. Nest SQLite is durability/relay, not a second agent database that diverges forever.
2. **Daemon initiates** the connection to the nest. Home host never requires inbound ports for core product.
3. **Presentation is derived:** `directory → agent → thread`. Orphans land in **未分类 / Uncategorized**. Do not rewrite source session rows to build the tree.
4. **Delivery is not transport:** WebSocket write success ≠ agent acceptance. Keep accepted / committed / failed / not_seen / indeterminate (and phone-visible states derived from them).
5. **Fail closed** on prompt journal ambiguity (prefer visible error over double execution).
6. **Untrusted content rendering:** agent/remote Markdown stays sanitized.
7. **No fake capabilities:** if an agent cannot host an approval, steer, attachment path, or start-thread, the UI must say so — never green-check a no-op. Capability flags default false/unsupported when absent.
8. **One missing agent is non-fatal** for the rest of the nest.
9. **Sealed by default:** new nests use E2E sealed transport; open relay requires explicit administrator configuration; sealed and open clients never mix on one nest; never auto-downgrade sealed to open.
10. **Codex-only full control:** approve/deny, steer, and full attachments are Codex app-server promises. Any of the four active agents may advertise agent-scoped phone `start_thread` only after its native starter is installed and probed; that capability does not promote a compatibility adapter to full control.

## 4. Competitive position (why v1 looks like this)

| Class | Examples | NekoNest stance |
|---|---|---|
| Mobile remote for 1–2 agents | Happy, Remodex | Match **push, approvals, steer, install polish, trust** on Codex; remain multi-agent + self-hosted-first via compatibility adapters |
| Multi-agent relay + IM | Legax, botmux | Match **multi-host + approval callbacks** (Codex); keep chat PWA primary; IM is post-v1 extension |
| Phone terminal / tmux | Pane Remote, run-kit, QuickTUI, chat2ide | Do **not** pivot primary UX to raw PTY; stay structured chat + control |
| Session timeline archive | Longhouse | Borrow **search / multi-machine timeline** ideas; stay lighter |

**v1 wedge:** outbound-only home host + Codex full-control phone loop + multi-agent compatibility resume + honest delivery + sealed-by-default relay.

## 5. Actors and topology

```text
┌──────────────┐     HTTPS/WSS      ┌─────────────────┐
│ Phone client │ ◄────────────────► │ Nest server     │
│ (PWA first)  │                    │ auth · relay    │
└──────────────┘                    │ durability      │
                                    │ push · attach   │
                                    └────────┬────────┘
                                             │ WSS (outbound from each host)
                    ┌────────────────────────┼────────────────────────┐
                    ▼                        ▼                        ▼
             ┌────────────┐           ┌────────────┐           ┌────────────┐
             │ Daemon A   │           │ Daemon B   │           │ Daemon …   │
             │ Windows    │           │ Linux      │           │            │
             └─────┬──────┘           └─────┬──────┘           └─────┬──────┘
                   │                        │                        │
            native CLIs+stores        native CLIs+stores      native CLIs+stores
```

| Actor | v1 responsibility |
|---|---|
| **Phone client** | Pair, browse, chat, control, notifications, drafts/outbox; independent identity/token/keys |
| **Nest server** | Auth, pairing, multi-device registry, phone identities/grants, WS fan-out, sealed durability, attachments (ciphertext in sealed mode), push fan-out |
| **Host daemon** | Discover, history, journal, exec, Codex app-server bridge, process control, health, E2E keys |
| **Agent adapter** | Ownership, list/history normalize, resume/send, stream parse, capability flags |

**Formal host OS (MUST):** Windows and Linux (artifacts: `windows-amd64`, `linux-amd64`, `linux-arm64`; Linux smoke baselines Ubuntu 22.04+ and Debian 12+; XDG paths and `systemd --user`). **macOS is LATER.**

## 6. Release definition: what “v1.0.0” means

v1.0.0 is **feature-complete for a single-operator self-hosted nest** used daily on the go, with **Codex full control** and **four compatibility-resume adapters**, on **Windows and Linux**, with **sealed E2E default**.

### 6.1 Must ship (release blockers)

Everything in §7 marked **MUST**.

### 6.2 Should ship (default in-tree; only drop with written exception)

Everything in §7 marked **SHOULD**.

### 6.3 Explicitly out of v1.0.0

See §11. These must not delay the release.

### 6.4 Quality bar

- Focused + full test suites green for `server/`, `daemon/`, `pwa/`
- Cross-layer protocol checklist complete for any wire change
- Documented deploy paths: VPS + Windows + Linux (amd64 and arm64 artifacts; Ubuntu/Debian smoke)
- E2E smoke checklist updated to this contract (not v0.1 compromises)
- No known “prompt double-send” or “orphan agent process after interrupt” on supported OS
- Security doc matches real trust model (sealed default, open admin-only, metadata visibility, key-loss limits)
- Codex approval, steer, interrupt, start-thread, image, and ordinary-file journeys pass against the pinned app-server baseline

## 7. Feature catalog

Priority tags:

- **MUST** — required for v1.0.0 tag
- **SHOULD** — expected in v1.0.0; omit only with maintainer exception note in CHANGELOG
- **MAY** — allowed if cheap; not a blocker
- **LATER** — designed for, not required in v1.0.0

### 7.0 Frozen decisions (normative summary)

| Area | v1 decision |
|---|---|
| Transport | E2E sealed is a release **MUST** and the **default** for new nests. VPS stores ciphertext only in sealed mode. |
| Open relay | Retained but **disabled by default**. Only a server administrator can enable it. One nest has one fixed mode; sealed/open clients cannot mix; **no automatic downgrade**. |
| Crypto | X25519 key agreement, Ed25519 identity signatures, HKDF-SHA-256 derivation, AES-256-GCM payload encryption. Mature implementations and cross-language vectors only. |
| Pairing | QR trust anchor includes relay URL, device ID, daemon public keys/fingerprint, signed transcript, one-time code, expiry, and protocol major. Six-digit code is fallback only and requires manual fingerprint comparison. |
| Multi-phone | Each phone has an **independent** identity, token, and pair wrapping key. Daemon wraps device/session keys for each authorized phone. |
| Revocation | Revoke token, WS, push, grants, and future key packages immediately; rotate to a new key epoch. Previously downloaded/decrypted history cannot be remotely erased. |
| Key recovery | Phone private keys are **never** backed up to VPS. Lost/cleared PWA state → re-pair as a **new** identity. Reconstruct available history from the native agent store; VPS-only old plaintext/ciphertext is not recoverable without the old key. |
| VPS metadata | VPS **may** see device ID, native session ID, timestamps, client message IDs, event classes, sizes, and connection metadata. Agent type, project path, title, prompt/response/tool bodies, approval details, and attachment bytes/metadata remain **encrypted** in sealed mode. |
| Phone auth | `NEKONEST_ADMIN_SECRET` is the administrator bootstrap credential (`NEKONEST_PHONE_SECRET` is a one-release deprecated alias when unset). General REST/WS uses independent revocable phone tokens scoped by device grants. |
| Migration | One explicit breaking v0.1→v1 migration; no long-term v0/v1 mixed protocol. Preserve daemon device IDs/token hashes and native stores. After verified backup, clear old VPS plaintext messages, prompts, pair codes, and attachments. Phones re-login/re-pair. |
| Protocol compatibility | Version is `major.minor`. Major mismatch rejects. Minor is backward compatible: unknown optional fields ignored; absent capabilities default false/unsupported. E2E, identity, or delivery semantic breaks require a major bump. Never downgrade sealed to open. |
| Formal host OS | **Windows + Linux**. macOS later. |
| Primary agent | **Codex** is the only full-control v1 agent. Normative path: `codex app-server` JSON-RPC; the minimum full-control baseline is **codex-cli 0.146.0**, with live schema/initialize probing. |
| Codex controls | Send, structured user input, approve/deny, interrupt, steer, and a durable FIFO follow-up queue are implemented full-control surfaces. Legacy `codex exec resume` is a capability-degraded compatibility path. |
| Start thread | **Agent-scoped, capability-gated.** The phone opens a local-only draft; it creates the native thread only when the first prompt is sent. An installed/probed starter may target only a directory in the daemon's **current union of native-discovered project directories**. No arbitrary path entry or filesystem browsing. Lifecycle: `starting → owned \| failed \| indeterminate` (no permanent ghost row); `owned` requires positive first-prompt acknowledgement and native-store ownership. |
| Other agents | Claude Code, Kimi CLI, and Grok Build remain **compatibility resume** adapters. Each control is independently raised only from a live native probe/event. A reliable send path may use the NekoNest durable FIFO; this is not an agent-native queue and does not grant steer or full control. |
| Attachments | Codex app-server **MUST** support images and ordinary files end-to-end. Other adapters advertise `native_image`, `path_best_effort`, or `unsupported`; UI never implies a stronger tier. |
| Notifications | Codex waiting for approval, waiting for user input, and run failure are **MUST**. Success and device offline are **SHOULD**. Sealed push contains only generic event text plus device/session references; details decrypt after opening the PWA. |
| Expansion agents | **No** v1 release gate for additional agents beyond the four active wire ids. The retired `kilo` id is parse-only compatibility. |

### 7.1 Nest server

| ID | Feature | Priority | Acceptance |
|---|---|---|---|
| S1 | Phone authentication via independent revocable phone tokens; admin bootstrap secret for mint/pair admin paths | MUST | Public bind refused without auth; loopback-only unauthenticated dev mode preserved; `NEKONEST_ADMIN_SECRET` (+ one-release `NEKONEST_PHONE_SECRET` alias) |
| S2 | Daemon bootstrap registration + long-lived device token | MUST | Bootstrap ≠ phone token; public mode disables open register |
| S3 | Pairing (QR primary; short-lived code fallback with fingerprint compare) | MUST | Single-use, TTL; phone binds device without re-sharing bootstrap; signed transcript |
| S4 | Multi-device registry (many hosts per nest) | MUST | Online/offline, last_seen, display name, OS (`windows` / `linux` formal) |
| S5 | Multi-phone subscribers per device with independent identities/tokens/grants | MUST | Two phones follow same device; revoke one without affecting the other |
| S6 | WebSocket relay with subscribe_ack gating + transport mode / protocol major.minor negotiation | MUST | No session traffic treated ready before ack; generation-safe daemon reconnect; reject major or mode mismatch |
| S7 | Durable sealed (or open-mode plaintext) messages + prompt command journal | MUST | Persistence failure visible; no false business ack before required write; sealed mode stores ciphertext only |
| S8 | Attachment upload/download with size/MIME limits | MUST | Auth required; sealed mode stores ciphertext blobs + non-sensitive metadata only |
| S9 | Web Push (VAPID) for actionable events | MUST | MUST events: waiting_approval, waiting_user, run failure. SHOULD: success, device offline. Sealed push: generic text + device/session refs only |
| S10 | Origin allowlist + trusted-proxy IP model | MUST | Defaults safe; documented |
| S11 | Rate limits / body & frame size caps | MUST | Abuse controls remain |
| S12 | Health endpoint + structured operator logs (no secrets) | MUST | |
| S13 | **E2E sealed channel** phone↔daemon (relay sees ciphertext + documented routing metadata only) | MUST | **Default for new nests.** Open relay admin-only, explicit, no auto-fallback |
| S14 | Static PWA hosting from server binary/dist | SHOULD | Same as today |
| S15 | Phone revoke / device rename / forget; phone identity list | MUST (phone revoke); SHOULD (device rename/forget) | Stolen phone or retired host recoverable |
| S16 | Attachment retention policy (TTL / max volume) | SHOULD | Operator-configurable |
| S17 | Metrics/debug endpoint (auth-gated) | MAY | |
| S18 | Explicit v0.1→v1 offline migration (`schema_meta`, backup, clear plaintext) | MUST | Idempotent; preserves daemon device IDs/token hashes; phones re-pair |
| S19 | Key packages for authorized phones (wrapped device-catalog / session keys) | MUST | With S13; epoch rotation on revoke |

### 7.2 Host daemon

| ID | Feature | Priority | Acceptance |
|---|---|---|---|
| D1 | Outbound reconnecting WSS with backoff + generation | MUST | |
| D2 | Single-instance lock per config identity | MUST | On every supported OS |
| D3 | **Windows + Linux** first-class hosts | MUST | Same product paths; OS-specific process kill correct on each. macOS LATER |
| D4 | Adapter registry; missing CLI non-fatal | MUST | |
| D5 | Completion-based 30s discover + ownership routing | MUST | Phone-visible list is the last 7 days by activity; running/waiting threads override age; empty transcript ≠ ownership |
| D6 | History import with stable ids; exclude subagents/sidechains/primers | MUST | |
| D7 | Headless resume/send via each agent’s supported path | MUST | Codex prefers app-server; others CLI resume |
| D8 | Prompt journal fail-closed; at-most-once with `client_msg_id` | MUST | Exact sealed envelope retry; never re-encrypt as same command |
| D9 | Stream normalize; stderr = diagnostics only | MUST | |
| D10 | Interrupt / stop process tree | MUST | No orphan trees on supported OS (Windows Job Objects; Linux process groups); delayed commands cannot interrupt a newer generation |
| D11 | Attachment materialize to per-run temp + agent wiring | MUST | Capability matrix honest per agent; Codex full image+file |
| D12 | **Native approval bridge for Codex app-server** | MUST | Phone approve/deny reaches real callback; non-Codex advertise unavailable; never fake |
| D13 | Session status machine: `idle` / `running` / `waiting_user` / `waiting_approval` / `error` | MUST | `waiting_*` only on positive app-server signal; unsupported adapters stay running/idle/error |
| D14 | Capability advertisement per agent/session | MUST | Protocol 1.2 flags: send, approve, deny, interrupt, steer, queue, spawn, user_input, attachment_mode, control path/version, and stable unavailable reasons; absent = false/unsupported |
| D15 | Register + pair generate CLI; **doctor** diagnostics | MUST | Checks protocol/mode, auth, keys, server, adapters, Codex app-server methods/version, writable state, process control |
| D16 | Autostart packages (Windows service/Task; Linux `systemd --user`) | SHOULD | Documented one-liners; macOS launchd LATER |
| D17 | Config validate + safe reload of non-identity fields | SHOULD | |
| D18 | Optional local HTTP debug (loopback) | MAY | |
| D19 | Daemon-side encryption keys for E2E (S13) | MUST | With S13; restrictive permissions; XDG on Linux |
| D20 | Agent-scoped `start_thread` on first prompt | MUST | Start only when the selected agent's native starter is installed/probed and `spawn=true`; target only the daemon's current union of discovered native project dirs. Journal: starting → owned \| failed \| indeterminate; positive prompt acknowledgement and native ownership required before owned |
| D21 | Steer active Codex turns | MUST | Capability-gated; non-Codex false |
| D22 | Publish encrypted device catalog (session list, discovered roots, capabilities) | MUST | With S13 |

### 7.3 Agent adapters (v1 matrix)

Wire ids remain stable; adding an agent is a full-stack change (schema, server, daemon, PWA, tests, docs).

Detailed **live** vs target cards for each harness: [agent-capability-matrix.md](./agent-capability-matrix.md). This §7.3 remains the frozen v1 release bar.

#### 7.3.1 Full-control agent (MUST)

| Wire id | Product | Role | Guarantees |
|---|---|---|---|
| `codex` | Codex | **Only full-control v1 agent** | Discover, ownership, history, send/stream, interrupt, **approve/deny**, structured user input, **steer**, durable FIFO queue, supervised recovery, and **attachments `native_image_and_file`** via `codex app-server`. May advertise **spawn/`start_thread`** only when its native starter is installed/probed. Legacy `codex exec resume` is degraded compatibility only (advertise real capabilities). Exclude subagents from main list. Minimum full-control CLI: **0.146.0**. |

#### 7.3.2 Compatibility-resume agents (MUST)

| Wire id | Product | Guarantees | Explicit non-promises |
|---|---|---|---|
| `claude_code` | Claude Code | Discover, ownership, history, send/stream, process interrupt, path attachments, NekoNest FIFO, and probed start | Approval/user-input/native image require a packaged bridge signal; steer remains false |
| `kimi_cli` | Kimi CLI | Same; modern store; legacy path documented; agent-scoped start only when advertised | Same non-promises |
| `grok_build` | Grok Build | Same; safe non-interactive defaults; agent-scoped start only when advertised | Same non-promises |

Attachment tiers for non-Codex: `native_image` \| `path_best_effort` \| `unsupported`. UI must never imply a stronger tier than advertised.

#### 7.3.3 Expansion agents

**LATER / no v1 gate.** OpenCode, Gemini CLI, Cursor Agent, and other CLIs are not required for the v1.0.0 tag. No “at least two expansion agents” release requirement.

#### 7.3.4 Adapter conformance

**Full-control (Codex) MUST** document and test ownership, list filtering, history stability, app-server send/stream, interrupt, attachments (image + file), approval, steer, start-thread lifecycle, status detection from positive signals, crash/restart, and fixture corpus.

**Compatibility-resume agents MUST** document and test ownership, list filtering, history stability, resume/send argv, stream mapping, interrupt, attachment strategy + failure modes, honest capability flags (approval/steer/queue false unless truly implemented; start false unless a native starter is installed/probed), and fixture corpus. They must **not** report `waiting_approval` / `waiting_user` by inference.

### 7.4 Phone client (PWA-first)

| ID | Feature | Priority | Acceptance |
|---|---|---|---|
| P1 | Installable PWA (iOS/Android/desktop browsers) | MUST | Offline shell; no duplicate WS handlers on route change |
| P2 | Setup: nest URL + admin/bootstrap then independent phone identity; clear error if nest unreachable | MUST | |
| P3 | Pair device (QR scan primary; code + fingerprint fallback) | MUST | |
| P4 | Device list with online/offline and thread counts | MUST | Decrypt device catalog in sealed mode |
| P5 | Workspace: directory → agent → thread tree | MUST | Collapse, sort, per-thread or whole-project archive, manual order |
| P6 | Local search across threads (summary, path, agent) | MUST | After local decrypt |
| P7 | Session chat: history merge + live stream + stable ids | MUST | No duplicate turns on reconnect |
| P8 | Composer: send, drafts, attachments, busy lock | MUST | Attachment UI tiered by capability |
| P9 | Outbox with `client_msg_id`, retry, cap, reconnect replay | MUST | Same sealed envelope on retry; clear only on committed |
| P10 | **Delivery UX** for accepted / committed / failed / not_seen / indeterminate | MUST | User-visible; not only transport send or deprecated `prompt_sent` |
| P11 | Interrupt control | MUST | Disabled when capability false or no exact active-turn binding is advertised |
| P12 | **Approve / Deny** UI when `waiting_approval` | MUST | Only after a positive live permission request and advertised capability; otherwise disabled with a reason |
| P13 | Status badges: running / waiting you / waiting approval / error | MUST | |
| P14 | Push opt-in + deep link into the needing session | MUST | With S9; generic sealed payload; decrypt details in PWA |
| P15 | i18n zh-CN + en | MUST | |
| P16 | Theme light / dark / system | MUST | |
| P17 | Sanitized Markdown + code blocks | MUST | |
| P18 | Touch targets ~44px; safe-area; reduced-motion respect | MUST | |
| P19 | Onboarding that matches real install commands | MUST | OS-specific copy (Windows + Linux) |
| P20 | Session prefs persist (thread/project archive, collapse, sort) | SHOULD | |
| P21 | Image capture from camera roll | SHOULD | Within attachment limits; Codex full path |
| P22 | Voice input → text (OS speech or browser API) | SHOULD | No cloud STT required |
| P23 | Prompt queue while running (when capability allows) | SHOULD | NekoNest durable FIFO on any reliable advertised path; blockers require resume or explicit indeterminate skip |
| P24 | Steer / follow-up without full interrupt | MUST when Codex app-server capability true; UI gated | Non-Codex disabled with reason |
| P25 | Device management + phone identity revoke | SHOULD / MUST for own-token revoke | With S15 |
| P26 | Offline banner + last-sync time | SHOULD | |
| P27 | Accessibility: focus order, labels, contrast | SHOULD | |
| P28 | Optional secondary “raw log” view for power users | MAY | Not primary UX |
| P29 | Agent-scoped start-thread UX over discovered directories | MUST | First open a phone-local draft; send its first prompt to create natively only when `spawn=true`. Navigate only after `thread_owned`; show recovery on indeterminate |
| P30 | IndexedDB identity/key store; key-loss re-pair as new identity | MUST | No server recovery of old phone private keys |

### 7.5 Thread lifecycle (product decision for v1)

| ID | Feature | Priority | Rules |
|---|---|---|---|
| L1 | Resume existing native threads | MUST | Core path for all four active agents; native id preserved. The phone catalog is a fixed 7-day recent view; hiding never deletes native data. Old-only projects disappear from the picker and reappear after host-side activity. An old deep link shows hidden/removed state and does not scan history. |
| L2 | **Start thread from phone** | MUST | Start with an **agent-scoped phone-local draft**. Its first prompt invokes the selected installed/probed native starter so that agent's **native store gains the thread** |
| L3 | Directory picker limited to the daemon's **current union of discovered** native project directories | MUST | No arbitrary filesystem walk; no operator path typing; reject vanished dirs, `..`, symlink escape |
| L4 | Refuse start when the selected agent's native starter is missing/unprobed, `spawn=false`, or directory is not in the current discovered union | MUST | Clear error → `thread_failed` before spawn when possible |
| L5 | Start lifecycle states | MUST | `thread_starting` → `thread_owned` \| `thread_failed` \| `thread_indeterminate`. Set `thread_owned` only after positive first-prompt acknowledgement and ownership in the selected agent's native store; otherwise use `thread_indeterminate`. Fail-closed journal by device + operation id. No auto-retry after indeterminate. **No permanent nest-only ghost row.** |
| L6 | No cross-agent “fake migrate” of transcripts | MUST | Handoff between agents is LATER unless native tools support it |
| L7 | Archive/hide thread in phone view only | SHOULD | Does not delete native store |
| L8 | Delete/kill native thread from phone | LATER | Too destructive for v1 default |
| L9 | Generic `create_session` / nest-invented sessions | Forbidden | Do not reintroduce |

**Invariant:** after L2 success (`thread_owned`), discovery must find the thread from the selected agent's native store; phone never keeps a permanent thread that the host cannot own. A phone-local draft is not a nest session. Indeterminate starts reconcile via future discovery only.

### 7.6 Control plane beyond chat

| ID | Feature | Priority | Acceptance |
|---|---|---|---|
| C1 | Interrupt running work | MUST | All agents that advertise interrupt |
| C2 | Approve / deny tool prompts | MUST | **Codex app-server real bridge**; others honest unavailable |
| C3 | User-input / question prompts when agent surfaces them | MUST for Codex positive signals; SHOULD channel | Same status channel as approvals |
| C4 | Prompt queue | SHOULD | NekoNest durable FIFO on any reliable queue-capable path; never presented as an agent-native queue |
| C5 | Steer active turn | MUST | Codex app-server; capability-gated |
| C6 | Read-only git snapshot (branch, dirty, short status) | SHOULD | No auto-commit from nest without explicit user action |
| C7 | Explicit git actions (commit/push) from phone | LATER | High risk; not v1 blocker |
| C8 | Worktree / multi-agent orchestration UI | LATER | Pane territory |
| C9 | IM bridges (Telegram / Feishu / Discord) | LATER | Protocol should not forbid; not in v1 client |

### 7.7 Notifications

| ID | Event | Priority | Notes |
|---|---|---|---|
| N1 | `waiting_approval` (Codex) | MUST | Generic sealed push + deep link |
| N2 | `waiting_user` (Codex needs-you / user input) | MUST | Same |
| N3 | Run completed successfully after background | SHOULD | Non-blocking |
| N4 | Run failed / crash | MUST | |
| N5 | Device went offline while subscribed | SHOULD | Non-blocking |
| N6 | Quiet hours / per-device mute | SHOULD | |

Push payload must deep-link to `device + session` without embedding secrets, prompts, approval details, or paths. Details decrypt only after opening the PWA.

Journey split (replaces kitchen-sink single story):

1. **Delivery / interrupt** — send, accepted→committed, reconnect integrity, interrupt mid-run  
2. **Push / deep-link** — generic attention event → open route → decrypt details  
3. **Codex approval** — real blocked request → approve/deny → continuation  

### 7.8 Security & privacy

| ID | Feature | Priority | Notes |
|---|---|---|---|
| X1 | Separate admin bootstrap secret, daemon bootstrap, and independent phone tokens | MUST | |
| X2 | No secret logging; no secret in git | MUST | |
| X3 | Public mode requires auth + bootstrap | MUST | |
| X4 | Attachment allowlist + size caps (plaintext policy on ends; ciphertext caps on server) | MUST | |
| X5 | **E2E sealed mode default** (S13) | MUST | Documented metadata still visible (see §7.0) |
| X6 | Open-relay mode for trusted private VPS | MUST | Admin-only enable; nest fixed mode; README warning; no mix/fallback |
| X7 | Pairing QR does not print long-lived tokens in plain logs | MUST | |
| X8 | Phone revoke + key epoch rotation; device token rotation / re-pair | MUST (phone revoke); SHOULD (device rotation) | |
| X9 | Optional phone lock (PIN/biometrics) on client | MAY | Local UX only |
| X10 | Multi-user nest ACLs | LATER | Single-operator v1 |
| X11 | Full zero-knowledge nest (hide even metadata) | LATER | |
| X12 | Key-loss policy: re-pair as new identity; native-store rebuild; no VPS private-key backup | MUST | Document unrecoverable VPS-only history |
| X13 | Authenticated AAD on sealed envelopes; unique nonce + monotonic sequence | MUST | Reject duplicate/out-of-window sequences |

### 7.9 Install, ops, docs

| ID | Feature | Priority |
|---|---|---|
| O1 | One documented VPS path (binary + reverse proxy + TLS) | MUST |
| O2 | Daemon install per formal OS (Windows + Linux) | MUST |
| O3 | `doctor` on daemon + nest health checks | MUST |
| O4 | e2e-smoke checklist aligned to this contract | MUST |
| O5 | Bilingual README + operator docs updated for v1 at release | MUST |
| O6 | Protocol change checklist still enforced | MUST |
| O7 | Release artifacts (server; daemon windows-amd64, linux-amd64, linux-arm64; PWA dist notes) | MUST |
| O8 | Migration guide v0.1 → v1.0 (backup, schema, re-pair, E2E keys, admin secret rename) | MUST |
| O9 | In-app version / support diagnostics bundle (redacted) | SHOULD |
| O10 | Linux `systemd --user` unit path | SHOULD |
| O11 | Document pinned Codex baseline and capability fallback | MUST |

### 7.10 Protocol (wire)

| ID | Feature | Priority |
|---|---|---|
| W1 | Keep envelope model; manual multi-surface sync | MUST |
| W2 | `protocol_version` major.minor negotiation; reject major mismatch | MUST |
| W3 | `transport_mode` sealed \| open; reject mismatch; no sealed→open fallback | MUST |
| W4 | Phone handles full prompt lifecycle types; deprecate `prompt_sent` | MUST |
| W5 | Session status: `idle` \| `running` \| `waiting_user` \| `waiting_approval` \| `error` | MUST |
| W6 | Capability flags (absent = false/unsupported) | MUST |
| W7 | Agent-scoped `start_thread` / `thread_starting` / `thread_owned` / `thread_failed` / `thread_indeterminate` | MUST |
| W8 | Approval payloads with stable approval id; `steer` | MUST |
| W9 | E2E control + `sealed_payload`; pair/key messages (`pair_*`, `key_package`, `phone_revoked`) | MUST |
| W10 | Generic `attention_event` for push-driving classes | MUST |
| W11 | IM/webhook extension points | LATER |

Any W* change follows the existing cross-layer checklist (`protocol.json`, Go, TS, daemon, tests, docs).

## 8. End-to-end user journeys (v1 acceptance stories)

### J1 — First-time setup (Windows or Linux)

1. Operator deploys nest with TLS, admin secret, bootstrap token; transport mode sealed (default).  
2. Installs daemon on home host (Windows or Linux), registers identity keys, runs doctor (green).  
3. Opens PWA, establishes phone identity, pairs via QR (or code + fingerprint).  
4. Sees device online and decrypted session list **or** an empty state with an agent picker that offers “start in project” only for agents whose native starter is installed/probed and whose discovered-directory union is non-empty.

### J2a — Delivery and interrupt

1. User opens existing Codex (or compatibility) thread; history matches native gist.  
2. Sends prompt; delivery shows accepted→committed (and not_seen/failed/indeterminate when applicable).  
3. Stream renders; interrupt works mid-run when capability true; no orphan process tree.

### J2b — Push and deep-link

1. App backgrounded while subscribed.  
2. Generic attention push arrives (no secrets/bodies).  
3. Open deep link to device+session; PWA fetches and decrypts details.

### J2c — Codex approval

1. Trigger a real Codex approval; status `waiting_approval`.  
2. Receive generic push; deep-link; decrypt approval details.  
3. Approve or deny; observe continuation or honest failure.

### J3 — Start an agent-scoped thread from phone

1. User picks a supported agent with `spawn=true` and a directory from the daemon's **current union of discovered** native project dirs; the PWA opens a local-only draft.
2. Before sending, the phone durably binds the draft to one operation id (and seals the whole command under the device-catalog key in sealed mode). Reload never mints a replacement operation. The daemon starts the selected native CLI/app-server and emits starting → owned only after positive first-prompt acknowledgement and native-store ownership; otherwise failed / indeterminate.
3. On owned, phone navigates to the thread under correct directory/agent; PC CLI sees the same native thread and the accepted first prompt may be synthesized locally. Indeterminate keeps the draft recoverable.
4. Forged or vanished paths are rejected; unresolved starts cannot be retried or double-spawned.

### J4 — Multi-host

1. Two daemons paired to one nest.  
2. Phone switches devices; subscriptions isolated; no crossed prompts.

### J5 — Reconnect integrity

1. Kill phone network during send; outbox retains `client_msg_id` and exact sealed envelope.  
2. Restore network; no double agent run; UI converges to single user turn.

### J6 — Sealed mode

1. Default sealed nest; confirm nest DB/logs/attachment store lack plaintext bodies/filenames/approval details (spot-check).  
2. Chat, approvals, and start-thread still function after local decrypt.

### J7 — Multi-phone and revoke

1. Pair phones A and B; both decrypt shared history under current epochs.  
2. Revoke A; A loses WS/token/push/grants; epoch rotates; B continues with new packages.  
3. Previously decrypted local history on A is not remotely erasable (documented).

### J8 — Key loss

1. Clear PWA state; re-pair as new identity.  
2. Rebuild available history from native agent store; clearly omit unrecoverable VPS-only history.

### J9 — Open mode (admin)

1. Administrator enables open mode on server; daemon and phone explicitly opt in.  
2. Sealed-only client cannot connect; no automatic fallback.

### J10 — Migration

1. Seed v0.1 DB/files; verified backup; offline migrate.  
2. Daemon token still authenticates; old plaintext content gone; phones re-login/re-pair.

## 9. UX information architecture

```text
Setup
  └─ Nest URL + admin/bootstrap → phone identity (+ sealed keys)

Home
  └─ Devices[]
        └─ Workspace
              ├─ Search
              ├─ Directory
              │     └─ Agent
              │           └─ Thread list (status badges)
              ├─ Start agent thread (capability-gated; discovered dirs only)
              └─ Device settings / doctor summary / revoke

Thread
  ├─ Header: agent, path, status, interrupt
  ├─ Timeline: messages, tools, approvals
  ├─ Approval / user-input card (when pending; Codex)
  ├─ Steer control (when capability true)
  └─ Composer: text, attach (tiered), queue indicator, send
```

## 10. Non-functional requirements

| Area | v1 target |
|---|---|
| Prompt ack visibility | User sees terminal failure within seconds of daemon decision |
| History first paint | Usable window (order of tens of messages) without blocking forever on full native import |
| Discover cadence | New host threads appear on phone within ~30s under normal load; periodic scans wait 30s after the prior scan completes, while reconnect/control/start events may force an immediate scan |
| Mobile battery | No hot-loop polling; WS + push |
| Binary size / deps | Server and daemon remain small Go deployments; PWA standard pnpm build |
| Concurrency | One writer per WS; no lock across slow IO (existing engineering invariant) |
| Windows process hygiene | Job Object or equivalent; interrupt cleans children |
| Linux process hygiene | Process group SIGINT then bounded SIGKILL; no orphans |
| iOS PWA limits | Document push/PWA constraints; graceful degrade |
| Protocol compatibility | major.minor as in §7.0 |

## 11. Out of scope for v1.0.0

Do not block v1 on:

- macOS formal support (LATER; Windows + Linux only for v1 tag)
- Expansion agents beyond the four active wire ids (no ≥2 expansion gate)
- Equal full-control (approval/steer/start/full attachments) for non-Codex agents
- Native App Store iOS/Android apps (PWA is primary; native MAY later)
- Feishu / Telegram / Slack / Discord as primary clients
- Multi-tenant RBAC, orgs, SSO
- Cloud-hosted multi-tenant SaaS offering
- Full git write UX, PR merge cockpit
- Worktree fleet orchestration (Pane-class)
- Primary raw terminal emulator product
- Running agents on the VPS instead of the home host
- Automatic cross-agent transcript migration
- Guaranteed support for every long-tail CLI worldwide
- End-to-end zero visibility of metadata to nest
- Collaborative multi-human editing of one thread with CRDT semantics
- Server-side backup of phone private keys
- Remote erasure of already-decrypted phone history
- Long-term mixed v0/v1 protocol on one nest

## 12. Mapping: v0.1 → v1.0 (delta summary)

| Area | v0.1 (launch slice) | v1.0.0 contract |
|---|---|---|
| Host OS | Windows product | **Windows + Linux**; macOS later |
| Create thread | Forbidden on phone | Agent-scoped local draft → first-prompt `start_thread` via installed/probed native starter, into the daemon's **current union of discovered** dirs; no generic `create_session` |
| Approvals | Wire mostly; UI display-only; CLI often unavailable | **Codex real bridge** + UI; others honest unavailable |
| Steer | Not productized | **Codex MUST** when app-server capable |
| Delivery UX | Mostly `prompt_sent` / failed | Full lifecycle including not_seen / indeterminate |
| Status | running / idle / waiting_approval | + waiting_user / error; positive-signal only |
| Push | Optional if VAPID | Required path for MUST attention events; generic sealed payloads |
| Trust | Plaintext on VPS | **Sealed E2E default** + admin-only open relay |
| Agents | 5 fixed, equal aspiration | **Codex full-control** + **4 compatibility-resume**; no expansion gate |
| Attachments | Best-effort | Codex image+file MUST; others tiered advertisement |
| Auth | Shared phone secret | Admin secret + **independent phone tokens/grants/revoke** |
| Install | Manual build-heavy | doctor + autostart + Windows/Linux docs |
| Pairing | 6-digit code | QR primary + code/fingerprint fallback |
| Capabilities | Implicit | Advertised flags; default false |
| Multi-host | Possible registry | First-class journey |
| Protocol | v0 envelope | major.minor + transport_mode |
| Docs | Describe compromises as “boundaries” | This contract is the v1 product truth |

## 13. Suggested implementation waves (not separate products)

Waves are delivery order inside the v1 effort; the tag still requires MUST complete. Align with the Codex-first E2E implementation plan.

| Wave | Focus | Exits when |
|---|---|---|
| **W0 — Contract** | This document frozen; AGENTS.md rules for agent-scoped first-prompt start | EN/ZH parity; no open release-blocking decisions |
| **W1 — Protocol scaffolding** | Envelope, status, capabilities, start/steer/pair/key messages, major.minor + mode | Old clients fail clearly; v1 peers negotiate first |
| **W2 — Server migration + phone identities** | schema_meta, migrate-v1, admin secret, grants, revoke | Two phones independent; revoke immediate |
| **W3 — Crypto + pairing** | Vectors, QR, key packages, epochs | Relay cannot substitute keys; second phone pairs |
| **W4 — Sealed relay durability** | Opaque payloads, sealed attachments, generic attention push | No plaintext application content on sealed server |
| **W5 — Capabilities + control UX** | Flags, delivery states, gated controls | No control without capability |
| **W6 — Codex app-server full control** | approve/deny/interrupt/steer/attachments | J2a–J2c on pinned baseline |
| **W7 — Agent-scoped start-thread** | Local draft, discovered-directory union, journal lifecycle | J3; native ownership |
| **W8 — Windows + Linux formal** | paths, process groups, artifacts, doctor, systemd | J1 on both OS baselines |
| **W9 — Release docs + gates** | Migration, security model, CHANGELOG, smoke | v1.0.0 tag |

## 14. Closed decisions (frozen)

All former open items are closed. Resolutions:

| # | Topic | Resolution |
|---|---|---|
| 1 | **E2E default** | Sealed-by-default for new nests. Open relay retained, disabled by default, **admin-only** enable. One nest one mode; no mix; no auto-downgrade. |
| 2 | **Start-thread UX** | Local-only draft first; the first prompt uses the selected agent's installed/probed native starter. Target **only** directories in the daemon's **current union of discovered** native project dirs. No arbitrary path entry or separate operator allowlist file required for v1. |
| 3 | **Auth evolution** | Admin bootstrap secret (`NEKONEST_ADMIN_SECRET`, one-release `NEKONEST_PHONE_SECRET` alias). General access: **independent revocable phone tokens** + device grants. Multi-phone keys wrapped per phone. |
| 4 | **Codex approval transport** | Normative: **`codex app-server` JSON-RPC** on **codex-cli 0.146.0+**, with schema/initialize probing. Legacy `exec resume` is degraded only. Claude Code and others: no approval promise in v1. |
| 5 | **Expansion agents** | **None required** for v1.0.0. No “at least two” gate. |
| 6 | **Message type names** | Stabilize in implementation against plan catalog: `start_thread`, `thread_starting`, `thread_owned`, `thread_failed`, `thread_indeterminate`, `steer`, `pair_request`, `pair_confirm`, `pair_ready`, `pair_failed`, `key_package`, `phone_revoked`, `attention_event`; status includes `waiting_user`. Exact schema lands in Phase 1 `protocol.json`. |
| 7 | **Formal OS** | Windows + Linux MUST; macOS LATER. |
| 8 | **Agent roles** | Codex full-control; three others compatibility-resume only. All four active agents may advertise agent-scoped start only after native-starter install/probe; this grants no other control capability. Kilo is retired from active catalogs. |
| 9 | **Start lifecycle** | `starting → owned \| failed \| indeterminate`; native ownership before owned; no permanent ghost row. |
| 10 | **Notifications** | MUST: waiting_approval, waiting_user, run failure. SHOULD: success, device offline. |
| 11 | **Protocol versioning** | `major.minor` as in §7.0. |
| 12 | **Migration / key loss / metadata** | As in §7.0 table. |

Changing any row requires updating this file and `v1-product.zh-CN.md` before further implementation.

## 15. Document control

| | |
|---|---|
| **Canonical product contract for v1.0.0** | This file + `v1-product.zh-CN.md` |
| **Live engineering invariants** | [AGENTS.md](../AGENTS.md) (includes agent-scoped first-prompt `start_thread`; forbids generic `create_session`) |
| **v0.1 operator docs** | `docs/*.md` until rewritten at release — **reference only** during v1 build |
| **Shipped root README** | Describes **live v0.1** behavior until release rewrite — do not treat as v1 contract |
| **Frozen history** | `docs/archive/` — never treat as contract |
| **User-visible history** | [CHANGELOG.md](../CHANGELOG.md) |

### Change process

1. Edit this contract first for product-scope changes.  
2. Mirror Simplified Chinese with parity.  
3. Update `AGENTS.md` when invariants change.  
4. Implement with tests.  
5. At v1.0.0 release: rewrite README + operator docs to match this contract; move obsolete “v0.1 boundaries” language out of the live README.

---

*NekoNest v1.0.0 is complete when a solo operator can deploy a sealed-by-default nest, connect home machines on Windows and Linux, resume native threads for four active agents, fully control Codex (notify → approve/steer/interrupt/start-in-discovered-dir → done) with honest compatibility resume for the other three, trust delivery and sealed transport within the documented metadata limits — without turning the product into a remote IDE, cloud agent, or equal full-control promise for every CLI.*
