<div align="center">
  <img src="./pwa/public/brand/nekonest-duo.webp" width="360" alt="NekoNest">

  <h1>NekoNest · 猫娘窝</h1>

  <p><strong>在手机上，安全续写 Windows 或 Linux 主机中的编码智能体线程。</strong></p>
  <p>自托管 · 主机仅出站连接 · 原生会话存储 · 移动端 PWA</p>

  <p>
    <a href="./README.md">English</a> ·
    <a href="#快速开始">快速开始</a> ·
    <a href="#支持的智能体">支持的智能体</a> ·
    <a href="#文档">文档</a> ·
    <a href="#许可证">许可证</a>
  </p>
  <p>
    <a href="https://github.com/klarkxy/nekonest/actions/workflows/ci.yml"><img src="https://github.com/klarkxy/nekonest/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  </p>
</div>

---

NekoNest 是一个自托管的远程续写桥梁：VPS 负责认证、配对、消息中转与持久化；Windows/Linux Daemon 主动连接 VPS，并从各智能体的本地**原生**存储中发现线程；手机 PWA 用于查看历史、发送提示词和附件、接收流式输出，并通过原生 app-server 完整控制 Codex。

> [!IMPORTANT]
> NekoNest 主要**续写**主机上已经存在的线程。手机可先打开 agent 范围的本地草稿；仅当该 agent 的 starter 已安装、探测通过并宣告 `spawn=true` 时，发送首条提示词才会创建原生线程，且目标只能是 daemon 当前由原生会话发现的项目目录并集。禁止任意路径与通用 `create_session`。创建结果仅在首条提示词得到正向确认、且该 agent 的权威原生存储确认所有权后显示为 owned。

## 工作方式

```text
┌─────────────┐       HTTPS / WSS       ┌──────────────────┐
│  手机 PWA   │ ◄─────────────────────► │  VPS Server      │
│ Vue 3 + PWA │                         │  Go + SQLite     │
└─────────────┘                         └────────┬─────────┘
                                                 │ WSS
                                                 │ 由 PC 主动发起
                                        ┌────────▼─────────┐
                                        │ 主机 Daemon      │
                                        │ Windows / Linux  │
                                        │ 发现 / 历史 / 执行 │
                                        └────────┬─────────┘
                                                 │ 本地存储与 CLI
                    ┌────────────┬───────────┬───┴────────┬────────────┐
                    │Claude Code │   Codex   │ Kimi CLI   │ Grok Build │
                    └────────────┴───────────┴────────────┴────────────┘
```

家中电脑不需要公网 IP，也不需要开放入站端口。Daemon 通过出站 WebSocket 连接 VPS；手机只访问启用 HTTPS/WSS 的 VPS。

## 核心能力

- **近期原生线程发现**：按 `目录 → 智能体 → 线程` 展示最近 7 天有活动的线程；仍在运行/等待处理的线程始终可见，没有可识别目录的线程归入「**未分类**」，隐藏旧线程不会删除原生数据。
- **可靠续写**：提示词具有独立的接受、提交与失败状态；断线重连不会把“传输成功”误当作“智能体已接收”。
- **历史与流式输出**：合并原生历史、服务端持久化与实时输出，并保持稳定消息标识；CLI 标准错误只作本机诊断，不进入对话正文。
- **图片与文档附件**：手机上传后由 Daemon 下载到本次任务临时目录，再按各 CLI 能力传入（最多 5 个、单个 ≤ 4 MB）。
- **按能力控制 Agent**：Codex 仍是唯一全控制 Agent。每条可靠且已安装的发送路径都可使用 NekoNest 持久 FIFO（不是 Agent 原生队列）；审批、用户提问、新建、中断与附件均独立探测，缺失即关闭。
- **持久传输模式**：每个窝只能固定为 `open` 或 `sealed`。新数据库默认 sealed；缺少模式元数据的旧数据库一次性认定并持久化为 open；不匹配时失败关闭。
- **手机端降级防护**：PWA 按来源钉扎模式；sealed 来源不能静默变成 open，首次使用管理员明确选择的开放中继必须显式确认。
- **移动端体验**：可安装 PWA、会话草稿、线程级或整项目的手机本地收起、经清理的 Markdown、断线恢复与可选 Web Push。
- **版本诊断**：页面顶部对比当前网页与实时 Server 版本；每台机器在自己的设备卡片上报告 Daemon 版本及更新状态。
- **安全默认值**：管理员引导、可撤销手机身份、Daemon 注册令牌、来源校验、附件校验、消息大小限制与受控代理信任。

