> [English](./release.md) | 简体中文

# 发版流程（维护者）

面向仓库维护者切割版本。日常部署见 [deploy-vps.zh-CN.md](./deploy-vps.zh-CN.md)、[deploy-windows.zh-CN.md](./deploy-windows.zh-CN.md)。

## 前置

- 工作树干净；未暂存密钥、本地 `data/`、原生 agent 存储或构建产物
- [README.md](../README.md) / [README.zh-CN.md](../README.zh-CN.md) 产品边界与本版 [CHANGELOG.md](../CHANGELOG.md) 一致
- `LICENSE` / `LICENSE_zh` 仍为 SATA 2.0；Project URL 指向本仓库
- 文档索引仍准确（`docs/README.md` 与中文镜像）

## 1. 验证

仓库根目录（Windows 推荐显式命令）：

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

行为或部署路径有变时，按 [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) 对真实链路验收。

## 2. 变更记录与版本号

1. 将 `[Unreleased]`（若有）整理为新版本小节并填日期  
2. 确认 `pwa/package.json` 的 `version` 与即将打的 tag 一致（如 `0.1.0` ↔ `v0.1.0`）  
3. 若环境变量、支持的智能体、部署步骤或验收路径有变，同步更新**中英**文档（`README.md` / `README.zh-CN.md`，`docs/*.md` / `docs/*.zh-CN.md`）  

## 3. 提交、标签与 GitHub Release

```powershell
git status --short --branch
# 审查 diff 后按仓库风格提交

git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z

# 将 CHANGELOG 对应版本正文写入临时 notes 后：
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file release-notes.md
```

v0.x 默认只发布**源码与构建说明**；预编译二进制可选、不强制。Release 说明应链接 README 快速开始与部署文档（中英）。

## 4. 可选：生产更新

标签不等于已部署。

1. 重新编译 server + PWA；部署到 VPS；保留 `data/`  
2. 在 Windows 重新编译 daemon；替换 exe；保留 `%USERPROFILE%\.nekonest\config.json`  
3. 再跑 [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md)  

## 不要

- 在含密钥或脏工作树时打 tag  
- 未经明确决定 force-push 或移动已发布 tag  
- 把 `docs/archive/` 当作现行产品合同  
- 运维向行为变更时只更新一种语言  

## 相关

- [CHANGELOG.md](../CHANGELOG.md)
- [开发](./development.zh-CN.md)
- [文档索引](./README.zh-CN.md)
