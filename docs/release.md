> English | [简体中文](./release.zh-CN.md)

# Release process

Maintainer checklist for publishing a repository release. The exact build,
package, container-tag, and asset behavior is defined by
`.github/workflows/release.yml`; this page does not duplicate that workflow.

## Preconditions

- Explicit authorization to publish a tag and GitHub Release
- Clean worktree based on the intended `main` commit
- No secrets, local data, native transcripts, caches, or build artifacts staged
- `CHANGELOG.md` and operator docs describe the release behavior
- English and Chinese operator docs are aligned

## 1. Verify

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
pnpm install --frozen-lockfile
pnpm test
pnpm type-check
pnpm build

Set-Location ..
git diff --check
git status --short --branch
```

Run the [acceptance checklist](./e2e-smoke.md) when the release changes runtime,
deployment, service-worker, reconnect, native-agent, or security behavior.

## 2. Align release identity

1. Add the dated release section to `CHANGELOG.md`.
2. Set the same semantic version in:
   - `pwa/package.json`
   - `server/internal/buildinfo/version.go`
   - `daemon/internal/buildinfo/version.go`
3. Verify the built Server, daemon, and PWA report the intended version.
4. Review all docs and examples affected by configuration or deployment changes.

## 3. Tag and publish

After reviewing the final commit:

```powershell
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z
```

The tag starts the release workflow. Wait for it to finish, then verify the
GitHub Release, checksums, platform archives, and GHCR image directly against
the workflow output. Do not move an already published tag.

Useful inspection commands:

```powershell
gh run list --workflow release.yml --limit 3
gh release view vX.Y.Z --repo klarkxy/nekonest
```

If automation fails, repair the workflow or rerun the supported workflow path;
do not manually assemble a different set of artifacts under the same tag.

## 4. Deploy separately

A published release does not update a live nest.

1. Record the live versions and preserve rollback material.
2. Back up Server data/secrets and daemon state.
3. Deploy the exact approved release.
4. Verify public health, host reconnect, current PWA, and the changed workflow.
5. Keep rollback material until live acceptance is complete.

Follow the [VPS](./deploy-vps.md), [Windows](./deploy-windows.md), or
[Linux](./deploy-linux.md) upgrade section for operator steps.

## Related

- [CHANGELOG.md](../CHANGELOG.md)
- [Development](./development.md)
- [Acceptance checklist](./e2e-smoke.md)
