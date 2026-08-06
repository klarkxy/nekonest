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
                    ┌────────────┬───────────┬───┴──────┬────────────┐
                    │Claude Code │   Codex   │  Kilo    │ Kimi CLI   │ Grok Build
                    └────────────┴───────────┴──────────┴────────────┴───────────
```

家中电脑不需要公网 IP，也不需要开放入站端口。Daemon 通过出站 WebSocket 连接 VPS；手机只访问启用 HTTPS/WSS 的 VPS。

## 核心能力

- **原生线程发现**：按 `目录 → 智能体 → 线程` 展示；没有可识别目录的线程归入「**未分类**」。
- **可靠续写**：提示词具有独立的接受、提交与失败状态；断线重连不会把“传输成功”误当作“智能体已接收”。
- **历史与流式输出**：合并原生历史、服务端持久化与实时输出，并保持稳定消息标识；CLI 标准错误只作本机诊断，不进入对话正文。
- **图片与文档附件**：手机上传后由 Daemon 下载到本次任务临时目录，再按各 CLI 能力传入（最多 5 个、单个 ≤ 4 MB）。
- **Codex 全控制**：原生 app-server 发送、批准/拒绝、转向、中断以及图片/文件附件；不健康时诚实降级到 `exec resume`。五个 agent 都只有在各自 starter 探测通过后，才可提供 agent 范围的原生新建。
- **传输协商**：每个窝固定 `open` 或 `sealed`；v0.2 默认 open，sealed 仍是显式 v1 预览。
- **移动端体验**：可安装 PWA、会话草稿、线程级或整项目的手机本地收起、经清理的 Markdown、断线恢复与可选 Web Push。
- **版本诊断**：页面顶部对比当前网页与实时 Server 版本；每台机器在自己的设备卡片上报告 Daemon 版本及更新状态。
- **安全默认值**：管理员引导、可撤销手机身份、Daemon 注册令牌、来源校验、附件校验、消息大小限制与受控代理信任。

## 支持的智能体

| 智能体 | 本地会话来源 | 控制方式 | 附件处理 |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | `claude --resume` 兼容续写 | 授权本次临时目录，并在提示词中提供本地路径 |
| Codex | `~/.codex/sessions` | 通过 `codex app-server` **全控制**；`exec resume` 降级 | app-server 健康时原生图片与文件 |
| Kilo | Kilo / OpenCode 本地数据库 | `kilo run --session` 兼容续写 | 原生 `--file`（广告为 `path_best_effort`） |
| Kimi CLI | `.kimi-code`，兼容 `.kimi` 旧布局 | `kimi --session` 兼容续写 | 在提示词中提供本地路径，能否读取取决于 CLI 文件权限 |
| Grok Build | `~/.grok/sessions` | `grok --resume` 兼容续写 | 在提示词中提供本地路径；非交互安全模式 |

未安装某个 CLI，或本机没有该智能体的有效主线程时，不会影响其他智能体。

线协议标识：`claude_code`、`codex`、`kilo`、`kimi_cli`、`grok_build`。

**完整分 harness 能力矩阵**（现行标志、建线探测、附件接线、现行 vs v1）：[docs/agent-capability-matrix.zh-CN.md](docs/agent-capability-matrix.zh-CN.md) · [English](docs/agent-capability-matrix.md)。

## 快速开始

### 1. 在 VPS 构建并启动 Server

需要 Go 1.22+、Node.js 和 pnpm。公网部署还需要启用 HTTPS 的域名与 Caddy/Nginx。

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest

cd pwa
pnpm install --frozen-lockfile
pnpm build

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server

export NEKONEST_ADMIN_SECRET='换成足够长的随机串'
export NEKONEST_BOOTSTRAP_TOKEN='换成另一段足够长的随机串'
export NEKONEST_TRANSPORT_MODE='open'
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

用 Caddy 或 Nginx 把公网 HTTPS/WSS 反向代理到 `127.0.0.1:8080`。完整示例见 [docs/deploy-vps.zh-CN.md](docs/deploy-vps.zh-CN.md)。

### 2. 在 Windows/Linux 注册并运行 Daemon

先安装并正常使用至少一个受支持的智能体 CLI，使其本地存储中存在可续写线程。

```powershell
git clone https://github.com/klarkxy/nekonest.git
Set-Location nekonest\daemon

$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o nekonest-daemon.exe ./cmd/daemon

$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "与 VPS 相同的注册令牌"
.\nekonest-daemon.exe -register -name "书房电脑"
.\nekonest-daemon.exe
```

注册成功后会写入主机配置并打印 6 位配对码。需要新码时：`.\nekonest-daemon.exe -pair gen`。常驻运行见 [Windows](docs/deploy-windows.zh-CN.md) · [Linux](docs/deploy-linux.zh-CN.md)。

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
| `NEKONEST_TRANSPORT_MODE` | 全窝固定模式；v0.2 默认 `open`，sealed 为显式预览 |
| `NEKONEST_ALLOWED_ORIGINS` | 浏览器来源白名单，逗号分隔 |
| `NEKONEST_TRUST_PROXY` | 仅在反代**覆盖**转发头时设为 `1` |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | 反代不在 loopback 时声明可信网段 |
| `NEKONEST_VAPID_*` | 可选 Web Push |
| `NEKONEST_SERVER` | Daemon 注册时使用的 VPS 地址 |

> [!WARNING]
> 未设置管理员密钥时，Server 只绑定 loopback，用于本地开发。不要把未鉴权模式暴露到公网。

完整 flags、`config.json`、路由与限额见 [docs/configuration.zh-CN.md](docs/configuration.zh-CN.md)。信任模型见 [docs/security.zh-CN.md](docs/security.zh-CN.md)。

v0.2 的运维默认值是 `open`，VPS 可中转并持久化应用明文；请把主机与 `data/` 视为敏感系统。密封 E2E 为显式预览模式，仅在 v1 验收切换后成为新窝默认。

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
- Codex 是唯一全控制智能体（发送、批准/拒绝、中断、转向与完整原生附件）；其余四种即使宣告原生新建能力，也仍是兼容续写适配器。
- v0.2 各端默认 `open` 传输。sealed 为显式预览；一个窝固定一种模式，禁止自动降级。
- Kimi CLI 与 Grok Build 当前只接收附件的本地路径，读取能力取决于对应 CLI 的文件权限。
- Web Push 需要额外配置 VAPID；未配置时不发送真实推送。
- Daemon 支持 **Windows 与 Linux**；macOS 后续再做。
- open 模式下 VPS 会持久化应用明文，请按敏感系统管理。

## 许可证

本项目采用 **Star And Thank Author License (SATA) 2.0**。

- 法律文本以英文 [LICENSE](LICENSE) 为准
- 简体中文译本 [LICENSE_zh](LICENSE_zh) 仅供方便理解，不具独立法律效力

使用、分发或修改本软件前，请先 star 本仓库并感谢作者。
