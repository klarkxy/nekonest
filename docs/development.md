> English | [简体中文](./development.zh-CN.md)

# Local development

Contributor setup for the NekoNest repository. Product and cross-layer
invariants are defined in [README.md](../README.md) and
[AGENTS.md](../AGENTS.md).

## Prerequisites

- Go 1.22+
- Node.js and pnpm
- Optional supported agent CLIs for read-only native-store smoke tests

The root `go.work` links three independent Go modules: `relaycore/`, `server/`,
and `daemon/`. The PWA is a separate pnpm project under `pwa/`; there is no root
Go module.

## Install and run locally

Build or start the PWA:

```powershell
Set-Location pwa
pnpm install --frozen-lockfile
pnpm dev
```

In another terminal, run a loopback-only development Server with disposable
data outside the repository:

```powershell
Set-Location server
$devData = Join-Path $env:TEMP "nekonest-server-dev"
go run ./cmd/server -port 8080 -data $devData
```

With no admin secret, the Server intentionally binds to loopback. For a local
end-to-end daemon run, register against that Server and then start the daemon:

```powershell
Set-Location daemon
$devDir = Join-Path $env:TEMP "nekonest-daemon-dev"
New-Item -ItemType Directory -Force -Path $devDir | Out-Null
$devConfig = Join-Path $devDir "config.json"
$env:NEKONEST_SERVER = "http://127.0.0.1:8080"
go run ./cmd/daemon -config $devConfig -register -name "dev-pc"
go run ./cmd/daemon -config $devConfig
```

Use disposable local data and do not mutate a user's native agent store during
smoke tests.

## Verification

Run the affected module first, then all modules for cross-layer changes.

```powershell
Set-Location relaycore
go test -count=1 ./...
go vet ./...

Set-Location ..\server
go test -count=1 ./...
go vet ./...

Set-Location ..\daemon
go test -count=1 ./...
go vet ./...

Set-Location ..\pwa
pnpm test
pnpm type-check
pnpm build

Set-Location ..
git diff --check
```

Useful focused PWA checks:

```powershell
Set-Location pwa
pnpm test:visual
# Update golden screenshots only after reviewing an intentional visual change:
pnpm test:visual:update
```

For behavior that depends on the real reverse proxy, service worker, native
store, host process, or reconnect path, finish with the
[acceptance checklist](./e2e-smoke.md).

## Change rules

- Treat `protocol/protocol.json` as the wire authority and update every affected
  Go/daemon/PWA surface together.
- Gate UI behavior on advertised capabilities; do not infer support from an
  agent name.
- Keep native stores read-only during discovery and verification.
- Add a regression test with every behavior fix.
- Run `gofmt` on changed Go files and use the existing TypeScript/Vue style.
- Do not commit secrets, local data, native transcripts, build output, caches,
  coverage, or `.codegraph/codegraph.db`.

## Documentation

English root/docs files are primary; Simplified Chinese mirrors use the
`.zh-CN.md` suffix. Update both languages when operator-facing behavior changes.
Keep exact enums, message fields, workflow algorithms, and internal package
details in their authoritative source instead of copying them into prose.

After source changes, run `codegraph sync` when CodeGraph is available.

## Related

- [Architecture](./architecture.md)
- [Protocol](./protocol.md)
- [Release](./release.md)
