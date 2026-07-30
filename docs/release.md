# 发版流程（维护者）

面向仓库维护者切割版本。操作者日常部署见 [VPS 部署](deploy-vps.md) 与
[Windows Daemon 部署](deploy-windows.md)。

## 前置

- 工作树干净，且不含密钥、本地 `data/`、本机 agent 存储或构建产物
- `README.md` 中的产品边界与本版 `CHANGELOG.md` 一致
- 许可证文件 `LICENSE` / `LICENSE_zh` 仍为 SATA 2.0，Project Url 指向本仓库

## 1. 验证

在仓库根目录按模块执行（Windows 推荐显式命令）：

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

行为或部署路径有变时，按 [端到端冒烟清单](e2e-smoke.md) 对真实链路做一次验收。

## 2. 变更记录与版本号

1. 将 `CHANGELOG.md` 中 `[Unreleased]`（若有）整理为新版本小节，并填写日期
2. 确认 `pwa/package.json` 的 `version` 与即将打的 tag 一致（如 `0.1.0` ↔ `v0.1.0`）
3. 若环境变量、支持的智能体、部署步骤或验收路径有变，同步 `README.md` 与 `docs/`

## 3. 提交、标签与 GitHub Release

```powershell
git status --short --branch
# 审查 diff 后提交（信息遵循仓库既有风格）

git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z

# 将 CHANGELOG 对应版本正文写入临时 notes 文件后：
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file release-notes.md
```

v0.1.x 默认只发布源码与构建说明，不强制附带预编译二进制。Release 说明中应链接
README 快速开始与部署文档。

## 4. 可选：生产更新

标签不等于已部署。需要更新自托管实例时：

1. 在构建机按 README 重新编译 server 与 PWA，部署到 VPS
2. 在 Windows 上重新编译并替换 daemon，保留 `%USERPROFILE%\.nekonest\config.json`
3. 再跑一遍 [e2e-smoke.md](e2e-smoke.md)

## 不要

- 在含密钥或脏工作树时打 tag
- 未经明确要求 force-push 或移动已发布 tag
- 把 `docs/archive/` 中的施工记录当作现行产品合同
