> English | [简体中文](./agent-capability-matrix.zh-CN.md)

# Agent harness capability matrix

Normative **live** matrix of what each supported harness (agent adapter) can do
through NekoNest today, how capabilities are advertised on the wire, and how
that differs from the frozen [v1.0.0 product contract](./v1-product.md).

**Sources of truth (in order when they disagree):**

1. Live wire advertisement (`session.capabilities`, `session_list.start_capabilities`)
2. Daemon implementation (`daemon/internal/adapters`, `daemon/internal/agentexec`,
   capability stamping in `daemon/cmd/daemon/main.go`)
3. This document (operator / contributor summary)
4. [v1-product.md](./v1-product.md) §7.3 (target release bar, not always live)
5. Short tables in [README.md](../README.md) (overview only)

PWA controls **must** gate on advertised flags. Absent fields mean
**false / unsupported**. Never imply a stronger control or attachment tier than
the daemon published for that session or device.

---

## 1. Roles and control modes

| Role | Meaning |
|---|---|
| **Full-control** | Phone can drive the live turn surface when healthy: send/stream, interrupt, approve/deny, steer, full native attachments, and (when probed) native start. **Codex only.** |
| **Compatibility-resume** | Discover + ownership + history + send/stream + interrupt. Attachments and `start_thread` only as advertised. **No** approval / steer / queue promise. |

| `control_mode` | When advertised | Phone expectation |
|---|---|---|
| `app_server` | Codex only, after `codex app-server` initialize succeeds | Full-control flags may be true |
| `exec_resume` | Codex default / unhealthy app-server fallback | Send, history, interrupt; image-oriented attachments; no approve/steer/spawn |
| `compatibility` | Claude Code, Kilo, Kimi CLI, Grok Build | Resume/send path; no full-control promise |

---

## 2. Legend

| Cell | Meaning |
|---|---|
| **Yes** | Implemented and advertised when the path is live |
| **Probe** | Yes only after a successful native starter / health probe |
| **Fallback** | Available on the degraded path |
| **No** | Not advertised; phone must disable the control with a concrete reason |
| **Detect only** | May appear in status/history heuristics; **not** a phone control path |
| **Best-effort** | Materialized files are exposed; agent sandbox/CLI may still refuse reads |

Common capabilities (all five harnesses when the CLI/store is usable):

- Discover main threads from the **native** store
- Positive ownership check before routing
- History import with stable merge ids
- Send prompt + stream normalized assistant output
- Interrupt a daemon-owned running process tree (when a run is active)
- Exclude subagents / sidechains / synthetic-only records from the phone main list where the adapter implements filtering

Shared non-capabilities unless a row says otherwise:

- `queue` is **never** advertised live today
- Arbitrary filesystem browsing and generic `create_session` remain forbidden
- `start_thread` may target only directories in the daemon’s **current union of
  native-discovered project dirs**
- `thread_owned` requires both first-prompt acknowledgement and native-store ownership

---

## 3. Live matrix (v0.2 shipped behavior)

Wire ids: `claude_code`, `codex`, `kilo`, `kimi_cli`, `grok_build`.

### 3.1 Identity and store

| | Claude Code | Codex | Kilo | Kimi CLI | Grok Build |
|---|---|---|---|---|---|
| Wire id | `claude_code` | `codex` | `kilo` | `kimi_cli` | `grok_build` |
| Product label | Claude Code | Codex | Kilo | Kimi CLI | Grok Build |
| Role | Compatibility-resume | Full-control when healthy; exec-resume fallback | Compatibility-resume | Compatibility-resume | Compatibility-resume |
| Native store | `~/.claude/projects/<encoded-path>/*.jsonl` | `~/.codex/sessions/…/rollout-*.jsonl` | Kilo/OpenCode SQLite (`kilo.db` under OS data dir) | `~/.kimi-code` (legacy `~/.kimi`) | `~/.grok/sessions` |
| Resume / send CLI surface | `claude --resume <id> -p … --output-format stream-json` | Healthy: app-server turn APIs. Fallback: `codex exec … resume <id> -- <prompt>` | `kilo run --session <id> …` (JSON format) | `kimi --session <id> -p … --output-format stream-json` (legacy may need `--print`) | `grok --resume <id> -p … --output-format streaming-json --permission-mode auto` |
| Default advertised `control_mode` | `compatibility` | `exec_resume` until app-server healthy → `app_server` | `compatibility` | `compatibility` | `compatibility` |