## 支持的智能体

| 智能体 | 本地会话来源 | 控制方式 | 附件处理 |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | `claude --resume` 兼容续写 | 授权本次临时目录，并在提示词中提供本地路径 |
| Codex | `~/.codex/sessions` | 通过 `codex app-server` **全控制**；`exec resume` 降级 | app-server 健康时原生图片 + 同回合落地文件路径 |
| Kimi CLI | `.kimi-code`，兼容 `.kimi` 旧布局 | `kimi --session` 兼容续写 | 在提示词中提供本地路径，能否读取取决于 CLI 文件权限 |
| Grok Build | `~/.grok/sessions` | `grok --resume` 兼容续写 | 在提示词中提供本地路径；非交互安全模式 |

### 能力实现进度（现行 v0.2）

图例：✅ 已实现并对手机端广告 · ⚙️ 已实现，但受运行状态、探测结果或降级路径限制 · ❌ 手机端未实现或不广告。

| 能力 | Claude Code | Codex | Kimi CLI | Grok Build |
|---|---|---|---|---|
| 发现 / 列表 | ✅ | ✅ | ✅ | ✅ |
| 所有权门槛 | ✅ | ✅ | ✅ | ✅ |
| 历史记录 | ✅ | ✅ | ✅ | ✅ |
| 发送 + 流式输出 | ✅ | ✅ | ✅ | ✅ |
| 中断 | ✅ | ✅ | ✅ | ✅ |
| 新建原生线程 | ⚙️ starter 探测 | ⚙️ app-server 健康 | ⚙️ ACP starter 探测 | ⚙️ starter 探测 |
| 图片 / 文件附件 | ⚙️ 路径尽力读取 | ⚙️ 原生图片 + 同回合落地文件路径；降级时仅原生图片 | ⚙️ 路径尽力读取 | ⚙️ 路径尽力读取 |
| 批准 / 拒绝 | ❌ | ⚙️ 仅 app-server | ❌ | ❌ |
| 转向当前回合 | ❌ | ⚙️ 仅 app-server | ❌ | ❌ |
| NekoNest 持久 FIFO | ⚙️ CLI + 可写队列日志 | ⚙️ app-server 或 exec 降级 + 可写日志 | ⚙️ 已安装 CLI + 可写日志 | ⚙️ 已安装 CLI + 可写日志 |
| 等待状态信号 | ❌ 无桥接正向信号时关闭 | ⚙️ app-server 审批 + 结构化问答 | ❌ 除非观察到合法 ACP 事件 | ❌ 除非观察到合法厂商事件 |

受运行时限制的能力，只有在已安装 CLI / 控制路径通过探测后才会对手机端广告。新建原生线程属于带独立 `attachment_mode` 的设备级 `start_capabilities`，并不表示每个现有会话都有 `capabilities.spawn=true`。协议 1.2 显式发送全部布尔能力与稳定 `unavailable_reasons`；新 PWA 只对确认来自 1.1 daemon 的能力表兼容推定发送/中断，来源未知时失败关闭。Codex app-server 不健康时降级为 `exec resume`；非 Codex `steer` 始终关闭。

未安装某个 CLI，或本机没有该智能体的有效主线程时，不会影响其他智能体。

