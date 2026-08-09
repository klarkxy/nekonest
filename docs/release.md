> English | [简体中文](./release.zh-CN.md)

# Release process (maintainers)

Cutting a version for the repository. Day-to-day deploy: [deploy-vps.md](./deploy-vps.md), [deploy-windows.md](./deploy-windows.md).

## Preconditions

- Clean worktree; no secrets, local `data/`, native agent stores, or build artifacts staged
- [README.md](../README.md) product boundaries match this version’s [CHANGELOG.md](../CHANGELOG.md)
- `LICENSE` / `LICENSE_zh` remain SATA 2.0; Project URL points at this repo
- Doc index still accurate (`docs/README.md` and Chinese twin)

## 1. Verify

From the repo root (Windows-friendly explicit commands):

```powershell
Set-Location server
go test -count=1 ./...
go vet ./...

Set-Location ..\daemon
go test -count=1 ./...
go vet ./...

Set-Location ..\pwa
pnpm install --frozen-lockfile
pnpm test
pnpm type-check
pnpm build

Set-Location ..
git diff --check
```

If behavior or deploy paths changed, run [e2e-smoke.md](./e2e-smoke.md) on a real nest.

## 2. Changelog and version

1. Fold `[Unreleased]` (if any) into a new version section with a date  
2. Align `pwa/package.json` `version`, `server/internal/buildinfo/version.go`, and `daemon/internal/buildinfo/version.go` with the tag (`0.2.0` ↔ `v0.2.0`)
   Verify with `nekonest-server -version`, `nekonest-daemon -version`, the PWA/Server version panel, and each machine's Daemon version on its device card.
3. If env vars, agents, deploy steps, or acceptance paths changed, update **both** English and Chinese docs (`README.md` / `README.zh-CN.md`, `docs/*.md` / `docs/*.zh-CN.md`)

## 3. Commit, tag, and publish

```powershell
git status --short --branch
# review diff, then commit in repo style

git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z

# The tag starts .github/workflows/release.yml. Watch it, then inspect assets:
$runId = gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId'
gh run watch $runId --exit-status --repo klarkxy/nekonest
gh release view vX.Y.Z --repo klarkxy/nekonest
```

The release workflow re-runs all module gates, verifies that the tag matches all
three version surfaces, then creates or updates the GitHub Release with:

- Server + matching PWA: Linux amd64 and arm64
- Daemon: Windows amd64, Linux amd64, and Linux arm64
- `checksums.txt` covering all five archives

Release asset names stay version-independent so README installation links may
use `releases/latest/download`; the immutable tag and each archive's `VERSION`
file carry the version. Server archives include `pwa-dist`. Release notes
should link the README quick start and deploy docs (EN + ZH).

To repair missing assets on an existing immutable tag, run **Release binaries**
manually with the exact tag. The workflow checks out that tag's source but uses
the automation from the selected workflow revision; it never moves the tag.

## 4. Production update and live acceptance

A tag is not a deploy.

Publishing release assets does not by itself update production. For
runtime-affecting maintenance of the configured live nest, local tests are not
final acceptance unless the task was explicitly scoped local-only.

1. Merge and push the approved commit; rebuild Server, PWA, and daemon from that
   exact commit.
2. Inspect the live systemd unit and daemon process/launcher before changing
   files; sample values in deploy docs are not authoritative for an existing host.
3. Verify artifact hashes and create rollback copies. Preserve `/opt/nekonest/data`,
   Server environment files, and `%USERPROFILE%\.nekonest\config.json`.
4. Update Server/PWA and the Windows daemon, then confirm public health, systemd
   stability, daemon reconnect, and current PWA asset/version.
5. Run the changed workflow through [e2e-smoke.md](./e2e-smoke.md). Capture
   post-deploy CPU/I/O/memory/handle evidence when runtime load was part of the fix.

Deployment does not authorize a tag, GitHub Release, or unrelated production
change. Report the deployed commit, rollback locations, and any check that could
not be completed.

## Do not

- Tag with secrets or a dirty tree  
- Force-push or move published tags without an explicit decision  
- Treat `docs/archive/` as the live product contract  
- Update only one language when operator-facing behavior changes  

## Related

- [CHANGELOG.md](../CHANGELOG.md)
- [Development](./development.md)
- [Docs index](./README.md)
