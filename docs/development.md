> English | [简体中文](./development.zh-CN.md)

# Local development

Contributor setup for the NekoNest monorepo. Cross-layer invariants: [AGENTS.md](../AGENTS.md). Product contract: [README](../README.md).

## Prerequisites

| Tool | Notes |
|---|---|
| Go 1.22+ | Separate modules under `server/` and `daemon/` |
| Node.js + pnpm | PWA; use committed `pnpm-lock.yaml` |
| Optional agent CLIs | For live discovery smoke on Windows only |
| codegraph (optional) | Repo navigation; run `codegraph sync` after source edits |

## Repository layout

```text
nekonest/
├── protocol/          # protocol.json (manual types)
├── server/            # module github.com/nekonest/server
├── daemon/            # module github.com/nekonest/daemon
├── pwa/               # Vue 3 + TypeScript + Pinia
├── docs/              # operator + contributor docs
├── tools/             # brand asset build script
├── AGENTS.md
├── README.md
└── README.zh-CN.md
```

There is **no root Go module**. Do not treat `_archive/`, `go-sdk/`, `gocache/`, `.pnpm-store/`, `bin/`, `data/`, built PWA output, or native agent stores as application source.

## Local server (loopback dev mode)

With **no** `NEKONEST_PHONE_SECRET`:

- Server binds **`127.0.0.1` only**
- Logs that phone auth is off
- Default local origins may be injected for CORS if `NEKONEST_ALLOWED_ORIGINS` is empty
- Registration may be open if bootstrap is also unset—**dev only**

```powershell
cd pwa
pnpm install --frozen-lockfile
pnpm build

cd ..\server
go run ./cmd/server -port 8080 -data ./data -pwa ../pwa/dist
```

For a closer-to-prod local run, set phone secret + bootstrap and still use loopback or a local reverse proxy.

PWA dev server:

```powershell
cd pwa
pnpm dev
```

Point the dev client at your local server URL as configured in the PWA env/settings for your branch.

## Local daemon

On Windows, with a registered config (or after pointing at a local server):

```powershell
cd daemon
go run ./cmd/daemon
```

Register against a server:

```powershell
$env:NEKONEST_SERVER = "http://127.0.0.1:8080"
# optional for public-like local: $env:NEKONEST_BOOTSTRAP_TOKEN = "…"
go run ./cmd/daemon -register -name "dev-pc"
go run ./cmd/daemon
```

Adapter smoke (read-only discovery helpers):

```powershell
go run ./cmd/adapter_smoke
```

Never mutate the user’s native agent store during smoke tests.

## Tests

Prefer explicit module commands on Windows:

```powershell
# Server
Set-Location server
go test -count=1 ./...
go vet ./...

# Daemon
Set-Location ..\daemon
go test -count=1 ./...
go vet ./...

# PWA
Set-Location ..\pwa
pnpm install --frozen-lockfile
pnpm test
pnpm type-check
pnpm build
```

### PWA visual regression

The repository includes a Windows/Chromium Playwright screenshot suite. It starts a deterministic HTTP/WebSocket mock on `127.0.0.1:18080` and Vite on `127.0.0.1:5173`; both ports must be free. The mock uses the real PWA REST and wire-message shapes but never reads native agent stores.

```powershell
Set-Location pwa
pnpm exec playwright install chromium

# Compare against committed golden screenshots.
pnpm test:visual

# Replace goldens only after reviewing an intentional UI change.
pnpm test:visual:update

# Open the HTML report produced by the latest run.
pnpm test:visual:report
```

Golden screenshots live beside `e2e/visual/visual.spec.ts`; `test-results/` and `playwright-report/` are local artifacts and are ignored. The primary matrix is `390×844`, Simplified Chinese, and light theme. Narrow mobile, desktop, dark-theme, and English samples protect responsive and overflow behavior. A visual run also checks expected page state, console/page errors, horizontal overflow, primary touch targets, delivery states, and the agent-scoped first-prompt `start_thread` handoff.

For a read-only screenshot smoke against an already running local PWA/server/daemon stack, provide the base URL and optional device/session identifiers. Supply either a phone token or admin secret without writing it to a file:

```powershell
$env:NEKONEST_VISUAL_BASE_URL = 'http://127.0.0.1:5173'
$env:NEKONEST_VISUAL_PHONE_TOKEN = '<temporary phone token>'
$env:NEKONEST_VISUAL_PHONE_ID = '<phone id>'
# Or use NEKONEST_VISUAL_ADMIN_SECRET for legacy/admin local authentication.
$env:NEKONEST_VISUAL_DEVICE_ID = '<device id>'
$env:NEKONEST_VISUAL_SESSION_ID = '<session id>'
pnpm test:visual:live
```

The live command only captures devices, device detail, and session detail by default. Setting `NEKONEST_VISUAL_SEND_PROMPT` explicitly sends that text to the selected session and therefore must only be used with a disposable test thread. Live screenshots are artifacts, not golden baselines.

Unix convenience: root `Makefile` targets `test`, `server`, `daemon`, `pwa` (daemon default cross-builds Windows).

Cross-layer protocol or catalog changes: run **all three** suites, then from repo root:

```powershell
git diff --check
codegraph sync
codegraph status
```

## PWA i18n and theme

- Locales: `pwa/src/i18n/locales/zh-CN.ts` (default) and `en.ts` — keep key sets identical (`pnpm test` includes parity check).
- Runtime: `vue-i18n` Composition API; stores/utils use `tGlobal` from `@/i18n`.
- User prefs: `localStorage` keys `nekonest_locale` (`zh-CN` \| `en`) and `nekonest_theme` (`system` \| `light` \| `dark`).
- Wire enums and agent product names stay English; only UI chrome is translated.
- Agent transcripts / Markdown body are never passed through i18n.

When adding UI copy: add keys to both locale files, then `t()` / `tGlobal()`. Do not hardcode Chinese or English strings in views.

## Protocol and agent changes

Wire changes must touch every applicable surface—see [protocol.md](./protocol.md) checklist and AGENTS.md “Wire protocol”.

Adding an agent:

1. Daemon adapter + registry
2. Server types / persistence if needed
3. PWA `types/protocol.ts`, `config/agents.ts`, assets
4. `protocol/protocol.json`
5. Tests + README/docs agent tables

## Formatting and style

- `gofmt` on changed Go files
- Match existing TypeScript/Vue style; no drive-by refactors
- Do not hand-edit lockfiles unless dependencies changed
- Do not add comments unless asked (code); docs are free-form markdown

## What not to commit

Secrets, `data/`, device `config.json`, attachment blobs, agent transcripts, build outputs, archives, coverage DBs, `.codegraph/codegraph.db`.

## Docs language layout

| Path pattern | Language |
|---|---|
| `README.md`, `docs/*.md` | English (primary) |
| `README.zh-CN.md`, `docs/*.zh-CN.md` | Simplified Chinese mirror |
| `AGENTS.md`, `CHANGELOG.md` | English only |
| `docs/archive/` | Frozen history (not the live contract) |

## Related docs

- [Architecture](./architecture.md)
- [Protocol](./protocol.md)
- [Configuration](./configuration.md)
- [E2E smoke](./e2e-smoke.md)
- [Release](./release.md)