### 3.2 Session controls (per-session `capabilities`)

Values below are what the daemon **stamps for the phone**. Codex full-control
flags are raised only when `AppServerHealthy()` is true
(`daemon/cmd/daemon/main.go`).

| Capability | Claude Code | Codex (app-server healthy) | Codex (exec-resume / unhealthy) | Kilo | Kimi CLI | Grok Build |
|---|---|---|---|---|---|---|
| Discover / list | Yes | Yes | Yes | Yes | Yes | Yes |
| Ownership gate | Yes | Yes | Yes | Yes | Yes | Yes |
| History | Yes | Yes | Yes | Yes | Yes | Yes |
| Send + stream | Yes | Yes | Yes | Yes | Yes | Yes |
| `interrupt` | Yes | Yes | Yes | Yes | Yes | Yes |
| `approve` / `deny` | **No** (see notes) | **Yes** | **No** | **No** | **No** | **No** |
| `steer` | **No** | **Yes** | **No** | **No** | **No** | **No** |
| `queue` | **No** | **No** | **No** | **No** | **No** | **No** |
| Per-session `spawn` | **No** (device catalog only) | **Yes** when healthy | **No** | **No** (device catalog only) | **No** (device catalog only) | **No** (device catalog only) |
| `attachment_mode` advertised | `path_best_effort` | `native_image_and_file` | `native_image` | `path_best_effort` | `path_best_effort` | `path_best_effort` |
| Status `waiting_approval` | Detect only (history heuristic; phone approve not advertised) | Yes from positive app-server signal | No fake approval UI | No | No | No |
| Status `waiting_user` | No | Yes from positive app-server signal | No | No | No | No |

**Notes**

- Claude / Kilo commander `Approve`/`Deny` exist only as best-effort stdin “y/n”
  when stdin is still open. Print/resume runs close stdin, so the phone path is
  advertised **false** and returns `approval_unavailable` if invoked.
- Kimi / Grok always return `approval_unavailable` for approve/deny.
- Claude may mark `waiting_approval` from transcript shape for list UX; that is
  **not** a promise that phone Approve/Deny works. Prefer PC terminal for those
  CLIs.
- Codex exec-resume can pass image files via `--image` and authorize dirs via
  `--add-dir`; non-image files are not a first-class native file turn input on
  that path (hence `native_image`, not `native_image_and_file`).

### 3.3 Device-level start catalog (`start_capabilities`)

Published on `session_list.payload.start_capabilities`. Each entry:

| Field | Meaning |
|---|---|
| `agent_type` | Wire id |
| `available` | Native start path found and probe succeeded |
| `spawn` | Currently equal to `available` when probe succeeds |
| `reason` | Concrete unavailable text for UI |
| `control_path` / `control_version` | Optional; may be omitted today |

| Harness | Start mechanism (probe) | `spawn` when probe OK | Notes |
|---|---|---|---|
| Claude Code | CLI help must advertise `--session-id` + stream-json; start uses `--session-id` + first `-p` | Probe | Ownership must still be confirmed under `~/.claude/projects` |
| Codex | Requires **healthy app-server** handshake (not bare CLI help) | Probe | `thread/start` then first turn; exec-resume alone does **not** advertise spawn |
| Kilo | CLI help `acp` + ACP start probe (`kilo acp`) | Probe | Start uses ACP; follow-up turns still use resume/`kilo run` |
| Kimi CLI | ACP start probe | Probe | Modern store preferred; legacy `.kimi` still discovered |
| Grok Build | CLI help `--session-id` + `streaming-json`; start uses `--session-id` | Probe | Non-interactive `--permission-mode auto` |

