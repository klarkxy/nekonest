<div align="center">
  <img src="./pwa/public/brand/nekonest-duo.webp" width="360" alt="NekoNest">

  <h1>NekoNest · 猫娘窝</h1>

  <p><strong>在手机上，安全续写家里 Windows 电脑中已有的编码智能体线程。</strong></p>
  <p>自托管 · PC 仅出站连接 · 原生会话存储 · 移动端 PWA</p>

  <p>
    <a href="#快速开始">快速开始</a> ·
    <a href="#支持的智能体">支持的智能体</a> ·
    <a href="#部署与配置">部署与配置</a> ·
    <a href="#开发与验证">开发与验证</a>
  </p>
</div>

---

NekoNest 是一个自托管的远程续写桥梁：VPS 负责认证、配对、消息中转与持久化；Windows Daemon 主动连接 VPS，并从各智能体的本地原生存储中发现线程；手机 PWA 用于查看历史、发送提示词和附件、接收流式输出。

> [!IMPORTANT]
> NekoNest 只续写电脑上已经存在的线程，不在手机端远程新建线程。各智能体自己的本地存储始终是会话发现与历史记录的权威来源。

## 工作方式

```text
┌─────────────┐       HTTPS / WSS       ┌──────────────────┐
│  手机 PWA   │ ◄─────────────────────► │  VPS Server      │
│ Vue 3 + PWA │                         │  Go + SQLite      │
└─────────────┘                         └────────┬─────────┘
                                               │ WSS
                                               │ 由 PC 主动发起
                                      ┌────────▼─────────┐
                                      │ Windows Daemon   │
                                      │ 发现 / 历史 / 执行 │
                                      └────────┬─────────┘
                                               │ 本地存储与 CLI
                      ┌────────────┬───────────┼──────────┬────────────┐
                      │Claude Code │   Codex   │   Kilo   │  Kimi CLI  │ Grok Build
                      └────────────┴───────────┴──────────┴────────────┴───────────
```

家中电脑不需要公网 IP，也不需要开放入站端口。Daemon 通过出站 WebSocket 连接 VPS；手机只访问启用 HTTPS/WSS 的 VPS。

## 核心能力

- **原生线程发现**：按 `目录 → 智能体 → 线程` 展示电脑上的已有会话；没有可识别目录的线程统一归入「未分类」。
- **可靠续写**：提示词具有独立的接受、提交与失败状态，断线重连不会把“传输成功”误当作“智能体已接收”。
- **历史与流式输出**：合并原生历史、服务端持久化记录和实时输出，并保持稳定消息标识，避免重放产生重复消息。
- **图片与文档附件**：手机上传后，由 Daemon 下载到本次任务专用临时目录，再按不同 CLI 的能力传入。
- **移动端体验**：可安装 PWA、会话草稿、经过清理的 Markdown 渲染、断线恢复和可选 Web Push。
- **安全默认值**：手机密钥、Daemon 注册令牌、来源校验、附件校验、消息大小限制和受控代理信任。

## 支持的智能体

| 智能体 | 本地会话来源 | 续写入口 | 附件处理 |
|---|---|---|---|
| Claude Code | `~/.claude/projects` | `claude --resume` | 授权本次临时目录，并在提示词中提供本地路径 |
| Codex | `~/.codex/sessions` | `codex exec resume` | 图片使用原生图片参数；其他文件通过受限目录与本地路径传入 |
| Kilo | Kilo / OpenCode 本地数据库 | `kilo run --session` | 使用原生 `--file` 参数 |
| Kimi CLI | `.kimi-code`，兼容 `.kimi` 旧布局 | `kimi --session` | 在提示词中提供本地路径，能否读取取决于 CLI 文件权限 |
| Grok Build | `~/.grok/sessions` | `grok --resume` | 在提示词中提供本地路径，能否读取取决于 CLI 文件权限 |

未安装某个 CLI，或本机没有该智能体的有效主线程时，不会影响其他智能体的发现与使用。

## 快速开始

### 1. 在 VPS 构建并启动 Server

需要 Go 1.22+、Node.js 和 pnpm。公网部署还需要一个启用 HTTPS 的域名与 Caddy/Nginx。

```bash
git clone https://github.com/klarkxy/nekonest.git
cd nekonest

cd pwa
pnpm install --frozen-lockfile
pnpm build

cd ../server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nekonest-server ./cmd/server

export NEKONEST_PHONE_SECRET='换成足够长的随机串'
export NEKONEST_BOOTSTRAP_TOKEN='换成另一段足够长的随机串'
./nekonest-server -port 8080 -data ./data -pwa ../pwa/dist
```

用 Caddy 或 Nginx 把公网 HTTPS/WSS 反向代理到 `127.0.0.1:8080`。完整的 systemd、Caddy、Nginx 和可信代理示例见 [VPS 部署指南](docs/deploy-vps.md)。

