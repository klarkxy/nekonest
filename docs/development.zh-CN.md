> [English](./development.md) | 简体中文

# 本地开发

这是 NekoNest 仓库的贡献者环境说明。产品与跨层不变量由
[README.zh-CN.md](../README.zh-CN.md) 和 [AGENTS.md](../AGENTS.md) 定义。

## 前置条件

- Go 1.22+
- Node.js 与 pnpm
- 可选的受支持智能体 CLI，用于只读原生存储冒烟

根 `go.work` 连接三个独立 Go 模块：`relaycore/`、`server/` 和 `daemon/`。
PWA 是 `pwa/` 下独立的 pnpm 项目；仓库根不是 Go 模块。

## 本地安装与运行

构建或启动 PWA：

```powershell
Set-Location pwa
pnpm install --frozen-lockfile
pnpm dev
```

在另一终端运行只监听 loopback 的开发 Server，并把可丢弃数据放到仓库外：

```powershell
Set-Location server
$devData = Join-Path $env:TEMP "nekonest-server-dev"
go run ./cmd/server -port 8080 -data $devData
```

没有管理员密钥时，Server 会有意只监听 loopback。需要本地端到端 Daemon 时，
先注册到该 Server，再启动 Daemon：

```powershell
Set-Location daemon
$devDir = Join-Path $env:TEMP "nekonest-daemon-dev"
New-Item -ItemType Directory -Force -Path $devDir | Out-Null
$devConfig = Join-Path $devDir "config.json"
$env:NEKONEST_SERVER = "http://127.0.0.1:8080"
go run ./cmd/daemon -config $devConfig -register -name "dev-pc"
go run ./cmd/daemon -config $devConfig
```

使用可丢弃的本地数据；冒烟测试不得修改用户的原生智能体存储。

## 验证

先运行受影响模块；跨层变更再运行所有模块。

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

常用 PWA 针对性检查：

```powershell
Set-Location pwa
pnpm test:visual
# 只有审查过有意的视觉变更后，才能更新黄金截图：
pnpm test:visual:update
```

行为依赖真实反代、Service Worker、原生存储、主机进程或重连路径时，最后还要
运行[验收清单](./e2e-smoke.zh-CN.md)。

## 变更规则

- 把 `protocol/protocol.json` 作为线上权威，并共同更新所有受影响的
  Go/Daemon/PWA 入口。
- UI 按声明能力开放，不根据智能体名称猜测支持。
- 发现与验证期间保持原生存储只读。
- 每个行为修复都增加回归测试。
- 变更的 Go 文件运行 `gofmt`，TypeScript/Vue 沿用现有风格。
- 不提交密钥、本地数据、原生转录、构建输出、缓存、覆盖率或
  `.codegraph/codegraph.db`。

## 文档

根目录和 docs 的英文文件是主版本，简体中文镜像使用 `.zh-CN.md` 后缀。面向
运维的行为变化必须同步两种语言。精确枚举、消息字段、工作流算法和内部包细节应
留在权威源码，不要复制到说明文档。

源码变更后，若 CodeGraph 可用，运行 `codegraph sync`。

## 相关文档

- [架构](./architecture.zh-CN.md)
- [协议](./protocol.zh-CN.md)
- [发版](./release.zh-CN.md)
