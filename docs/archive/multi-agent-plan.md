# Implementation Plan: Multi-Agent Neko Paradise

## Overview

Upgrade NekoNest from a three-agent session list into a local multi-agent
workspace navigator. Add Kimi CLI and Grok Build discovery/resume support,
remove remote thread creation, group sessions as
`directory -> agent -> thread`, and rebuild the PWA around an original
cel-shaded catgirl brand system with a duo logo and one differentiated avatar
per agent.

## Architecture Decisions

- Agent sessions remain authoritative in each CLI's local store. NekoNest only
  discovers, resumes, streams, and imports them.
- New Kimi and Grok wire session IDs are agent-qualified so UUID collisions
  cannot overwrite another agent's cached session.
- `project_dir` is the directory identity. Missing directories join one
  `未分类` project; an agent node exists only when that project has at
  least one session for it.
- The PWA builds the three-level tree as a pure derived view and never rewrites
  the session store.
- Remote thread creation is removed across PWA, server, daemon, and protocol.
  Old PWA URLs redirect to the device workspace.
- Agent presentation data lives in one frontend catalog with stable final
  asset paths. Original DragTokens artwork ships at those paths, while a
  reproducible Pillow builder derives the PWA icons and notification badge.
- Existing dirty-worktree changes are preserved; overlapping adapter and
  SessionDetail edits are merged rather than replaced.

## Task List

### Phase 1: Multi-agent runtime

- [x] Task 1: Extend the shared agent contract for Kimi CLI and Grok Build.
  - Acceptance: Go, TypeScript, and JSON schema agree on both identifiers.
  - Verification: protocol tests and type-check pass.
- [x] Task 2: Add Kimi local session discovery, history import, and headless
  resume streaming.
  - Acceptance: current and legacy home resolution work; subagents are ignored;
    missing CLI remains a non-fatal unavailable adapter.
  - Verification: fixture-based adapter and commander tests pass.
- [x] Task 3: Add Grok Build local session discovery, history import, and
  headless resume streaming.
  - Acceptance: URL-encoded project folders decode correctly; official
    `summary.json`/`chat_history.jsonl` layouts are supported; streamed output
    uses stable message IDs.
  - Verification: fixture-based adapter and commander tests pass, plus live
    read-only discovery against the installed Grok store.

### Checkpoint: Runtime

- [x] Daemon tests, vet, formatting, and non-Windows cross-build pass.
- [x] Existing Claude, Codex, and Kilo tests remain green.

### Phase 2: Thread information architecture

- [x] Task 4: Remove remote thread creation end to end.
  - Acceptance: no `create_session`/`session_created` wire types or daemon
    handlers remain; old PWA routes redirect without exposing a create action.
  - Verification: repository search returns only explanatory migration text.
- [x] Task 5: Implement `directory -> agent -> thread` grouping.
  - Acceptance: normalized full paths group together; same leaf names at
    different paths stay separate; missing paths join `未分类`; empty
    agent nodes never render.
  - Verification: focused Vitest coverage for Windows, UNC, Chinese, and
    directory-less sessions.

### Checkpoint: Navigation

- [x] Server tests and PWA grouping/store tests pass.
- [x] Direct session links hydrate without pretending the agent is Kilo.

### Phase 3: Brand and PWA redesign

- [x] Task 6: Integrate an original two-cat logo plus palette-specific Claude,
  Codex, Kilo, Kimi, and Grok portraits.
  - Acceptance: every final asset path exists under `pwa/public`, the UI and
    manifest reference them, and the logo remains legible at PWA icon sizes.
  - Verification: source-size review, a 96 px contact sheet, icon dimensions,
    referenced paths, and the production precache are validated.
- [x] Task 7: Rebuild the device workspace and session navigator in the deeper
  cute catgirl style.
  - Acceptance: folder-tab project sections, avatar-led agent bands, readable
    thread rows, responsive 360-480 px layout, focus states, reduced motion,
    empty/error/offline states, and no nested-card clutter.
  - Verification: type-check, production build, and browser screenshots at
    phone widths.
- [x] Task 8: Synchronize metadata and documentation.
  - Acceptance: PWA manifest/icons, README, deployment guide, and smoke checklist
    describe five agents, local-only thread creation, and the new hierarchy.
  - Verification: build precache includes every asset and `git diff --check`
    passes.

### Checkpoint: Complete

- [x] `go test -count=1 ./...` and `go vet ./...` pass in server and daemon
  after final adapter hardening.
- [x] PWA Vitest, `vue-tsc --noEmit`, and production build pass after the
  generated assets and mobile interaction fixes land.
- [x] Live Grok discovery and read-only history checks match its installed
  local store without exposing child or synthetic/system records; the missing
  Kimi executable is reported as an independent non-fatal unavailable adapter.
- [x] Final diff review confirms pre-existing user changes were preserved.

### Phase 4: Final hardening and generated-art replacement

- [x] Task 9: Close adapter ownership, history, process-stream, and watcher
  lifecycle gaps.
  - Acceptance: each adapter positively owns a session before routing;
    Kimi/Grok persisted history is turn-safe and excludes hidden synthetic
    records; stderr cannot become assistant text; watchers stop on close.
  - Verification: focused fixtures plus full daemon tests and vet.
- [x] Task 10: Replace every fallback brand image with an original DragTokens
  output.
  - Acceptance: one duo logo and five visually distinct agent portraits use
    the intended cel-shaded catgirl language, each portrait matches its tool
    palette, and all final source images live under `pwa/public`.
  - Verification: visual inspection at source size and 44/64/192/512 px,
    exact-dimension/format checks, referenced-path and precache checks.
- [x] Task 11: Close mobile navigation and touch ergonomics gaps while
  deepening the cute Neko Paradise presentation.
  - Acceptance: route params safely handle reserved characters; safe-area
    ownership is singular; interactive controls meet approximately 44 px
    targets; the hierarchy remains readable at 360–480 px.
  - Verification: focused Vitest, type-check, build, and browser screenshots.
- [x] Task 12: Run end-to-end final verification and remove temporary or
  rejected visual artifacts.
  - Acceptance: no remote-create surface returns, no generated temporary files
    remain, and all documented commands reflect the final five-agent product.
  - Verification: repository searches, full test matrix, `git diff --check`,
    and independent backend/PWA review.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Kimi is installed incompletely or outside PATH | Medium | Probe official home locations and treat availability independently from stored sessions |
| Agent store schemas evolve | High | Parse defensively, keep fixture coverage, and fall back to file timestamps and folder metadata |
| Headless CLIs cannot surface interactive approvals | High | Use each CLI's documented non-interactive mode, report blocked tools, and keep approval actions explicitly unavailable |
| Session IDs collide across agents | High | Qualify Kimi/Grok public IDs while retaining native IDs internally |
| Existing staged adapter edits overlap | High | Patch narrowly and verify staged/unstaged scope before final handoff |
| Generated art loses readability at icon size | Medium | Use centered head-and-shoulders compositions and generate derived PWA icon sizes from the duo mark |
| API-generated portraits drift into near-identical characters | Medium | Give every agent a distinct silhouette, hair, accessory, expression, and palette; inspect as a contact sheet before replacing assets |
| Generated-image work blocks the control path | High | Generate only during development/manual regeneration and ship cached static assets |

## Open Questions

- None blocking. The user's requirements explicitly authorize the multi-layer
  change and destructive UI refactor while excluding remote thread creation.