Lifecycle for every harness: phone-local draft → `start_thread` →
`thread_starting` → `thread_owned` | `thread_failed` | `thread_indeterminate`.
Navigate only after `thread_owned`.

### 3.4 Attachment wiring (implementation vs advertisement)

All agents share: phone upload → server blob → daemon per-run temp dir
(max **5** files, **4 MB** each on the open path; see
[configuration.md](./configuration.md)).

| Harness | Advertised mode | How files reach the agent | Practical limits |
|---|---|---|---|
| Claude Code | `path_best_effort` | `--add-dir` on attachment parent dirs; paths appear in the NekoNest prompt suffix. **Does not** use Claude remote `--file` ids | Agent must Read the authorized dir; sandbox may still block |
| Codex app-server | `native_image_and_file` | Turn input carries native image + file parts | Full-control path |
| Codex exec-resume | `native_image` | `--add-dir` for dirs + `--image` for image MIME/ext; other files mainly via prompt paths | Degraded vs app-server |
| Kilo | `path_best_effort` | Native repeated `--file <path>` on `kilo run` | Stronger than the advertised enum name suggests; UI still must not claim `native_image_and_file` until the flag is raised |
| Kimi CLI | `path_best_effort` | Paths only in prompt suffix; attachment slice ignored by argv builder | Depends on Kimi file permissions / sandbox |
| Grok Build | `path_best_effort` | Paths only in prompt suffix; attachment slice ignored by argv builder | Same class of limit; headless auto permission mode |

### 3.5 Filter / hygiene rules (all harnesses)

| Rule | Requirement |
|---|---|
| Subagents / sidechains | Excluded from phone main thread list when detectable |
| Empty history | Not proof of ownership |
| Missing CLI | Non-fatal; other harnesses keep working |
| stderr | Diagnostics only; never assistant text |
| Capability honesty | Absent = false/unsupported; no fake Approve/Steer/Start success |

---

## 4. Per-harness cards

### 4.1 Codex (`codex`) — full-control harness

| Area | Live behavior |
|---|---|
| Healthy path | `codex app-server` JSON-RPC: initialize, thread/turn APIs, approvals, interrupt, steer, attachments |
| Degraded path | `codex exec resume` send/stream/interrupt + `native_image` attachments |
| Capability stamp | When healthy: `control_mode=app_server`, `approve/deny/interrupt/steer/spawn=true`, `attachment_mode=native_image_and_file` |
| Start | Device catalog + per-session `spawn` only while app-server healthy |
| Status | `waiting_approval` / `waiting_user` only from positive app-server signals (plus overlay on discover) |
| Baseline | Development smoke pins **codex-cli 0.144.1** surface; method names can drift—use `nekonest-daemon -doctor` |
| v1 target | Same role; sealed default and always-honest fallback remain release requirements |

### 4.2 Claude Code (`claude_code`)

| Area | Live behavior |
|---|---|
| Control | Compatibility resume only |
| Attachments | Authorize temp dirs (`--add-dir`); paths in prompt |
| Start | Probe `--session-id` path; UUID + first prompt; confirm under projects store |
| Approve | Not advertised; print mode non-interactive |
| Special | May surface transcript-based waiting state; do not treat as phone approval bridge |

### 4.3 Kilo (`kilo`)

| Area | Live behavior |
|---|---|
| Control | Compatibility resume via `kilo run --session` |
| Attachments | Native `--file` wiring; advertised `path_best_effort` today |
| Start | ACP (`kilo acp`) probe + native create; resume path remains for later turns |
| Approve | Not advertised |

### 4.4 Kimi CLI (`kimi_cli`)

