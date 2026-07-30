> [English](./development.md) | 简体中文

# 本地开发

NekoNest monorepo 贡献者环境。跨层不变量：[AGENTS.md](../AGENTS.md)。产品合同：[README.zh-CN.md](../README.zh-CN.md)。

## 前置

| 工具 | 说明 |
|---|---|
| Go 1.22+ | `server/` 与 `daemon/` 各自独立 module |
| Node.js + pnpm | PWA；使用已提交的 `pnpm-lock.yaml` |
| 可选 agent CLI | 仅 Windows 上做只读发现冒烟时需要 |
| codegraph（可选） | 代码导航；改源码后 `codegraph sync` |

## 仓库布局

```text
nekonest/
├── protocol/          # protocol.json（手动类型）
├── server/            # module github.com/nekonest/server
├── daemon/            # module github.com/nekonest/daemon
├── pwa/               # Vue 3 + TypeScript + Pinia
├── docs/              # 运维 + 贡献者文档
├── tools/             # 品牌资源构建脚本
├── AGENTS.md
├── README.md
└── README.zh-CN.md
```

**没有根 Go module**。勿把 `_archive/`、`go-sdk/`、`gocache/`、`.pnpm-store/`、`bin/`、`data/`、已构建 PWA 或原生 agent 存储当作应用源码。

## 本地 Server（loopback 开发模式）

**未**设置 `NEKONEST_PHONE_SECRET` 时：

- Server 仅绑定 **`127.0.0.1`**
- 日志提示手机鉴权关闭
- 若 `NEKONEST_ALLOWED_ORIGINS` 为空，可能注入默认本机来源供 CORS
- bootstrap 亦空时注册可能开放——**仅开发**

```powershell
cd pwa
pnpm install --frozen-lockfile
pnpm build

cd ..\server
go run ./cmd/server -port 8080 -data ./data -pwa ../pwa/dist
```

更接近生产的本地运行可设置手机密钥 + bootstrap，仍用 loopback 或本地反代。

PWA 开发服：

```powershell
cd pwa
pnpm dev
```

按分支将开发客户端指向本地 server URL。

## 本地 Daemon

在 Windows 上，使用已注册配置（或指向本地 server）：

```powershell
cd daemon
go run ./cmd/daemon
```

注册：

```powershell
$env:NEKONEST_SERVER = "http://127.0.0.1:8080"
# 类公网本地可选：$env:NEKONEST_BOOTSTRAP_TOKEN = "…"
go run ./cmd/daemon -register -name "dev-pc"
go run ./cmd/daemon
```

适配器冒烟（只读发现辅助）：

```powershell
go run ./cmd/adapter_smoke
```

冒烟测试中切勿改写用户原生 agent 存储。

## 测试

Windows 上推荐显式模块命令：

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

Unix 可用根目录 `Makefile` 的 `test`、`server`、`daemon`、`pwa`（daemon 默认交叉编译 Windows）。

跨层协议或目录变更：跑**全部三个**套件，然后在仓库根：

```powershell
git diff --check
codegraph sync
codegraph status
```

## 协议与智能体变更

线协议变更须触及所有适用面——见 [protocol.zh-CN.md](./protocol.zh-CN.md) 清单与 AGENTS.md「Wire protocol」。

新增智能体：

1. Daemon 适配器 + 注册表  
2. 如需则 server 类型 / 持久化  
3. PWA `types/protocol.ts`、`config/agents.ts`、资源  
4. `protocol/protocol.json`  
5. 测试 + README/文档智能体表  

## 格式与风格

- 改动的 Go 文件跑 `gofmt`
- 遵循既有 TypeScript/Vue 风格；不做顺手大重构
- 除非依赖声明变更，勿手改 lockfile
- 代码默认不擅自加注释；文档为自由 Markdown

## 勿提交

密钥、`data/`、设备 `config.json`、附件 blob、agent 转录、构建产物、归档包、覆盖率 DB、`.codegraph/codegraph.db`。

## 文档语言布局

| 路径模式 | 语言 |
|---|---|
| `README.md`、`docs/*.md` | 英文（主） |
| `README.zh-CN.md`、`docs/*.zh-CN.md` | 简体中文镜像 |
| `AGENTS.md`、`CHANGELOG.md` | 仅英文 |
| `docs/archive/` | 冻结历史（非现行合同） |

## 相关文档

- [架构](./architecture.zh-CN.md)
- [协议](./protocol.zh-CN.md)
- [配置](./configuration.zh-CN.md)
- [端到端冒烟](./e2e-smoke.zh-CN.md)
- [发版](./release.zh-CN.md)
