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

## 3. Commit, tag, GitHub Release

```powershell
git status --short --branch
# review diff, then commit in repo style

git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z

# write CHANGELOG section body to a temp notes file, then:
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file release-notes.md
```

v0.x defaults to **source + build instructions**; prebuilt binaries are optional, not required. Release notes should link README quick start and deploy docs (EN + ZH).

## 4. Production update and live acceptance

A tag is not a deploy.

For source-only releases, production deployment may still be omitted deliberately.
For runtime-affecting maintenance of the configured live nest, however, local
tests are not final acceptance unless the task was explicitly scoped local-only.

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