| Area | Live behavior |
|---|---|
| Control | Compatibility resume via `kimi --session` |
| Store | Prefer `~/.kimi-code`; still read legacy `~/.kimi` |
| Attachments | Prompt paths only |
| Start | ACP probe when CLI supports it |
| Approve / steer / queue | No |

### 4.5 Grok Build (`grok_build`)

| Area | Live behavior |
|---|---|
| Control | Compatibility resume via `grok --resume` |
| Safety defaults | `--permission-mode auto`, streaming JSON, non-interactive |
| Attachments | Prompt paths only |
| Start | `--session-id` probe + deterministic new id; confirm under `~/.grok/sessions` |
| Approve / steer / queue | No |

---

## 5. Live vs v1.0.0 target

| Topic | Live (document this matrix) | v1 contract ([v1-product.md](./v1-product.md)) |
|---|---|---|
| Codex role | Full-control when app-server healthy | Same; must stay honest on fallback |
| Other four | Compatibility-resume + probed start | Same non-promises for approve/steer/queue |
| Transport default | Open (sealed opt-in preview) | Sealed default for new nests |
| `queue` | Not advertised | SHOULD for Codex when ordering is guaranteed |
| Host OS | Windows + Linux | Same; macOS later |
| Expansion agents | None required | OpenCode / Gemini / Cursor etc. later, not v1 gate |
| Kilo attachment enum | Still `path_best_effort` while using `--file` | Honesty required; raising `native_image` (or a clearer tier) is an implementation follow-up, not a phone guess |

When building toward the v1.0.0 tag, prefer the v1 contract for **scope**. When
operating or debugging a running nest, prefer **this matrix + live flags**.

---

## 6. Wire and UI mapping

| Surface | Fields | Consumer rule |
|---|---|---|
| `AgentSession.capabilities` | `control_mode`, booleans, `attachment_mode` | Gate composer controls per open thread |
| `session_list.start_capabilities` | per-agent `available`/`spawn`/`reason` | Gate “new thread” drafts per agent on a device |
| Messages | `approve`, `deny`, `interrupt`, `steer`, `start_thread`, `send_prompt` | Server relays; daemon enforces real support |
| PWA | `pwa/src/types/protocol.ts`, session store, SessionDetail controls | Disable + explain when flag false |

Schema definitions: [protocol.md](./protocol.md), `protocol/protocol.json`.

---

## 7. Verification checklist

Minimal proof that a harness row is still accurate:

1. Unit/fixture tests under `daemon/internal/adapters` and `daemon/internal/agentexec` for that agent.
2. `DefaultCapabilities` / discover stamping tests (Codex must not claim app-server flags until healthy).
3. Start-capability catalog test: unavailable reason when CLI missing; `spawn=true` only after probe.
4. Operator: `nekonest-daemon -doctor` adapter + Codex app-server lines.
5. Manual: [e2e-smoke.md](./e2e-smoke.md) sections A/B for the agents installed on the host.

---

## 8. Maintenance rules

When changing a harness:

1. Update adapter + agentexec + tests first.
2. Keep `protocol/protocol.json`, server types, and `pwa/src/types/protocol.ts` aligned if the wire shape changes.
3. Update **this file and the Chinese mirror together**.
4. Adjust the short tables in [README.md](../README.md) / [README.zh-CN.md](../README.zh-CN.md) if the one-line summary drifts.
5. If the v1 release bar changes, edit [v1-product.md](./v1-product.md) (+ zh) first, then this matrix’s §5.
6. Do not document a capability the phone cannot see in live `capabilities` / `start_capabilities`.

## Related

- [Architecture](./architecture.md)
- [Protocol](./protocol.md)
- [v1 product contract](./v1-product.md)
- [E2E smoke](./e2e-smoke.md)
- [Troubleshooting](./troubleshooting.md)
- [AGENTS.md](../AGENTS.md)