现行线协议标识：`claude_code`、`codex`、`kimi_cli`、`grok_build`。协议 1.x 仍解析已退役的 `kilo` id，使混合版本节点失败关闭而不是断开连接；现行目录不会再广告它。

**完整分 harness 能力矩阵**（现行标志、建线探测、附件接线、现行 vs v1）：[docs/agent-capability-matrix.zh-CN.md](docs/agent-capability-matrix.zh-CN.md) · [English](docs/agent-capability-matrix.md)。

## 快速开始

### 1. 在 VPS 安装并启动 Server

[GitHub Releases](https://github.com/klarkxy/nekonest/releases/latest) 提供
Linux amd64 与 arm64 的 Server 压缩包。包内已经包含同版本的
`pwa-dist`、中英文 README、许可证和版本标记；使用预编译包不需要安装
Node.js 或 Go 工具链。

```bash
# ARM VPS 请把 amd64 换成 arm64。
asset=nekonest-server-linux-amd64.tar.gz
base=https://github.com/klarkxy/nekonest/releases/latest/download
curl -fLO "$base/$asset"
curl -fLO "$base/checksums.txt"
grep "  $asset$" checksums.txt | sha256sum -c -

mkdir -p nekonest-server
tar -xzf "$asset" -C nekonest-server
cd nekonest-server
./nekonest-server -version

export NEKONEST_ADMIN_SECRET='换成足够长的随机串'
export NEKONEST_BOOTSTRAP_TOKEN='换成另一段足够长的随机串'
./nekonest-server -port 8080 -data ./data -pwa ./pwa-dist
```

如果要从源码构建，先安装 Go 1.22+、Node.js 和 pnpm：

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest/pwa
pnpm install --frozen-lockfile
pnpm build

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server
export NEKONEST_ADMIN_SECRET='换成足够长的随机串'
export NEKONEST_BOOTSTRAP_TOKEN='换成另一段足够长的随机串'
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

全新数据目录会初始化为 `sealed`。只有在首次启动时明确要创建管理员选定的 open 窝，才设置 `NEKONEST_TRANSPORT_MODE=open`；后续该变量只是断言，必须与持久化模式一致。

用 Caddy 或 Nginx 把公网 HTTPS/WSS 反向代理到 `127.0.0.1:8080`。完整示例见 [docs/deploy-vps.zh-CN.md](docs/deploy-vps.zh-CN.md)。

### 2. 在 Windows/Linux 安装、注册并运行 Daemon

先安装并正常使用至少一个受支持的智能体 CLI，使其本地存储中存在可续写线程。

Windows 使用 `nekonest-daemon-windows-amd64.zip`；Linux 使用
`nekonest-daemon-linux-amd64.tar.gz` 或
`nekonest-daemon-linux-arm64.tar.gz`。

```powershell
$asset = "nekonest-daemon-windows-amd64.zip"
$base = "https://github.com/klarkxy/nekonest/releases/latest/download"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt

$line = Get-Content .\checksums.txt | Where-Object { $_.EndsWith("  $asset") }
$expected = ($line -split '\s+')[0].ToLowerInvariant()
$actual = (Get-FileHash $asset -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "SHA-256 校验失败" }

Expand-Archive $asset -DestinationPath .\nekonest-daemon -Force
Set-Location .\nekonest-daemon
.\nekonest-daemon.exe -version

$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "与 VPS 相同的注册令牌"
.\nekonest-daemon.exe -register -name "书房电脑"
.\nekonest-daemon.exe
```

Linux 使用相同的校验文件和目录结构：

```bash
# ARM 主机请把 amd64 换成 arm64。
asset=nekonest-daemon-linux-amd64.tar.gz
base=https://github.com/klarkxy/nekonest/releases/latest/download
curl -fLO "$base/$asset"
curl -fLO "$base/checksums.txt"
grep "  $asset$" checksums.txt | sha256sum -c -
mkdir -p nekonest-daemon
tar -xzf "$asset" -C nekonest-daemon
cd nekonest-daemon
./nekonest-daemon -version
export NEKONEST_SERVER='https://nekonest.example.com'
export NEKONEST_BOOTSTRAP_TOKEN='与 VPS 相同的注册令牌'
./nekonest-daemon -register -name '书房电脑'
./nekonest-daemon
```

若要从源码构建，请克隆仓库，在 `daemon/` 下运行
`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-daemon ./cmd/daemon`。

注册成功后会写入主机配置并打印 6 位配对码。需要新码时，Windows 运行
`.\nekonest-daemon.exe -pair gen`，Linux 运行
`./nekonest-daemon -pair gen`。常驻运行见
[Windows](docs/deploy-windows.zh-CN.md) · [Linux](docs/deploy-linux.zh-CN.md)。

### 3. 在手机上配对

1. 打开 `https://nekonest.example.com`，输入 `NEKONEST_ADMIN_SECRET` 引导手机身份。
2. 进入「配对电脑」，输入 Daemon 打印的 6 位配对码。
3. 确认设备在线，按「目录 → 智能体 → 线程」进入已有线程。
4. 发送提示词，或选择图片、TXT、Markdown、PDF、JSON 等附件后发送。

验收清单：[docs/e2e-smoke.zh-CN.md](docs/e2e-smoke.zh-CN.md)。

## 配置（摘要）

| 变量 | 用途 |
|---|---|
| `NEKONEST_ADMIN_SECRET` | 管理员引导与签发手机令牌；**公网必须设置** |
| `NEKONEST_PHONE_SECRET` | 管理员密钥的弃用兼容别名 |
| `NEKONEST_BOOTSTRAP_TOKEN` | 保护 Daemon 注册；**公网必须设置**，且应与手机密钥不同 |
| `NEKONEST_TRANSPORT_MODE` | 可选的首次模式选择 / 后续断言；新 DB 默认 `sealed`，旧 DB 固定为 `open` |
| `NEKONEST_ALLOWED_ORIGINS` | 浏览器来源白名单，逗号分隔 |
| `NEKONEST_TRUST_PROXY` | 仅在反代**覆盖**转发头时设为 `1` |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | 反代不在 loopback 时声明可信网段 |
| `NEKONEST_VAPID_*` | 可选 Web Push |
| `NEKONEST_SERVER` | Daemon 注册时使用的 VPS 地址 |

> [!WARNING]
> 未设置管理员密钥时，Server 只绑定 loopback，用于本地开发。不要把未鉴权模式暴露到公网。

完整 flags、`config.json`、路由与限额见 [docs/configuration.zh-CN.md](docs/configuration.zh-CN.md)。信任模型见 [docs/security.zh-CN.md](docs/security.zh-CN.md)。

Server 会把模式持久化到 SQLite，并通过 `/health` 暴露；PWA 在建立 WebSocket 前读取这个运行时值。已有 open 窝继续 open。迁移到 sealed 必须执行离线备份与明文清理并重新配对；仅修改环境变量不能静默转换。

## 文档

| 文档 | 用途 |
|---|---|
| [docs/README.zh-CN.md](docs/README.zh-CN.md) | 文档总索引（中英对照） |
| [docs/deploy-vps.zh-CN.md](docs/deploy-vps.zh-CN.md) | Server、systemd、反代 |
| [docs/deploy-windows.zh-CN.md](docs/deploy-windows.zh-CN.md) | Daemon 注册、常驻、开机启动 |
| [docs/deploy-linux.zh-CN.md](docs/deploy-linux.zh-CN.md) | Linux Daemon 与 systemd 用户服务 |
| [docs/configuration.zh-CN.md](docs/configuration.zh-CN.md) | 环境变量、flags、限额 |
| [docs/security.zh-CN.md](docs/security.zh-CN.md) | 密钥、信任边界、加固 |
| [docs/architecture.zh-CN.md](docs/architecture.zh-CN.md) | 架构与投递语义 |
| [docs/protocol.zh-CN.md](docs/protocol.zh-CN.md) | 线协议与 REST/WS |
| [docs/development.zh-CN.md](docs/development.zh-CN.md) | 本地开发与测试 |
| [docs/troubleshooting.zh-CN.md](docs/troubleshooting.zh-CN.md) | 常见故障 |
| [docs/e2e-smoke.zh-CN.md](docs/e2e-smoke.zh-CN.md) | 部署验收 |
| [docs/release.zh-CN.md](docs/release.zh-CN.md) | 维护者发版 |
| [docs/v1-product.zh-CN.md](docs/v1-product.zh-CN.md) | 冻结 v1.0.0 目标合同 |
| [docs/brand-art.zh-CN.md](docs/brand-art.zh-CN.md) | 品牌资源重建 |
| [CHANGELOG.md](CHANGELOG.md) | 用户可见版本历史（英文） |
| [docs/archive/](docs/archive/) | 历史施工快照（**非**现行合同） |

English: [README.md](README.md) and `docs/*.md` short paths.

贡献者与编码智能体请先读 [AGENTS.md](AGENTS.md)（英文）。

## 项目结构

```text
nekonest/
├── protocol/   # 语言无关的 JSON 协议 schema
├── server/     # VPS：认证、配对、中转、SQLite、附件与 Web Push
├── daemon/     # Windows/Linux：发现、历史、提示词日志与智能体进程控制
├── pwa/        # Vue 3 + TypeScript + Pinia 移动端
├── docs/       # 运维与贡献者文档（英文短路径 + .zh-CN 中文）
├── CHANGELOG.md
├── LICENSE / LICENSE_zh
└── tools/      # 可复现的品牌资源构建工具
```

两个独立 Go module（`server/`、`daemon/`）和一个 pnpm 项目（`pwa/`），没有根 Go module。协议类型手动维护——见 [docs/protocol.zh-CN.md](docs/protocol.zh-CN.md)。

## 开发与验证

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
# 可选：Windows/Chromium 截图回归
pnpm test:visual
```

本地运行示意：

```text
server:  go run ./cmd/server -port 8080 -pwa ../pwa/dist
daemon:  go run ./cmd/daemon
pwa:     pnpm dev
```

详见 [docs/development.zh-CN.md](docs/development.zh-CN.md)。

## 当前边界（v0.2）

以下为稳定产品边界，不是待办清单：

- 手机主要续写原生线程。任何受支持 agent 都只有在其原生 starter 已安装/探测通过时才可提供 agent 范围的 `start_thread`；手机在首条提示词创建原生线程前仅保留本地草稿，目标只能是 daemon 当前已发现项目目录并集。
- Codex 是唯一全控制智能体（发送、批准/拒绝、中断、转向与完整原生附件）；其余三种即使宣告原生新建能力，也仍是兼容续写适配器。
- 新窝默认 sealed；缺少模式元数据的旧数据库/配置一次性认定为 open。一个窝只有一种持久化模式，禁止自动降级。
- Kimi CLI 与 Grok Build 当前只接收附件的本地路径，读取能力取决于对应 CLI 的文件权限。
- Web Push 需要额外配置 VAPID；未配置时不发送真实推送。
- Daemon 支持 **Windows 与 Linux**；macOS 后续再做。
- open 模式下 VPS 会持久化应用明文，请按敏感系统管理。

## 许可证

本项目采用 **Star And Thank Author License (SATA) 2.0**。

- 法律文本以英文 [LICENSE](LICENSE) 为准
- 简体中文译本 [LICENSE_zh](LICENSE_zh) 仅供方便理解，不具独立法律效力

使用、分发或修改本软件前，请先 star 本仓库并感谢作者。
