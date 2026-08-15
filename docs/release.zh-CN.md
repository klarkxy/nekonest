> [English](./release.md) | 简体中文

# 发版流程

这是维护者发布仓库版本的清单。准确的构建、打包、容器标签和资产行为由
`.github/workflows/release.yml` 定义；本页不重复工作流算法。

## 前置条件

- 已明确授权发布 tag 与 GitHub Release
- 工作树干净，基于预期的 `main` 提交
- 暂存区没有密钥、本地数据、原生转录、缓存或构建产物
- `CHANGELOG.md` 与运维文档已经描述本版本行为
- 中英文运维文档一致

## 1. 验证

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

版本改变运行时、部署、Service Worker、重连、原生智能体或安全行为时，运行
[验收清单](./e2e-smoke.zh-CN.md)。

## 2. 对齐发行身份

1. 在 `CHANGELOG.md` 增加带日期的版本小节。
2. 在以下位置设置同一个语义版本：
   - `pwa/package.json`
   - `server/internal/buildinfo/version.go`
   - `daemon/internal/buildinfo/version.go`
3. 验证构建后的 Server、Daemon 与 PWA 报告预期版本。
4. 审查所有受配置或部署变化影响的文档与示例。

## 3. 打标签并发布

审查最终提交后：

```powershell
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z
```

tag 会启动发布工作流。等待完成后，根据工作流输出直接验证 GitHub Release、
checksums、平台压缩包和 GHCR 镜像。不要移动已经发布的 tag。

常用检查命令：

```powershell
gh run list --workflow release.yml --limit 3
gh release view vX.Y.Z --repo klarkxy/nekonest
```

自动化失败时，修复工作流或重跑受支持的工作流路径；不要在同一 tag 下手工拼装
另一套资产。

## 4. 单独部署

发布版本不会自动更新线上实例。

1. 记录线上版本并保留回滚材料。
2. 备份 Server 数据/密钥和 Daemon 状态。
3. 部署准确的已批准版本。
4. 验证公网健康、主机重连、当前 PWA 与本次变更流程。
5. 线上验收完成前保留回滚材料。

运维步骤见 [VPS](./deploy-vps.zh-CN.md)、
[Windows](./deploy-windows.zh-CN.md) 或 [Linux](./deploy-linux.zh-CN.md)
升级小节。

## 相关文档

- [CHANGELOG.md](../CHANGELOG.md)
- [本地开发](./development.zh-CN.md)
- [验收清单](./e2e-smoke.zh-CN.md)
