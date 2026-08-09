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
2. 确认 `pwa/package.json`、`server/internal/buildinfo/version.go` 与 `daemon/internal/buildinfo/version.go` 的版本均与即将打的 tag 一致（如 `0.2.0` ↔ `v0.2.0`）
   用 `nekonest-server -version`、`nekonest-daemon -version`、PWA/Server 版本面板及每台机器卡片上的 Daemon 版本核验。
3. 若环境变量、支持的智能体、部署步骤或验收路径有变，同步更新**中英**文档（`README.md` / `README.zh-CN.md`，`docs/*.md` / `docs/*.zh-CN.md`）  

## 3. 提交、打标签并发布

```powershell
git status --short --branch
# 审查 diff 后按仓库风格提交

git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z

# tag 会启动 .github/workflows/release.yml；等待并检查产物：
$runId = gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId'
gh run watch $runId --exit-status --repo klarkxy/nekonest
gh release view vX.Y.Z --repo klarkxy/nekonest
```

发布工作流会重新执行全部模块门禁，确认 tag 与三处版本号一致，然后创建或
更新 GitHub Release，包含：

- Server + 同版本 PWA：Linux amd64、Linux arm64
- Daemon：Windows amd64、Linux amd64、Linux arm64
- 覆盖以上五个压缩包的 `checksums.txt`

Release 附件名不含版本号，因此 README 可以使用
`releases/latest/download` 稳定链接；不可变 tag 与每个压缩包内的
`VERSION` 文件记录版本。Server 压缩包内含 `pwa-dist`。Release 说明应链接
README 快速开始与中英文部署文档。

若要补齐既有不可变 tag 缺失的附件，可手动运行 **Release binaries** 并填写
该 tag。工作流会检出该 tag 的源码，同时使用所选工作流版本中的发布自动化，
不会移动 tag。

## 4. 生产更新与线上验收

标签不等于已部署。

发布附件本身不会更新生产环境。但对于当前配置线上猫窝的运行时维护，只要任务没有
明确限定 local-only，本地测试就不算最终验收。

1. 合并并推送已批准提交；从这个确切提交重新构建 Server、PWA 与 daemon。
2. 改文件前先读取线上 systemd unit 与 daemon 进程/启动器；部署文档示例值不代表
   已有主机的真实配置。
3. 核对产物哈希并建立回滚副本；保留 `/opt/nekonest/data`、Server 环境文件和
   `%USERPROFILE%\.nekonest\config.json`。
4. 更新 Server/PWA 与 Windows daemon，然后确认公网健康、systemd 稳定、daemon
   重连以及当前 PWA 资源/版本。
5. 按 [e2e-smoke.zh-CN.md](./e2e-smoke.zh-CN.md) 跑此次改动路径；若修复涉及运行
   负载，还须采集部署后 CPU/I/O/内存/句柄证据。

部署不代表有权打 tag、发布 GitHub Release 或执行无关生产变更。交付时须报告部署
提交、回滚位置以及未完成的检查。

## 不要

- 在含密钥或脏工作树时打 tag  
- 未经明确决定 force-push 或移动已发布 tag  
- 把 `docs/archive/` 当作现行产品合同  
- 运维向行为变更时只更新一种语言  

## 相关

- [CHANGELOG.md](../CHANGELOG.md)
- [开发](./development.zh-CN.md)
- [文档索引](./README.zh-CN.md)