### 2. 在 Windows 注册并运行 Daemon

先安装并正常使用至少一个受支持的智能体 CLI，使其本地存储中存在可续写线程。

```powershell
git clone https://github.com/klarkxy/nekonest.git
Set-Location nekonest\daemon

$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o nekonest-daemon.exe ./cmd/daemon

$env:NEKONEST_SERVER = "https://nekonest.example.com"
$env:NEKONEST_BOOTSTRAP_TOKEN = "与 VPS 相同的注册令牌"
.\nekonest-daemon.exe -register -name "书房电脑"
```

注册成功后，Daemon 会保存设备凭据并打印 6 位配对码。随后常驻运行：

```powershell
.\nekonest-daemon.exe
```

需要新的配对码时运行 `.\nekonest-daemon.exe -pair gen`。任务计划程序开机启动示例见 [Windows Daemon 部署指南](docs/deploy-windows.md)。

### 3. 在手机上配对

1. 打开 `https://nekonest.example.com`，输入 `NEKONEST_PHONE_SECRET`。
2. 进入「配对电脑」，输入 Daemon 打印的 6 位配对码。
3. 确认设备在线，按「目录 → 智能体 → 线程」进入已有线程。
4. 发送提示词，或选择图片、TXT、Markdown、PDF、JSON 等附件后发送。

发布后可按 [端到端冒烟清单](docs/e2e-smoke.md) 验收完整链路。

## 部署与配置

### Server 环境变量

| 变量 | 用途 |
|---|---|
| `NEKONEST_PHONE_SECRET` | 保护手机 REST 与 WebSocket；公网部署必须设置 |
| `NEKONEST_BOOTSTRAP_TOKEN` | 保护 Daemon 注册接口；公网部署必须设置，且应与手机密钥不同 |
| `NEKONEST_ALLOWED_ORIGINS` | 可选的浏览器来源白名单，逗号分隔 |
| `NEKONEST_TRUST_PROXY` | 正确覆盖转发头的反向代理位于前方时设为 `1` |
| `NEKONEST_TRUSTED_PROXY_CIDRS` | 反向代理不在 loopback 时，声明可信代理网段 |
| `NEKONEST_VAPID_PUBLIC_KEY` | 可选 Web Push 公钥 |
| `NEKONEST_VAPID_PRIVATE_KEY` | 可选 Web Push 私钥 |
| `NEKONEST_VAPID_SUBJECT` | 可选 Web Push 联系地址，如 `mailto:you@example.com` |

### Daemon 环境变量

| 变量 | 用途 |
|---|---|
| `NEKONEST_SERVER` | 注册时使用的 VPS 地址，如 `https://nekonest.example.com` |
| `NEKONEST_BOOTSTRAP_TOKEN` | 注册时发送给 VPS 的注册令牌 |

> [!WARNING]
> 未设置 `NEKONEST_PHONE_SECRET` 时，Server 只绑定 loopback，用于本地开发。不要通过修改监听地址或代理配置把未鉴权模式暴露到公网。

附件每次最多选择 5 个，单个文件不超过 4 MB。上传内容保存在 Server 的数据目录中，并在执行时暂存到 Windows；不要把数据目录、设备令牌、手机密钥或本地会话存储提交到版本库。

NekoNest 的 VPS 会中转并持久化设备信息、消息和附件，当前不提供端到端加密。请把 VPS 与 `data/` 目录视为敏感系统，使用 HTTPS/WSS，并限制服务器和备份的访问权限。

## 项目结构

```text
nekonest/
├── protocol/   # 语言无关的 JSON 协议 schema
├── server/     # VPS 服务：认证、配对、中转、SQLite、附件与 Web Push
├── daemon/     # Windows 服务：发现、历史、提示词日志与智能体进程控制
├── pwa/        # Vue 3 + TypeScript + Pinia 移动端
├── docs/       # 部署与端到端验收文档
└── tools/      # 可复现的品牌资源构建工具
```

协议类型目前手动维护。修改线协议时，需要同步检查：

- `protocol/protocol.json`
- `server/internal/protocol/types.go`
- `pwa/src/types/protocol.ts`
- Daemon 的消息分发与各智能体适配器

## 开发与验证

仓库包含两个独立 Go module 和一个 pnpm 项目，没有根 Go module。

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

本地开发可分别运行：

```text
server:  go run ./cmd/server -port 8080 -pwa ../pwa/dist
daemon:  go run ./cmd/daemon
pwa:     pnpm dev
```

## 当前边界

- 手机端不创建新线程；请先在 PC 上创建。
- 工具审批取决于各智能体的非交互 CLI 能力；无法承载时应回到 PC 处理。
- Kimi CLI 与 Grok Build 当前只接收附件的本地路径，读取能力取决于对应 CLI 的文件权限。
- Web Push 需要额外配置 VAPID；未配置时不发送真实推送。
- Daemon 当前面向 Windows。
