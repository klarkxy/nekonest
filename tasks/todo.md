# Multi-Agent Neko Paradise Checklist

## Runtime contract

- [x] Add `kimi_cli` and `grok_build` to daemon, server, PWA, and JSON schema.
- [x] Add agent-qualified public IDs for Kimi/Grok sessions.
- [x] Wire both adapters into discovery, routing, callbacks, cleanup, and logs.

## Kimi CLI

- [x] Discover `KIMI_CODE_HOME` / `~/.kimi-code/sessions`.
- [x] Parse `state.json` and main-agent `wire.jsonl`.
- [x] Resume current Kimi Code sessions and add legacy `--print` compatibility.
- [x] Import history and stream output with stable IDs.
- [x] Cover discovery, parsing, missing CLI, and interruption in tests.

## Grok Build

- [x] Discover `~/.grok/sessions/<encoded-cwd>/<session-id>`.
- [x] Parse `summary.json` and `chat_history.jsonl`, excluding subagents and
  synthetic prompts.
- [x] Resume with `grok -p ... --resume ... --output-format streaming-json`
  and explicit safe `--permission-mode auto`.
- [x] Import history and stream cumulative text with stable IDs.
- [x] Cover discovery, parsing, CLI arguments, and interruption in tests.

## Local-only threads

- [x] Remove create-session protocol constants and schema values.
- [x] Remove server forwarding and daemon creation handler/helper.
- [x] Remove NewSession page and route import.
- [x] Redirect old `/sessions` and `/new-session` URLs to the device workspace.
- [x] Update README and deployment/smoke docs.

## Directory -> agent -> thread

- [x] Add the central Agent presentation catalog.
- [x] Add pure session-tree grouping and normalization helpers.
- [x] Add grouping tests for Windows/UNC/Chinese/missing paths.
- [x] Upgrade collapse keys to project- and agent-scoped nodes.
- [x] Rebuild session navigation with project folders, agent avatars, and rows.
- [x] Remove the temporary Kilo fallback from direct detail hydration.

## Brand assets

- [x] Define the stable two-cat logo and five agent portrait slots.
- [x] Add palette-specific Claude Code, Codex, Kilo, Kimi CLI, and Grok Build
  presentation metadata.
- [x] Derive Apple/PWA icon sizes and validate production inclusion.
- [x] Generate an original two-cat NekoNest logo with DragTokens `gpt-image-2`.
- [x] Generate distinct Claude Code, Codex, Kilo, Kimi CLI, and Grok Build
  catgirl portraits with tool-specific palettes and silhouettes.
- [x] Inspect all six outputs at source and icon sizes, then replace the
  deterministic fallbacks and regenerate PWA derivatives.

## Adapter hardening

- [x] Require positive adapter ownership before session routing.
- [x] Keep stderr as diagnostics only for Kimi/Grok headless execution.
- [x] Aggregate Kimi streamed history parts into coherent turns.
- [x] Exclude Grok primer/system/synthetic records from phone-visible history.
- [x] Stop Kimi/Grok watcher goroutines when adapters close.
- [x] Add focused regression fixtures for all of the above.

## Mobile polish

- [x] Use encoded/named route params for device and session navigation.
- [x] Keep a single safe-area owner and use dynamic viewport units.
- [x] Expand reorder/archive and other compact controls to reliable touch
  targets without visually inflating the glyphs.
- [x] Recheck focus, reduced motion, long paths, empty/error/offline states,
  and 360/480 px layouts after the final generated assets land.

## UI redesign

- [x] Centralize palette, spacing, typography, surfaces, and motion tokens.
- [x] Redesign DeviceList branding and device states.
- [x] Redesign DeviceDetail as the single workspace navigator.
- [x] Update SessionDetail identity/avatar surfaces without clobbering its
  current attachment/message changes.
- [x] Update Setup/Pair shared styling where global changes expose gaps.
- [x] Verify keyboard focus, reduced motion, long paths, and empty states.

## Final verification

- [x] Daemon tests, vet, gofmt, and Linux cross-build after final hardening.
- [x] Server tests, vet, and gofmt.
- [x] PWA tests, type-check, production build, and asset precache.
- [x] Live read-only Kimi/Grok discovery smoke tests where installed.
- [x] Browser screenshots at 360 px and 480 px with final art.
- [x] `git diff --check` and focused preservation review.
